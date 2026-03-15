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

// BackupDatabase vytvoří konzistentní snapshot SQLite databáze a vrátí jako download.
func (d *Deps) BackupDatabase(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if !user.IsOwner {
		util.Error(w, http.StatusForbidden, "owner only")
		return
	}

	// Vytvořit temp soubor pro backup
	tmpFile, err := os.CreateTemp("", "nora-backup-*.db")
	if err != nil {
		slog.Error("backup: vytvoření temp souboru selhalo", "error", err)
		util.Error(w, http.StatusInternalServerError, "backup failed")
		return
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	// VACUUM INTO vytvoří konzistentní kopii bez WAL
	// Escape single quotes v tmpPath (tmpPath je z os.CreateTemp, ale pro jistotu)
	escaped := strings.ReplaceAll(tmpPath, "'", "''")
	_, err = d.DB.Exec(fmt.Sprintf("VACUUM INTO '%s'", escaped))
	if err != nil {
		slog.Error("backup: VACUUM INTO selhalo", "error", err)
		util.Error(w, http.StatusInternalServerError, "backup failed")
		return
	}

	// Otevřít backup soubor pro čtení
	f, err := os.Open(tmpPath)
	if err != nil {
		slog.Error("backup: otevření backup souboru selhalo", "error", err)
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

// RestoreDatabase nahraje .db soubor a nahradí databázi.
// NEBEZPEČNÁ OPERACE — pouze owner.
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

	// Ověřit SQLite magic bytes
	magic := make([]byte, 16)
	if _, err := io.ReadFull(file, magic); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid file")
		return
	}
	if string(magic[:15]) != "SQLite format 3" {
		util.Error(w, http.StatusBadRequest, "not a valid SQLite database")
		return
	}
	// Přetočit zpět na začátek
	file.Seek(0, io.SeekStart)

	// Uložit do temp souboru
	tmpFile, err := os.CreateTemp(filepath.Dir(d.DBPath), "nora-restore-*.db")
	if err != nil {
		slog.Error("restore: vytvoření temp souboru selhalo", "error", err)
		util.Error(w, http.StatusInternalServerError, "restore failed")
		return
	}
	tmpPath := tmpFile.Name()

	if _, err := io.Copy(tmpFile, file); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		slog.Error("restore: kopírování dat selhalo", "error", err)
		util.Error(w, http.StatusInternalServerError, "restore failed")
		return
	}
	tmpFile.Close()

	// Zavřít stávající DB
	d.DB.Close()

	// Vytvořit zálohu stávající DB
	backupPath := d.DBPath + ".pre-restore"
	if err := os.Rename(d.DBPath, backupPath); err != nil {
		slog.Error("restore: přejmenování stávající DB selhalo", "error", err)
		// Zkusit znovu otevřít stávající
		d.reopenDB()
		os.Remove(tmpPath)
		util.Error(w, http.StatusInternalServerError, "restore failed")
		return
	}

	// Přesunout novou DB na místo
	if err := os.Rename(tmpPath, d.DBPath); err != nil {
		slog.Error("restore: přesun nové DB selhalo", "error", err)
		// Vrátit zpět starou DB
		os.Rename(backupPath, d.DBPath)
		d.reopenDB()
		util.Error(w, http.StatusInternalServerError, "restore failed")
		return
	}

	// Smazat WAL a SHM soubory (staré)
	os.Remove(d.DBPath + "-wal")
	os.Remove(d.DBPath + "-shm")

	// Otevřít novou DB
	if err := d.reopenDB(); err != nil {
		slog.Error("restore: otevření nové DB selhalo, vracím starou", "error", err)
		os.Rename(d.DBPath, tmpPath) // odložit novou
		os.Rename(backupPath, d.DBPath)
		d.reopenDB()
		os.Remove(tmpPath)
		util.Error(w, http.StatusInternalServerError, "restore failed - reverted")
		return
	}

	// Úspěch — smazat zálohu
	os.Remove(backupPath)
	slog.Info("databáze obnovena z uploadu", "user", user.ID)

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok", "message": "database restored, server restart recommended"})
}

// BackupInfo vrátí informace o databázi.
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

// reopenDB znovu otevře databázi a aktualizuje všechny queries.
func (d *Deps) reopenDB() error {
	db, err := d.DB.Reopen(d.DBPath)
	if err != nil {
		return err
	}
	d.DB = db

	// Aktualizovat všechny query structs
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
