package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"nora/auth"
	"nora/util"
	"nora/ws"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type settingsResponse struct {
	ServerName          string `json:"server_name"`
	ServerDescription   string `json:"server_description"`
	IconURL             string `json:"icon_url"`
	MaxUploadSizeMB     int    `json:"max_upload_size_mb"`
	OpenRegistration    bool   `json:"open_registration"`
	GameServersEnabled  bool   `json:"game_servers_enabled"`
	SwarmSharingEnabled bool   `json:"swarm_sharing_enabled"`

	AutoModEnabled     bool     `json:"automod_enabled"`
	AutoModWordFilter  []string `json:"automod_word_filter"`
	AutoModSpamMaxMsg  int      `json:"automod_spam_max_messages"`
	AutoModSpamWindow  int      `json:"automod_spam_window_seconds"`
	AutoModSpamTimeout int      `json:"automod_spam_timeout_seconds"`
}

type updateSettingsRequest struct {
	ServerName          *string `json:"server_name"`
	ServerDescription   *string `json:"server_description"`
	MaxUploadSizeMB     *int    `json:"max_upload_size_mb"`
	OpenRegistration    *bool   `json:"open_registration"`
	GameServersEnabled  *bool   `json:"game_servers_enabled"`
	SwarmSharingEnabled *bool   `json:"swarm_sharing_enabled"`

	AutoModEnabled     *bool      `json:"automod_enabled"`
	AutoModWordFilter  *[]string  `json:"automod_word_filter"`
	AutoModSpamMaxMsg  *int       `json:"automod_spam_max_messages"`
	AutoModSpamWindow  *int       `json:"automod_spam_window_seconds"`
	AutoModSpamTimeout *int       `json:"automod_spam_timeout_seconds"`
}

func (d *Deps) GetSettings(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if !user.IsOwner {
		util.Error(w, http.StatusForbidden, "owner only")
		return
	}

	d.SettingsMu.RLock()
	resp := settingsResponse{
		ServerName:          d.ServerName,
		ServerDescription:   d.ServerDesc,
		IconURL:             d.ServerIconURL,
		MaxUploadSizeMB:     int(d.MaxUploadSize / (1024 * 1024)),
		OpenRegistration:    d.OpenReg,
		GameServersEnabled:  d.GameServersEnabled,
		SwarmSharingEnabled: d.SwarmSharingEnabled,
	}
	d.SettingsMu.RUnlock()
	if d.AutoMod != nil {
		d.AutoMod.Mu.RLock()
		resp.AutoModEnabled = d.AutoMod.Enabled
		resp.AutoModWordFilter = d.AutoMod.WordFilter
		resp.AutoModSpamMaxMsg = d.AutoMod.SpamMaxMessages
		resp.AutoModSpamWindow = d.AutoMod.SpamWindowSeconds
		resp.AutoModSpamTimeout = d.AutoMod.SpamTimeoutSeconds
		d.AutoMod.Mu.RUnlock()
		if resp.AutoModWordFilter == nil {
			resp.AutoModWordFilter = []string{}
		}
	}
	util.JSON(w, http.StatusOK, resp)
}

func (d *Deps) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if !user.IsOwner {
		util.Error(w, http.StatusForbidden, "owner only")
		return
	}

	var req updateSettingsRequest
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	d.SettingsMu.Lock()
	if req.ServerName != nil {
		name := *req.ServerName
		if len(name) < 1 || len(name) > 64 {
			d.SettingsMu.Unlock()
			util.Error(w, http.StatusBadRequest, "server_name must be 1-64 characters")
			return
		}
		d.Settings.Set("server_name", name)
		d.ServerName = name
	}

	if req.ServerDescription != nil {
		desc := *req.ServerDescription
		if len(desc) > 256 {
			d.SettingsMu.Unlock()
			util.Error(w, http.StatusBadRequest, "server_description must be at most 256 characters")
			return
		}
		d.Settings.Set("server_description", desc)
		d.ServerDesc = desc
	}

	if req.MaxUploadSizeMB != nil {
		mb := *req.MaxUploadSizeMB
		if mb < 1 {
			d.SettingsMu.Unlock()
			util.Error(w, http.StatusBadRequest, "max_upload_size_mb must be at least 1")
			return
		}
		d.Settings.Set("max_upload_size_mb", fmt.Sprintf("%d", mb))
		d.MaxUploadSize = int64(mb) * 1024 * 1024
	}

	if req.OpenRegistration != nil {
		d.Settings.Set("open_registration", strconv.FormatBool(*req.OpenRegistration))
		d.OpenReg = *req.OpenRegistration
	}

	if req.GameServersEnabled != nil {
		d.Settings.Set("game_servers_enabled", strconv.FormatBool(*req.GameServersEnabled))
		d.GameServersEnabled = *req.GameServersEnabled
	}

	if req.SwarmSharingEnabled != nil {
		d.Settings.Set("swarm_sharing_enabled", strconv.FormatBool(*req.SwarmSharingEnabled))
		d.SwarmSharingEnabled = *req.SwarmSharingEnabled
	}
	d.SettingsMu.Unlock()

	// Auto-moderation settings
	if d.AutoMod != nil {
		if req.AutoModEnabled != nil {
			d.Settings.Set("automod_enabled", strconv.FormatBool(*req.AutoModEnabled))
			d.AutoMod.Mu.Lock()
			d.AutoMod.Enabled = *req.AutoModEnabled
			d.AutoMod.Mu.Unlock()
		}
		if req.AutoModWordFilter != nil {
			b, _ := json.Marshal(*req.AutoModWordFilter)
			d.Settings.Set("automod_word_filter", string(b))
			d.AutoMod.SetWordFilter(*req.AutoModWordFilter)
		}
		if req.AutoModSpamMaxMsg != nil {
			v := *req.AutoModSpamMaxMsg
			if v < 2 {
				util.Error(w, http.StatusBadRequest, "automod_spam_max_messages must be at least 2")
				return
			}
			d.Settings.Set("automod_spam_max_messages", strconv.Itoa(v))
			d.AutoMod.Mu.Lock()
			d.AutoMod.SpamMaxMessages = v
			d.AutoMod.Mu.Unlock()
		}
		if req.AutoModSpamWindow != nil {
			v := *req.AutoModSpamWindow
			if v < 5 {
				util.Error(w, http.StatusBadRequest, "automod_spam_window_seconds must be at least 5")
				return
			}
			d.Settings.Set("automod_spam_window_seconds", strconv.Itoa(v))
			d.AutoMod.Mu.Lock()
			d.AutoMod.SpamWindowSeconds = v
			d.AutoMod.Mu.Unlock()
		}
		if req.AutoModSpamTimeout != nil {
			v := *req.AutoModSpamTimeout
			if v < 60 {
				util.Error(w, http.StatusBadRequest, "automod_spam_timeout_seconds must be at least 60")
				return
			}
			d.Settings.Set("automod_spam_timeout_seconds", strconv.Itoa(v))
			d.AutoMod.Mu.Lock()
			d.AutoMod.SpamTimeoutSeconds = v
			d.AutoMod.Mu.Unlock()
		}
	}

	resp := settingsResponse{
		ServerName:          d.ServerName,
		ServerDescription:   d.ServerDesc,
		IconURL:             d.ServerIconURL,
		MaxUploadSizeMB:     int(d.MaxUploadSize / (1024 * 1024)),
		OpenRegistration:    d.OpenReg,
		GameServersEnabled:  d.GameServersEnabled,
		SwarmSharingEnabled: d.SwarmSharingEnabled,
	}
	if d.AutoMod != nil {
		d.AutoMod.Mu.RLock()
		resp.AutoModEnabled = d.AutoMod.Enabled
		resp.AutoModWordFilter = d.AutoMod.WordFilter
		resp.AutoModSpamMaxMsg = d.AutoMod.SpamMaxMessages
		resp.AutoModSpamWindow = d.AutoMod.SpamWindowSeconds
		resp.AutoModSpamTimeout = d.AutoMod.SpamTimeoutSeconds
		d.AutoMod.Mu.RUnlock()
		if resp.AutoModWordFilter == nil {
			resp.AutoModWordFilter = []string{}
		}
	}

	event, _ := ws.NewEvent(ws.EventServerUpdate, map[string]any{
		"name":        d.ServerName,
		"description": d.ServerDesc,
		"icon_url":    d.ServerIconURL,
	})
	d.Hub.Broadcast(event)

	d.logAudit(user.ID, "server.update", "server", "", map[string]string{"name": d.ServerName})

	util.JSON(w, http.StatusOK, resp)
}

func (d *Deps) UploadServerIcon(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if !user.IsOwner {
		util.Error(w, http.StatusForbidden, "owner only")
		return
	}

	// Max 512KB
	r.Body = http.MaxBytesReader(w, r.Body, 512*1024)

	file, header, err := r.FormFile("file")
	if err != nil {
		util.Error(w, http.StatusBadRequest, "failed to read file")
		return
	}
	defer file.Close()

	// MIME detekce
	buf := make([]byte, 512)
	n, _ := file.Read(buf)
	mimeType := http.DetectContentType(buf[:n])
	file.Seek(0, io.SeekStart)

	if !strings.HasPrefix(mimeType, "image/") {
		util.Error(w, http.StatusBadRequest, "only image files are allowed")
		return
	}

	// Smazat předchozí ikonu
	entries, _ := os.ReadDir(d.UploadsDir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "server_icon.") {
			os.Remove(filepath.Join(d.UploadsDir, e.Name()))
		}
	}

	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext != ".jpg" && ext != ".jpeg" && ext != ".png" && ext != ".gif" && ext != ".webp" {
		ext = ".png"
	}
	filename := "server_icon" + ext

	os.MkdirAll(d.UploadsDir, 0755)
	dst, err := os.Create(filepath.Join(d.UploadsDir, filename))
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to save file")
		return
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to write file")
		return
	}

	iconURL := "/api/uploads/" + filename
	d.Settings.Set("server_icon_url", iconURL)
	d.ServerIconURL = iconURL

	event, _ := ws.NewEvent(ws.EventServerUpdate, map[string]any{
		"name":     d.ServerName,
		"icon_url": d.ServerIconURL,
	})
	d.Hub.Broadcast(event)

	util.JSON(w, http.StatusOK, map[string]string{"icon_url": iconURL})
}

func (d *Deps) DeleteServerIcon(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if !user.IsOwner {
		util.Error(w, http.StatusForbidden, "owner only")
		return
	}

	// Smazat soubor ikony
	entries, _ := os.ReadDir(d.UploadsDir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "server_icon.") {
			os.Remove(filepath.Join(d.UploadsDir, e.Name()))
		}
	}

	d.Settings.Set("server_icon_url", "")
	d.ServerIconURL = ""

	event, _ := ws.NewEvent(ws.EventServerUpdate, map[string]any{
		"name":     d.ServerName,
		"icon_url": "",
	})
	d.Hub.Broadcast(event)

	util.JSON(w, http.StatusOK, map[string]string{"icon_url": ""})
}
