package handlers

import (
	"net/http"
	"nora/auth"
	"nora/util"
	"nora/ws"

	"github.com/google/uuid"
)

// SwarmAddSeed — user registers as a seeder for a file
func (d *Deps) SwarmAddSeed(w http.ResponseWriter, r *http.Request) {
	if !d.SwarmSharingEnabled {
		util.Error(w, http.StatusForbidden, "swarm sharing is disabled")
		return
	}

	user := auth.GetUser(r)
	shareID := r.PathValue("id")

	dir, err := d.Shares.GetDirectory(shareID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "share not found")
		return
	}

	// S6 fix: kontrola isActive
	if !dir.IsActive {
		util.Error(w, http.StatusGone, "share is not active")
		return
	}

	// Verify access to share
	if dir.OwnerID != user.ID {
		perm, err := d.Shares.GetEffectivePermission(shareID, user.ID)
		if err != nil || !perm.CanRead || perm.IsBlocked {
			util.Error(w, http.StatusForbidden, "no access")
			return
		}
	}

	var req struct {
		FileID string `json:"file_id"`
	}
	if err := util.DecodeJSON(r, &req); err != nil || req.FileID == "" {
		util.Error(w, http.StatusBadRequest, "file_id is required")
		return
	}

	// Verify the file belongs to the share
	file, err := d.Shares.GetFile(req.FileID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "file not found")
		return
	}
	if file.DirectoryID != shareID {
		util.Error(w, http.StatusBadRequest, "file does not belong to this share")
		return
	}

	id, _ := uuid.NewV7()
	if err := d.SwarmSeeds.AddSeed(id.String(), req.FileID, user.ID); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to add seed")
		return
	}

	// S5 fix: broadcast only to owner and users with access to the share
	event, _ := ws.NewEvent(ws.EventSwarmSeedAdded, map[string]string{
		"directory_id": shareID,
		"file_id":      req.FileID,
		"user_id":      user.ID,
	})
	d.Hub.BroadcastToUser(dir.OwnerID, event)
	if dir.OwnerID != user.ID {
		d.Hub.BroadcastToUser(user.ID, event)
	}

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// SwarmRemoveSeed — unregister seed
func (d *Deps) SwarmRemoveSeed(w http.ResponseWriter, r *http.Request) {
	if !d.SwarmSharingEnabled {
		util.Error(w, http.StatusForbidden, "swarm sharing is disabled")
		return
	}

	user := auth.GetUser(r)
	shareID := r.PathValue("id")
	fileID := r.PathValue("fileId")

	// S1 fix: verify access to the share
	dir, err := d.Shares.GetDirectory(shareID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "share not found")
		return
	}
	if dir.OwnerID != user.ID {
		perm, err := d.Shares.GetEffectivePermission(shareID, user.ID)
		if err != nil || !perm.CanRead || perm.IsBlocked {
			util.Error(w, http.StatusForbidden, "no access")
			return
		}
	}

	// S2 fix: verify that the file belongs to the share
	file, err := d.Shares.GetFile(fileID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "file not found")
		return
	}
	if file.DirectoryID != shareID {
		util.Error(w, http.StatusBadRequest, "file does not belong to this share")
		return
	}

	if err := d.SwarmSeeds.RemoveSeed(fileID, user.ID); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to remove seed")
		return
	}

	event, _ := ws.NewEvent(ws.EventSwarmSeedRemoved, map[string]string{
		"directory_id": shareID,
		"file_id":      fileID,
		"user_id":      user.ID,
	})
	d.Hub.BroadcastToUser(dir.OwnerID, event)
	if dir.OwnerID != user.ID {
		d.Hub.BroadcastToUser(user.ID, event)
	}

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// SwarmSources — online seeders for a file
func (d *Deps) SwarmSources(w http.ResponseWriter, r *http.Request) {
	if !d.SwarmSharingEnabled {
		util.Error(w, http.StatusForbidden, "swarm sharing is disabled")
		return
	}

	user := auth.GetUser(r)
	shareID := r.PathValue("id")
	fileID := r.PathValue("fileId")

	dir, err := d.Shares.GetDirectory(shareID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "share not found")
		return
	}

	// Verify access
	if dir.OwnerID != user.ID {
		perm, err := d.Shares.GetEffectivePermission(shareID, user.ID)
		if err != nil || !perm.CanRead || perm.IsBlocked {
			util.Error(w, http.StatusForbidden, "no access")
			return
		}
	}

	// S3 fix: verify that the file belongs to the share
	file, err := d.Shares.GetFile(fileID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "file not found")
		return
	}
	if file.DirectoryID != shareID {
		util.Error(w, http.StatusBadRequest, "file does not belong to this share")
		return
	}

	seederIDs, err := d.SwarmSeeds.ListSeeders(fileID)
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to list seeders")
		return
	}

	// Owner is always a potential source (if online)
	ownerIncluded := false
	for _, id := range seederIDs {
		if id == dir.OwnerID {
			ownerIncluded = true
			break
		}
	}
	if !ownerIncluded {
		seederIDs = append(seederIDs, dir.OwnerID)
	}

	type seeder struct {
		UserID string `json:"user_id"`
		Online bool   `json:"online"`
	}
	var seeders []seeder
	online := 0
	for _, id := range seederIDs {
		isOnline := d.Hub.IsUserOnline(id)
		seeders = append(seeders, seeder{UserID: id, Online: isOnline})
		if isOnline {
			online++
		}
	}

	util.JSON(w, http.StatusOK, map[string]any{
		"seeders": seeders,
		"total":   len(seeders),
		"online":  online,
	})
}

// SwarmCounts — seed counts pro soubory v share
func (d *Deps) SwarmCounts(w http.ResponseWriter, r *http.Request) {
	if !d.SwarmSharingEnabled {
		util.Error(w, http.StatusForbidden, "swarm sharing is disabled")
		return
	}

	user := auth.GetUser(r)
	shareID := r.PathValue("id")

	dir, err := d.Shares.GetDirectory(shareID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "share not found")
		return
	}

	// Verify access
	if dir.OwnerID != user.ID {
		perm, err := d.Shares.GetEffectivePermission(shareID, user.ID)
		if err != nil || !perm.CanRead || perm.IsBlocked {
			util.Error(w, http.StatusForbidden, "no access")
			return
		}
	}

	counts, err := d.SwarmSeeds.ListFileSeederCounts(shareID)
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to list counts")
		return
	}
	if counts == nil {
		counts = make(map[string]int)
	}

	util.JSON(w, http.StatusOK, map[string]any{
		"counts": counts,
	})
}

// SwarmRequest — initiate multi-source download
func (d *Deps) SwarmRequest(w http.ResponseWriter, r *http.Request) {
	if !d.SwarmSharingEnabled {
		util.Error(w, http.StatusForbidden, "swarm sharing is disabled")
		return
	}

	user := auth.GetUser(r)
	shareID := r.PathValue("id")

	dir, err := d.Shares.GetDirectory(shareID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "share not found")
		return
	}

	// S6 fix: kontrola isActive
	if !dir.IsActive {
		util.Error(w, http.StatusGone, "share is not active")
		return
	}

	// Verify access
	if dir.OwnerID != user.ID {
		perm, err := d.Shares.GetEffectivePermission(shareID, user.ID)
		if err != nil || !perm.CanRead || perm.IsBlocked {
			util.Error(w, http.StatusForbidden, "no access")
			return
		}
	}

	var req struct {
		FileID string `json:"file_id"`
	}
	if err := util.DecodeJSON(r, &req); err != nil || req.FileID == "" {
		util.Error(w, http.StatusBadRequest, "file_id is required")
		return
	}

	file, err := d.Shares.GetFile(req.FileID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "file not found")
		return
	}
	if file.DirectoryID != shareID {
		util.Error(w, http.StatusBadRequest, "file does not belong to this share")
		return
	}

	// Find online seeders
	seederIDs, err := d.SwarmSeeds.ListSeeders(req.FileID)
	if err != nil {
		seederIDs = nil
	}

	// Owner is always a potential source
	ownerIncluded := false
	for _, id := range seederIDs {
		if id == dir.OwnerID {
			ownerIncluded = true
			break
		}
	}
	if !ownerIncluded {
		seederIDs = append(seederIDs, dir.OwnerID)
	}

	type sourceInfo struct {
		UserID string `json:"user_id"`
		Online bool   `json:"online"`
	}
	var sources []sourceInfo
	for _, id := range seederIDs {
		isOnline := d.Hub.IsUserOnline(id)
		if isOnline && id != user.ID {
			sources = append(sources, sourceInfo{UserID: id, Online: true})
		}
	}

	const pieceSize = 256 * 1024 // 256KB
	totalPieces := int((file.FileSize + int64(pieceSize) - 1) / int64(pieceSize))
	if totalPieces == 0 {
		totalPieces = 1
	}

	transferID, _ := uuid.NewV7()

	// Notify seeders via WS — swarm.download_notify (auto-seed registration)
	for _, src := range sources {
		event, _ := ws.NewEvent(ws.EventSwarmDownloadNotify, map[string]any{
			"transfer_id":   transferID.String(),
			"directory_id":  shareID,
			"file_id":       file.ID,
			"file_name":     file.FileName,
			"file_size":     file.FileSize,
			"file_hash":     file.FileHash,
			"relative_path": file.RelativePath,
			"requester_id":  user.ID,
			"piece_size":    pieceSize,
			"total_pieces":  totalPieces,
		})
		d.Hub.BroadcastToUser(src.UserID, event)
	}

	util.JSON(w, http.StatusOK, map[string]any{
		"transfer_id":  transferID.String(),
		"sources":      sources,
		"piece_size":   pieceSize,
		"total_pieces": totalPieces,
		"file_size":    file.FileSize,
		"file_name":    file.FileName,
	})
}
