package handlers

import (
	"fmt"
	"log/slog"
	"net/http"
	"nora/auth"
	"nora/database/queries"
	"nora/util"
	"os"
	"path/filepath"
	"strings"
)

type storageInfoResponse struct {
	DBBytes             int64 `json:"db_bytes"`
	UploadsBytes        int64 `json:"uploads_bytes"`
	AttachmentsBytes    int64 `json:"attachments_bytes"`
	EmojisBytes         int64 `json:"emojis_bytes"`
	IconBytes           int64 `json:"icon_bytes"`
	AvatarsBytes        int64 `json:"avatars_bytes"`
	TotalFiles          int   `json:"total_files"`
	MaxMB               int   `json:"max_mb"`
	ChannelHistoryLimit int   `json:"channel_history_limit"`
}

type updateStorageRequest struct {
	MaxMB               *int `json:"max_mb"`
	ChannelHistoryLimit *int `json:"channel_history_limit"`
}

func (d *Deps) GetStorageInfo(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if !user.IsOwner {
		util.Error(w, http.StatusForbidden, "owner only")
		return
	}

	// DB size
	var dbBytes int64
	if info, err := os.Stat(d.DBPath); err == nil {
		dbBytes = info.Size()
	}

	// Walk uploads directory and count categories
	var uploadsBytes, attachmentsBytes, emojisBytes, iconBytes, avatarsBytes int64
	var totalFiles int

	entries, _ := os.ReadDir(d.UploadsDir)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		size := info.Size()
		name := e.Name()
		uploadsBytes += size
		totalFiles++

		if strings.HasPrefix(name, "emoji_") {
			emojisBytes += size
		} else if strings.HasPrefix(name, "server_icon.") {
			iconBytes += size
		} else if strings.HasPrefix(name, "avatar_") {
			avatarsBytes += size
		} else {
			attachmentsBytes += size
		}
	}

	util.JSON(w, http.StatusOK, storageInfoResponse{
		DBBytes:             dbBytes,
		UploadsBytes:        uploadsBytes,
		AttachmentsBytes:    attachmentsBytes,
		EmojisBytes:         emojisBytes,
		IconBytes:           iconBytes,
		AvatarsBytes:        avatarsBytes,
		TotalFiles:          totalFiles,
		MaxMB:               d.StorageMaxMB,
		ChannelHistoryLimit: d.ChannelHistoryLimit,
	})
}

func (d *Deps) UpdateStorageSettings(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if !user.IsOwner {
		util.Error(w, http.StatusForbidden, "owner only")
		return
	}

	var req updateStorageRequest
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.MaxMB != nil {
		mb := *req.MaxMB
		if mb < 0 {
			util.Error(w, http.StatusBadRequest, "max_mb must be >= 0")
			return
		}
		d.Settings.Set("storage_max_mb", fmt.Sprintf("%d", mb))
		d.StorageMaxMB = mb
	}

	if req.ChannelHistoryLimit != nil {
		limit := *req.ChannelHistoryLimit
		if limit < 0 {
			util.Error(w, http.StatusBadRequest, "channel_history_limit must be >= 0")
			return
		}
		d.Settings.Set("channel_history_limit", fmt.Sprintf("%d", limit))
		d.ChannelHistoryLimit = limit

		// Immediately perform trim if a non-zero limit was set
		if limit > 0 {
			go d.TrimAllChannels()
		}
	}

	util.JSON(w, http.StatusOK, map[string]any{
		"max_mb":                d.StorageMaxMB,
		"channel_history_limit": d.ChannelHistoryLimit,
	})
}

// TrimAllChannels performs history trim on all channels.
func (d *Deps) TrimAllChannels() {
	if d.ChannelHistoryLimit <= 0 {
		return
	}

	channelIDs, err := d.Storage.AllChannelIDs()
	if err != nil {
		slog.Error("storage cleanup: listing channels failed", "error", err)
		return
	}

	for _, chID := range channelIDs {
		paths, err := d.Storage.TrimChannelHistory(chID, d.ChannelHistoryLimit)
		if err != nil {
			slog.Error("storage cleanup: channel trim failed", "channel_id", chID, "error", err)
			continue
		}
		DeleteAttachmentFiles(d.UploadsDir, paths, d.Attachments)
	}
}

// DeleteAttachmentFiles deletes files from disk (only if no other attachment references them).
func DeleteAttachmentFiles(uploadsDir string, filepaths []string, attQ *queries.AttachmentQueries) {
	for _, fp := range filepaths {
		// Ref counting: skip if another attachment still uses this file
		if attQ != nil {
			if count, err := attQ.CountByFilepath(fp); err == nil && count > 0 {
				continue
			}
		}
		p := filepath.Join(uploadsDir, fp)
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			slog.Error("failed to delete file", "path", p, "error", err)
		}
	}
}
