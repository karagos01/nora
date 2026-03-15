package handlers

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"nora/auth"
	"nora/gameserver"
	"nora/models"
	"nora/util"
	"nora/ws"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/coder/websocket"
	"github.com/google/uuid"
)

// Stav instalace Dockeru (in-memory, per server proces)
var (
	dockerInstallMu      sync.Mutex
	dockerInstallRunning bool
	dockerInstallLog     string
	dockerInstallDone    bool
	dockerInstallError   string
)

// GetGameServerPresets vrátí dostupné presety z disku
func (d *Deps) GetGameServerPresets(w http.ResponseWriter, r *http.Request) {
	if d.GameServerMgr == nil {
		util.JSON(w, http.StatusOK, []gameserver.PresetEntry{})
		return
	}
	presets := gameserver.ListPresets(d.GameServerMgr.PresetsDir)
	if presets == nil {
		presets = []gameserver.PresetEntry{}
	}
	util.JSON(w, http.StatusOK, presets)
}

// GetGameServers vrátí všechny instance herních serverů
func (d *Deps) GetGameServers(w http.ResponseWriter, r *http.Request) {
	servers, err := d.GameServerQ.GetAll()
	if err != nil {
		servers = []models.GameServerInstance{}
	}
	if servers == nil {
		servers = []models.GameServerInstance{}
	}

	// Zkontroluj skutečný stav kontejnerů
	if d.GameServerMgr != nil {
		for i := range servers {
			if servers[i].Status == "running" && servers[i].ContainerID != "" {
				if !d.GameServerMgr.IsRunning(servers[i].ContainerID) {
					servers[i].Status = "stopped"
					d.GameServerQ.UpdateStatus(servers[i].ID, "stopped", servers[i].ContainerID, "")
				}
			}
		}
	}

	util.JSON(w, http.StatusOK, servers)
}

// CreateGameServer vytvoří novou instanci herního serveru
func (d *Deps) CreateGameServer(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermAdmin); err != nil {
		util.Error(w, http.StatusForbidden, "admin permission required")
		return
	}

	if !d.GameServersEnabled {
		util.Error(w, http.StatusServiceUnavailable, "game servers are not enabled")
		return
	}

	var req struct {
		Name   string `json:"name"`
		Preset string `json:"preset"`
	}
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" || len(req.Name) > 64 {
		util.Error(w, http.StatusBadRequest, "name must be 1-64 characters")
		return
	}

	id, _ := uuid.NewV7()
	gs := &models.GameServerInstance{
		ID:         id.String(),
		Name:       req.Name,
		Status:     "stopped",
		CreatorID:  user.ID,
		AccessMode: "open",
	}

	// Vytvoř adresář + server.toml z presetu
	preset := req.Preset
	if preset == "" {
		preset = "minecraft"
	}
	if err := d.GameServerMgr.CreateServerDir(gs.ID, preset); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to create server directory")
		return
	}

	if err := d.GameServerQ.Create(gs); err != nil {
		d.GameServerMgr.DeleteServerDir(gs.ID)
		util.Error(w, http.StatusInternalServerError, "failed to create game server")
		return
	}

	gs, _ = d.GameServerQ.GetByID(gs.ID)

	event, _ := ws.NewEvent(ws.EventGameServerCreate, gs)
	d.Hub.Broadcast(event)

	util.JSON(w, http.StatusCreated, gs)
}

// DeleteGameServer smaže herní server a odebere kontejner
func (d *Deps) DeleteGameServer(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermAdmin); err != nil {
		util.Error(w, http.StatusForbidden, "admin permission required")
		return
	}

	id := r.PathValue("id")
	gs, err := d.GameServerQ.GetByID(id)
	if err != nil {
		util.Error(w, http.StatusNotFound, "game server not found")
		return
	}

	// Zastavit a odebrat kontejner
	if d.GameServerMgr != nil {
		if gs.ContainerID != "" {
			d.GameServerMgr.Stop(gs.ContainerID, gs.ID)
			d.GameServerMgr.Remove(gs.ContainerID)
		}
		d.GameServerMgr.RemoveByName(gs.ID)
		d.GameServerMgr.DeleteServerDir(gs.ID)
	}

	// Smazat všechny členy (CASCADE by měl stačit, ale pro jistotu)
	d.GameServerQ.RemoveAllMembers(id)
	d.GameServerQ.Delete(id)

	event, _ := ws.NewEvent(ws.EventGameServerDelete, map[string]string{"id": id})
	d.Hub.Broadcast(event)

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// StartGameServer spustí herní server (docker run) — async
func (d *Deps) StartGameServer(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermAdmin); err != nil {
		util.Error(w, http.StatusForbidden, "admin permission required")
		return
	}

	if !d.GameServersEnabled || !d.GameServerMgr.DockerAvailable() {
		util.Error(w, http.StatusServiceUnavailable, "game servers are not enabled or Docker is not available")
		return
	}

	id := r.PathValue("id")
	gs, err := d.GameServerQ.GetByID(id)
	if err != nil {
		util.Error(w, http.StatusNotFound, "game server not found")
		return
	}

	if gs.Status == "running" || gs.Status == "starting" {
		util.Error(w, http.StatusConflict, "server is already "+gs.Status)
		return
	}

	// Update status → starting
	d.GameServerQ.UpdateStatus(gs.ID, "starting", "", "")
	gs.Status = "starting"
	event, _ := ws.NewEvent(ws.EventGameServerStatus, gs)
	d.Hub.Broadcast(event)

	// Async start
	go func() {
		d.GameServerMgr.RemoveByName(gs.ID)

		containerID, err := d.GameServerMgr.Start(gs.ID)
		if err != nil {
			slog.Error("game server start selhal", "server_id", gs.ID, "error", err)
			d.GameServerQ.UpdateStatus(gs.ID, "error", "", err.Error())
			gs.Status = "error"
			gs.ErrorMsg = err.Error()
			event, _ := ws.NewEvent(ws.EventGameServerStatus, gs)
			d.Hub.Broadcast(event)
			return
		}

		d.GameServerQ.UpdateStatus(gs.ID, "running", containerID, "")
		gs.Status = "running"
		gs.ContainerID = containerID
		gs.ErrorMsg = ""
		event, _ := ws.NewEvent(ws.EventGameServerStatus, gs)
		d.Hub.Broadcast(event)
		slog.Info("game server spuštěn", "server_id", gs.ID, "container_id", containerID[:12])

		// Nastavit firewall podle access_mode
		d.RefreshGameServerFirewall(gs)
	}()

	util.JSON(w, http.StatusOK, map[string]string{"status": "starting"})
}

// StopGameServer zastaví herní server
func (d *Deps) StopGameServer(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermAdmin); err != nil {
		util.Error(w, http.StatusForbidden, "admin permission required")
		return
	}

	if !d.GameServersEnabled || !d.GameServerMgr.DockerAvailable() {
		util.Error(w, http.StatusServiceUnavailable, "game servers are not enabled or Docker is not available")
		return
	}

	id := r.PathValue("id")
	gs, err := d.GameServerQ.GetByID(id)
	if err != nil {
		util.Error(w, http.StatusNotFound, "game server not found")
		return
	}

	if gs.ContainerID != "" {
		d.GameServerMgr.Stop(gs.ContainerID, gs.ID)
		d.GameServerMgr.Remove(gs.ContainerID)
	}

	d.GameServerQ.UpdateStatus(gs.ID, "stopped", "", "")
	gs.Status = "stopped"
	event, _ := ws.NewEvent(ws.EventGameServerStop, gs)
	d.Hub.Broadcast(event)

	util.JSON(w, http.StatusOK, map[string]string{"status": "stopped"})
}

// RestartGameServer restartuje herní server
func (d *Deps) RestartGameServer(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermAdmin); err != nil {
		util.Error(w, http.StatusForbidden, "admin permission required")
		return
	}

	if !d.GameServersEnabled || !d.GameServerMgr.DockerAvailable() {
		util.Error(w, http.StatusServiceUnavailable, "game servers are not enabled or Docker is not available")
		return
	}

	id := r.PathValue("id")
	gs, err := d.GameServerQ.GetByID(id)
	if err != nil {
		util.Error(w, http.StatusNotFound, "game server not found")
		return
	}

	if gs.ContainerID != "" {
		d.GameServerMgr.Stop(gs.ContainerID, gs.ID)
		d.GameServerMgr.Remove(gs.ContainerID)
	}
	d.GameServerMgr.RemoveByName(gs.ID)

	d.GameServerQ.UpdateStatus(gs.ID, "starting", "", "")
	gs.Status = "starting"
	event, _ := ws.NewEvent(ws.EventGameServerStatus, gs)
	d.Hub.Broadcast(event)

	go func() {
		containerID, err := d.GameServerMgr.Start(gs.ID)
		if err != nil {
			d.GameServerQ.UpdateStatus(gs.ID, "error", "", err.Error())
			gs.Status = "error"
			gs.ErrorMsg = err.Error()
			event, _ := ws.NewEvent(ws.EventGameServerStatus, gs)
			d.Hub.Broadcast(event)
			return
		}

		d.GameServerQ.UpdateStatus(gs.ID, "running", containerID, "")
		gs.Status = "running"
		gs.ContainerID = containerID
		gs.ErrorMsg = ""
		event, _ := ws.NewEvent(ws.EventGameServerStatus, gs)
		d.Hub.Broadcast(event)

		// Nastavit firewall podle access_mode
		d.RefreshGameServerFirewall(gs)
	}()

	util.JSON(w, http.StatusOK, map[string]string{"status": "restarting"})
}

// RefreshGameServerFirewall nastaví iptables pravidla podle access_mode a členů
func (d *Deps) RefreshGameServerFirewall(gs *models.GameServerInstance) {
	if d.GameServerMgr == nil {
		return
	}
	cfg, err := gameserver.ReadConfig(d.GameServerMgr.DataDir, gs.ID)
	if err != nil {
		slog.Error("refreshGameServerFirewall: čtení configu selhalo", "server_id", gs.ID, "error", err)
		return
	}
	// Načíst aktuální access_mode z DB
	fresh, err := d.GameServerQ.GetByID(gs.ID)
	if err != nil {
		return
	}
	var memberIPs []string
	if fresh.AccessMode == "room" {
		memberIPs, _ = d.GameServerQ.GetMemberIPs(gs.ID)
	}
	d.GameServerMgr.SetupFirewall(cfg, fresh.AccessMode, memberIPs)
}

// JoinGameServer přidá uživatele do game server roomu
func (d *Deps) JoinGameServer(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	id := r.PathValue("id")

	gs, err := d.GameServerQ.GetByID(id)
	if err != nil {
		util.Error(w, http.StatusNotFound, "game server not found")
		return
	}

	if err := d.GameServerQ.JoinMember(gs.ID, user.ID); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to join")
		return
	}

	member := models.GameServerMember{
		GameServerID: gs.ID,
		UserID:       user.ID,
		Username:     user.Username,
	}
	event, _ := ws.NewEvent(ws.EventGameServerJoin, member)
	d.Hub.Broadcast(event)

	// Refresh firewall pokud server běží v room mode
	if gs.Status == "running" && gs.AccessMode == "room" {
		d.RefreshGameServerFirewall(gs)
	}

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// LeaveGameServer odebere uživatele z game server roomu
func (d *Deps) LeaveGameServer(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	id := r.PathValue("id")

	gs, err := d.GameServerQ.GetByID(id)
	if err != nil {
		util.Error(w, http.StatusNotFound, "game server not found")
		return
	}

	d.GameServerQ.LeaveMember(gs.ID, user.ID)

	event, _ := ws.NewEvent(ws.EventGameServerLeave, map[string]string{
		"game_server_id": gs.ID,
		"user_id":        user.ID,
	})
	d.Hub.Broadcast(event)

	// Refresh firewall pokud server běží v room mode
	if gs.Status == "running" && gs.AccessMode == "room" {
		d.RefreshGameServerFirewall(gs)
	}

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// GetGameServerMembers vrátí seznam členů roomu
func (d *Deps) GetGameServerMembers(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	members, err := d.GameServerQ.GetMembers(id)
	if err != nil {
		members = []models.GameServerMember{}
	}
	if members == nil {
		members = []models.GameServerMember{}
	}
	util.JSON(w, http.StatusOK, members)
}

// SetGameServerAccess změní access mode (open/room) — vyžaduje admin
func (d *Deps) SetGameServerAccess(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermAdmin); err != nil {
		util.Error(w, http.StatusForbidden, "admin permission required")
		return
	}

	id := r.PathValue("id")
	gs, err := d.GameServerQ.GetByID(id)
	if err != nil {
		util.Error(w, http.StatusNotFound, "game server not found")
		return
	}

	var req struct {
		Mode string `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Mode != "open" && req.Mode != "room" {
		util.Error(w, http.StatusBadRequest, "mode must be 'open' or 'room'")
		return
	}

	d.GameServerQ.UpdateAccessMode(gs.ID, req.Mode)

	// Refresh firewall pokud server běží
	if gs.Status == "running" {
		d.RefreshGameServerFirewall(gs)
	}

	// Broadcast updated status
	gs.AccessMode = req.Mode
	event, _ := ws.NewEvent(ws.EventGameServerStatus, gs)
	d.Hub.Broadcast(event)

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// GameServerStats vrátí statistiky běžícího kontejneru
func (d *Deps) GameServerStats(w http.ResponseWriter, r *http.Request) {
	if !d.GameServersEnabled {
		util.Error(w, http.StatusServiceUnavailable, "game servers are not enabled")
		return
	}

	id := r.PathValue("id")
	gs, err := d.GameServerQ.GetByID(id)
	if err != nil {
		util.Error(w, http.StatusNotFound, "game server not found")
		return
	}

	if gs.Status != "running" || gs.ContainerID == "" {
		util.Error(w, http.StatusBadRequest, "server is not running")
		return
	}

	stats, err := d.GameServerMgr.Stats(gs.ContainerID)
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to get stats")
		return
	}

	util.JSON(w, http.StatusOK, stats)
}

// GameServerLogs — WS endpoint pro live streaming docker logů
func (d *Deps) GameServerLogs(w http.ResponseWriter, r *http.Request) {
	// Token z Authorization headeru (preferovaný) nebo query parametru (fallback)
	var token string
	if ah := r.Header.Get("Authorization"); strings.HasPrefix(ah, "Bearer ") {
		token = ah[7:]
	} else {
		token = r.URL.Query().Get("token")
	}
	if token == "" {
		http.Error(w, "missing token", http.StatusUnauthorized)
		return
	}

	claims, err := d.JWTService.Validate(token)
	if err != nil {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}

	if d.Bans.IsBanned(claims.UserID) {
		http.Error(w, "banned", http.StatusForbidden)
		return
	}

	// Admin permission kontrola
	perms, err := d.Roles.GetUserPermissions(claims.UserID)
	if err != nil || (!claims.IsOwner && perms&models.PermAdmin == 0) {
		http.Error(w, "admin permission required", http.StatusForbidden)
		return
	}

	if !d.GameServersEnabled {
		http.Error(w, "game servers not enabled", http.StatusServiceUnavailable)
		return
	}

	id := r.PathValue("id")
	gs, err := d.GameServerQ.GetByID(id)
	if err != nil {
		http.Error(w, "game server not found", http.StatusNotFound)
		return
	}

	if gs.Status != "running" || gs.ContainerID == "" {
		http.Error(w, "server is not running", http.StatusBadRequest)
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		slog.Error("gameserver logs ws: upgrade selhal", "error", err)
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	reader, err := d.GameServerMgr.StreamLogs(ctx, gs.ContainerID, 100)
	if err != nil {
		conn.Close(websocket.StatusInternalError, "failed to stream logs")
		return
	}
	defer reader.Close()

	// Čtení příkazů od klienta (async)
	go func() {
		defer cancel()
		for {
			_, msg, err := conn.Read(ctx)
			if err != nil {
				return
			}
			cmd := string(msg)
			if cmd != "" {
				d.GameServerMgr.SendCommand(gs.ContainerID, gs.ID, cmd)
			}
		}
	}()

	// Stream logů klientovi
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := scanner.Text()
		err := conn.Write(ctx, websocket.MessageText, []byte(line))
		if err != nil {
			return
		}
	}
}

// --- File endpointy ---

// GameServerFiles vrátí výpis souborů v adresáři game serveru
func (d *Deps) GameServerFiles(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermAdmin); err != nil {
		util.Error(w, http.StatusForbidden, "admin permission required")
		return
	}

	if !d.GameServersEnabled {
		util.Error(w, http.StatusServiceUnavailable, "game servers are not enabled")
		return
	}

	id := r.PathValue("id")
	if _, err := d.GameServerQ.GetByID(id); err != nil {
		util.Error(w, http.StatusNotFound, "game server not found")
		return
	}

	path := r.URL.Query().Get("path")
	entries, err := d.GameServerMgr.ListFiles(id, path)
	if err != nil {
		util.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	util.JSON(w, http.StatusOK, entries)
}

// GameServerFileContent vrátí obsah souboru
func (d *Deps) GameServerFileContent(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermAdmin); err != nil {
		util.Error(w, http.StatusForbidden, "admin permission required")
		return
	}

	if !d.GameServersEnabled {
		util.Error(w, http.StatusServiceUnavailable, "game servers are not enabled")
		return
	}

	id := r.PathValue("id")
	if _, err := d.GameServerQ.GetByID(id); err != nil {
		util.Error(w, http.StatusNotFound, "game server not found")
		return
	}

	path := r.URL.Query().Get("path")
	if path == "" {
		util.Error(w, http.StatusBadRequest, "path required")
		return
	}

	content, err := d.GameServerMgr.ReadFile(id, path)
	if err != nil {
		util.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	util.JSON(w, http.StatusOK, map[string]string{"content": content})
}

// GameServerFileWrite zapíše obsah do souboru
func (d *Deps) GameServerFileWrite(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermAdmin); err != nil {
		util.Error(w, http.StatusForbidden, "admin permission required")
		return
	}

	if !d.GameServersEnabled {
		util.Error(w, http.StatusServiceUnavailable, "game servers are not enabled")
		return
	}

	id := r.PathValue("id")
	if _, err := d.GameServerQ.GetByID(id); err != nil {
		util.Error(w, http.StatusNotFound, "game server not found")
		return
	}

	var req struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Path == "" {
		util.Error(w, http.StatusBadRequest, "path required")
		return
	}

	if err := d.GameServerMgr.WriteFile(id, req.Path, req.Content); err != nil {
		util.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// GameServerFileUpload nahraje soubor (multipart, max 50MB)
func (d *Deps) GameServerFileUpload(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermAdmin); err != nil {
		util.Error(w, http.StatusForbidden, "admin permission required")
		return
	}

	if !d.GameServersEnabled {
		util.Error(w, http.StatusServiceUnavailable, "game servers are not enabled")
		return
	}

	id := r.PathValue("id")
	if _, err := d.GameServerQ.GetByID(id); err != nil {
		util.Error(w, http.StatusNotFound, "game server not found")
		return
	}

	// Max 50MB
	r.Body = http.MaxBytesReader(w, r.Body, 50<<20)
	if err := r.ParseMultipartForm(50 << 20); err != nil {
		util.Error(w, http.StatusBadRequest, "file too large (max 50MB)")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		util.Error(w, http.StatusBadRequest, "missing file")
		return
	}
	defer file.Close()

	path := r.URL.Query().Get("path")
	if path == "" {
		path = "."
	}

	// Cílová cesta: path/filename
	targetPath := path + "/" + header.Filename
	if path == "." || path == "" {
		targetPath = header.Filename
	}

	data, err := io.ReadAll(file)
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to read file")
		return
	}

	if err := d.GameServerMgr.WriteFile(id, targetPath, string(data)); err != nil {
		util.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// GameServerFileDelete smaže soubor nebo adresář
func (d *Deps) GameServerFileDelete(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermAdmin); err != nil {
		util.Error(w, http.StatusForbidden, "admin permission required")
		return
	}

	if !d.GameServersEnabled {
		util.Error(w, http.StatusServiceUnavailable, "game servers are not enabled")
		return
	}

	id := r.PathValue("id")
	if _, err := d.GameServerQ.GetByID(id); err != nil {
		util.Error(w, http.StatusNotFound, "game server not found")
		return
	}

	path := r.URL.Query().Get("path")
	if path == "" {
		util.Error(w, http.StatusBadRequest, "path required")
		return
	}

	if err := d.GameServerMgr.DeleteFile(id, path); err != nil {
		util.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// GameServerMkdir vytvoří adresář
func (d *Deps) GameServerMkdir(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermAdmin); err != nil {
		util.Error(w, http.StatusForbidden, "admin permission required")
		return
	}

	if !d.GameServersEnabled {
		util.Error(w, http.StatusServiceUnavailable, "game servers are not enabled")
		return
	}

	id := r.PathValue("id")
	if _, err := d.GameServerQ.GetByID(id); err != nil {
		util.Error(w, http.StatusNotFound, "game server not found")
		return
	}

	var req struct {
		Path string `json:"path"`
	}
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Path == "" {
		util.Error(w, http.StatusBadRequest, "path required")
		return
	}

	if err := d.GameServerMgr.Mkdir(id, req.Path); err != nil {
		util.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// GameServerFileDownload stáhne soubor z game serveru (binární)
func (d *Deps) GameServerFileDownload(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermAdmin); err != nil {
		util.Error(w, http.StatusForbidden, "admin permission required")
		return
	}

	if !d.GameServersEnabled {
		util.Error(w, http.StatusServiceUnavailable, "game servers are not enabled")
		return
	}

	id := r.PathValue("id")
	if _, err := d.GameServerQ.GetByID(id); err != nil {
		util.Error(w, http.StatusNotFound, "game server not found")
		return
	}

	path := r.URL.Query().Get("path")
	if path == "" {
		util.Error(w, http.StatusBadRequest, "path required")
		return
	}

	absPath, err := d.GameServerMgr.FilePath(id, path)
	if err != nil {
		util.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	// Content-Disposition pro download
	filename := filepath.Base(path)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	http.ServeFile(w, r, absPath)
}

// GameServerListRecursive vrátí rekurzivní výpis souborů v adresáři
func (d *Deps) GameServerListRecursive(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermAdmin); err != nil {
		util.Error(w, http.StatusForbidden, "admin permission required")
		return
	}

	if !d.GameServersEnabled {
		util.Error(w, http.StatusServiceUnavailable, "game servers are not enabled")
		return
	}

	id := r.PathValue("id")
	if _, err := d.GameServerQ.GetByID(id); err != nil {
		util.Error(w, http.StatusNotFound, "game server not found")
		return
	}

	path := r.URL.Query().Get("path")
	entries, err := d.GameServerMgr.ListFilesRecursive(id, path)
	if err != nil {
		util.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if entries == nil {
		entries = []gameserver.RecursiveFileEntry{}
	}

	util.JSON(w, http.StatusOK, entries)
}

// RCONCommand vykoná RCON příkaz na běžícím game serveru (Source RCON protokol)
func (d *Deps) RCONCommand(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermAdmin); err != nil {
		util.Error(w, http.StatusForbidden, "admin permission required")
		return
	}

	if !d.GameServersEnabled {
		util.Error(w, http.StatusServiceUnavailable, "game servers are not enabled")
		return
	}

	id := r.PathValue("id")
	gs, err := d.GameServerQ.GetByID(id)
	if err != nil {
		util.Error(w, http.StatusNotFound, "game server not found")
		return
	}

	if gs.Status != "running" || gs.ContainerID == "" {
		util.Error(w, http.StatusBadRequest, "server is not running")
		return
	}

	// Přečíst RCON konfiguraci ze server.toml
	cfg, err := gameserver.ReadConfig(d.GameServerMgr.DataDir, gs.ID)
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to read server config")
		return
	}

	if cfg.RCONPort == 0 || cfg.RCONPassword == "" {
		util.Error(w, http.StatusBadRequest, "RCON is not configured for this server (set rcon_port and rcon_password in server.toml)")
		return
	}

	var req struct {
		Command string `json:"command"`
	}
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Command == "" {
		util.Error(w, http.StatusBadRequest, "command is required")
		return
	}

	// Sanitizovat command — newliny by mohly způsobit problémy
	req.Command = strings.ReplaceAll(req.Command, "\n", " ")
	req.Command = strings.ReplaceAll(req.Command, "\r", "")

	// Získat IP adresu kontejneru
	containerIP, err := gameserver.GetContainerIP(gs.ContainerID)
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to get container IP: "+err.Error())
		return
	}

	// Vykonat RCON příkaz
	response, err := gameserver.RCONExec(containerIP, cfg.RCONPort, cfg.RCONPassword, req.Command)
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "RCON command failed: "+err.Error())
		return
	}

	util.JSON(w, http.StatusOK, map[string]string{"response": response})
}

// DockerStatus vrátí stav Dockeru na serveru
func (d *Deps) DockerStatus(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if !user.IsOwner {
		util.Error(w, http.StatusForbidden, "owner only")
		return
	}

	available := d.GameServerMgr != nil && d.GameServerMgr.DockerAvailable()

	dockerInstallMu.Lock()
	resp := map[string]any{
		"available":  available,
		"installing": dockerInstallRunning,
	}
	if dockerInstallDone {
		resp["install_done"] = true
		resp["install_error"] = dockerInstallError
		resp["install_log"] = dockerInstallLog
	}
	dockerInstallMu.Unlock()

	util.JSON(w, http.StatusOK, resp)
}

// InstallDocker nainstaluje Docker na server (owner only, Linux)
func (d *Deps) InstallDocker(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if !user.IsOwner {
		util.Error(w, http.StatusForbidden, "owner only")
		return
	}

	if d.GameServerMgr != nil && d.GameServerMgr.DockerAvailable() {
		util.JSON(w, http.StatusOK, map[string]string{"status": "already_installed"})
		return
	}

	dockerInstallMu.Lock()
	if dockerInstallRunning {
		dockerInstallMu.Unlock()
		util.JSON(w, http.StatusOK, map[string]string{"status": "already_running"})
		return
	}
	dockerInstallRunning = true
	dockerInstallDone = false
	dockerInstallLog = ""
	dockerInstallError = ""
	dockerInstallMu.Unlock()

	go func() {
		// Instalace Dockeru přes apt (bezpečnější než curl|sh)
		cmd := exec.Command("sh", "-c",
			"apt-get update && apt-get install -y docker.io && systemctl enable --now docker")
		out, err := cmd.CombinedOutput()

		dockerInstallMu.Lock()
		dockerInstallRunning = false
		dockerInstallDone = true
		dockerInstallLog = string(out)
		if err != nil {
			dockerInstallError = err.Error()
			slog.Error("Docker instalace selhala", "error", err)
		} else {
			dockerInstallError = ""
			slog.Info("Docker úspěšně nainstalován")
		}
		dockerInstallMu.Unlock()
	}()

	util.JSON(w, http.StatusOK, map[string]string{"status": "started"})
}
