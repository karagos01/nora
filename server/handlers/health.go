package handlers

import (
	"net/http"
	"nora/util"
	"os"
	"runtime"
	"time"
)

var serverStartTime = time.Now()

func (d *Deps) Health(w http.ResponseWriter, r *http.Request) {
	// Základní metriky
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	uptime := time.Since(serverStartTime)

	// Počet online uživatelů (WS klientů)
	wsClients := 0
	if d.Hub != nil {
		wsClients = d.Hub.ClientCount()
	}

	// Velikost DB souboru
	var dbSizeBytes int64
	if d.DBPath != "" {
		if info, err := os.Stat(d.DBPath); err == nil {
			dbSizeBytes = info.Size()
		}
	}

	util.JSON(w, http.StatusOK, map[string]any{
		"status":          "ok",
		"uptime_seconds":  int(uptime.Seconds()),
		"ws_connections":  wsClients,
		"db_size_bytes":   dbSizeBytes,
		"memory_alloc_mb": int(mem.Alloc / 1024 / 1024),
		"memory_sys_mb":   int(mem.Sys / 1024 / 1024),
		"goroutines":      runtime.NumGoroutine(),
		"go_version":      runtime.Version(),
	})
}

func (d *Deps) ServerInfo(w http.ResponseWriter, r *http.Request) {
	automodEnabled := false
	if d.AutoMod != nil {
		d.AutoMod.Mu.RLock()
		automodEnabled = d.AutoMod.Enabled
		d.AutoMod.Mu.RUnlock()
	}
	regMode := d.RegMode
	if regMode == "" {
		if d.OpenReg {
			regMode = "open"
		} else {
			regMode = "closed"
		}
	}
	util.JSON(w, http.StatusOK, map[string]any{
		"name":                   d.ServerName,
		"description":            d.ServerDesc,
		"icon_url":               d.ServerIconURL,
		"stun_url":               d.StunURL,
		"game_servers_enabled":   d.GameServersEnabled,
		"swarm_sharing_enabled":  d.SwarmSharingEnabled,
		"automod_enabled":        automodEnabled,
		"registration_mode":      regMode,
		"version":                "2.0.0",
		"hint":                   "Use NORA client",
	})
}
