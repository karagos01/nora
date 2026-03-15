package handlers

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"nora/auth"
	"nora/util"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// BackupDatabase creates a consistent snapshot of the SQLite database and returns it as a download.
func (d *Deps) BackupDatabase(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if !user.IsOwner {
		util.Error(w, http.StatusForbidden, "owner only")
		return
	}

	// Create temp file for backup
	tmpFile, err := os.CreateTemp("", "nora-backup-*.db")
	if err != nil {
		slog.Error("backup: failed to create temp file", "error", err)
		util.Error(w, http.StatusInternalServerError, "backup failed")
		return
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	// VACUUM INTO creates a consistent copy without WAL
	// Escape single quotes in tmpPath (tmpPath is from os.CreateTemp, but just in case)
	escaped := strings.ReplaceAll(tmpPath, "'", "''")
	_, err = d.DB.Exec(fmt.Sprintf("VACUUM INTO '%s'", escaped))
	if err != nil {
		slog.Error("backup: VACUUM INTO failed", "error", err)
		util.Error(w, http.StatusInternalServerError, "backup failed")
		return
	}

	// Open backup file for reading
	f, err := os.Open(tmpPath)
	if err != nil {
		slog.Error("backup: failed to open backup file", "error", err)
		util.Error(w, http.StatusInternalServerError, "backup failed")
		return
	}
	defer f.Close()

	info, _ := f.Stat()
	filename := fmt.Sprintf("nora-backup-%s.db", time.Now().Format("2006-01-02"))

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", info.Size()))
	io.Copy(w, f)
}

// RestoreDatabase uploads a .db file and replaces the database.
// DANGEROUS OPERATION — owner only.
func (d *Deps) RestoreDatabase(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if !user.IsOwner {
		util.Error(w, http.StatusForbidden, "owner only")
		return
	}

	// Max 500MB
	r.Body = http.MaxBytesReader(w, r.Body, 500*1024*1024)

	file, _, err := r.FormFile("database")
	if err != nil {
		util.Error(w, http.StatusBadRequest, "missing database file")
		return
	}
	defer file.Close()

	// Verify SQLite magic bytes
	magic := make([]byte, 16)
	if _, err := io.ReadFull(file, magic); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid file")
		return
	}
	if string(magic[:15]) != "SQLite format 3" {
		util.Error(w, http.StatusBadRequest, "not a valid SQLite database")
		return
	}
	// Seek back to beginning
	file.Seek(0, io.SeekStart)

	// Save to temp file
	tmpFile, err := os.CreateTemp(filepath.Dir(d.DBPath), "nora-restore-*.db")
	if err != nil {
		slog.Error("restore: failed to create temp file", "error", err)
		util.Error(w, http.StatusInternalServerError, "restore failed")
		return
	}
	tmpPath := tmpFile.Name()

	if _, err := io.Copy(tmpFile, file); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		slog.Error("restore: failed to copy data", "error", err)
		util.Error(w, http.StatusInternalServerError, "restore failed")
		return
	}
	tmpFile.Close()

	// Close existing DB
	d.DB.Close()

	// Create backup of existing DB
	backupPath := d.DBPath + ".pre-restore"
	if err := os.Rename(d.DBPath, backupPath); err != nil {
		slog.Error("restore: failed to rename existing DB", "error", err)
		// Try to reopen existing
		d.reopenDB()
		os.Remove(tmpPath)
		util.Error(w, http.StatusInternalServerError, "restore failed")
		return
	}

	// Move new DB into place
	if err := os.Rename(tmpPath, d.DBPath); err != nil {
		slog.Error("restore: failed to move new DB", "error", err)
		// Revert to old DB
		os.Rename(backupPath, d.DBPath)
		d.reopenDB()
		util.Error(w, http.StatusInternalServerError, "restore failed")
		return
	}

	// Delete WAL and SHM files (old)
	os.Remove(d.DBPath + "-wal")
	os.Remove(d.DBPath + "-shm")

	// Open new DB
	if err := d.reopenDB(); err != nil {
		slog.Error("restore: failed to open new DB, reverting to old", "error", err)
		os.Rename(d.DBPath, tmpPath) // set aside new one
		os.Rename(backupPath, d.DBPath)
		d.reopenDB()
		os.Remove(tmpPath)
		util.Error(w, http.StatusInternalServerError, "restore failed - reverted")
		return
	}

	// Success — delete backup
	os.Remove(backupPath)
	slog.Info("database restored from upload", "user", user.ID)

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok", "message": "database restored, server restart recommended"})
}

// BackupInfo returns information about the database.
func (d *Deps) BackupInfo(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if !user.IsOwner {
		util.Error(w, http.StatusForbidden, "owner only")
		return
	}

	var dbSize int64
	if info, err := os.Stat(d.DBPath); err == nil {
		dbSize = info.Size()
	}

	var users, messages, channels int
	d.DB.QueryRow("SELECT COUNT(*) FROM users").Scan(&users)
	d.DB.QueryRow("SELECT COUNT(*) FROM messages").Scan(&messages)
	d.DB.QueryRow("SELECT COUNT(*) FROM channels").Scan(&channels)

	util.JSON(w, http.StatusOK, map[string]any{
		"database_size": dbSize,
		"users":         users,
		"messages":      messages,
		"channels":      channels,
	})
}

// reopenDB reopens the database and updates all query structs.
func (d *Deps) reopenDB() error {
	db, err := d.DB.Reopen(d.DBPath)
	if err != nil {
		return err
	}
	d.DB = db

	// Update all query structs
	sqlDB := db.DB
	d.Users.DB = sqlDB
	d.Channels.DB = sqlDB
	d.Messages.DB = sqlDB
	d.Roles.DB = sqlDB
	d.Invites.DB = sqlDB
	d.RefreshTokens.DB = sqlDB
	d.DMs.DB = sqlDB
	d.Bans.DB = sqlDB
	d.Attachments.DB = sqlDB
	d.AuthChallenges.DB = sqlDB
	d.Timeouts.DB = sqlDB
	d.Friends.DB = sqlDB
	d.Settings.DB = sqlDB
	d.FriendRequests.DB = sqlDB
	d.Blocks.DB = sqlDB
	d.Groups.DB = sqlDB
	d.Emojis.DB = sqlDB
	d.Categories.DB = sqlDB
	d.Reactions.DB = sqlDB
	d.IPBans.DB = sqlDB
	d.Storage.DB = sqlDB
	d.AuditLog.DB = sqlDB
	d.LinkPreviews.DB = sqlDB
	d.Polls.DB = sqlDB
	d.Webhooks.DB = sqlDB
	d.GalleryQ.DB = sqlDB
	d.FileStorage.DB = sqlDB
	d.Shares.DB = sqlDB
	d.LAN.DB = sqlDB
	d.Whiteboards.DB = sqlDB
	d.GameServerQ.DB = sqlDB
	d.SwarmSeeds.DB = sqlDB
	d.Scheduled.DB = sqlDB
	d.KanbanQ.DB = sqlDB
	d.CalendarQ.DB = sqlDB
	d.DeviceBans.DB = sqlDB
	d.UserDevices.DB = sqlDB
	d.InviteChain.DB = sqlDB
	d.Quarantine.DB = sqlDB
	d.Approvals.DB = sqlDB

	return nil
}
