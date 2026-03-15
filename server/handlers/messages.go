package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"nora/auth"
	"nora/database/queries"
	"nora/linkpreview"
	"nora/models"
	"nora/util"
	"nora/ws"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

type attachmentRequest struct {
	Filename    string `json:"filename"`
	URL         string `json:"url"`
	MimeType    string `json:"mime_type"`
	Size        int64  `json:"size"`
	ContentHash string `json:"content_hash"`
}

type pollRequest struct {
	Question  string     `json:"question"`
	PollType  string     `json:"poll_type"`
	Options   []string   `json:"options"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

type createMessageRequest struct {
	Content     string              `json:"content"`
	Attachments []attachmentRequest `json:"attachments,omitempty"`
	ReplyToID   string              `json:"reply_to_id,omitempty"`
	Poll        *pollRequest        `json:"poll,omitempty"`
}

type updateMessageRequest struct {
	Content string `json:"content"`
}

type pinMessageRequest struct {
	Pinned bool `json:"pinned"`
}

func (d *Deps) ListMessages(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	channelID := r.PathValue("id")

	// Per-channel read permission check
	if err := d.requireChannelPermission(user, channelID, models.PermRead); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	before := r.URL.Query().Get("before")
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 100 {
		limit = 100
	}

	callerPos, _ := d.Roles.GetHighestPosition(user.ID, user.IsOwner)
	messages, err := d.Messages.ListByChannel(channelID, before, limit, callerPos)
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to list messages")
		return
	}
	if messages == nil {
		messages = []models.Message{}
	}

	// Load attachments
	if len(messages) > 0 {
		ids := make([]string, len(messages))
		for i, m := range messages {
			ids[i] = m.ID
		}
		attMap, _ := d.Attachments.ListByMessageIDs(ids)
		for i := range messages {
			if atts, ok := attMap[messages[i].ID]; ok {
				for j := range atts {
					if strings.HasPrefix(atts[j].Filepath, "/api/") {
						atts[j].URL = atts[j].Filepath
					} else {
						atts[j].URL = "/api/uploads/" + atts[j].Filepath
					}
				}
				messages[i].Attachments = atts
			}
		}

		// Load reactions
		reactMap, _ := d.Reactions.ListByMessageIDs(ids)
		for i := range messages {
			if reacts, ok := reactMap[messages[i].ID]; ok {
				messages[i].Reactions = reacts
			}
		}

		// Load polls
		pollMap, _ := d.Polls.GetByMessageIDs(ids)
		for i := range messages {
			if poll, ok := pollMap[messages[i].ID]; ok {
				messages[i].Poll = poll
			}
		}

		// Load link previews
		previewMap, _ := d.LinkPreviews.GetByMessageIDs(ids)
		for i := range messages {
			if lp, ok := previewMap[messages[i].ID]; ok {
				messages[i].LinkPreview = lp
			}
		}

		// Load reply counts (threads)
		replyMap, _ := d.Messages.BatchCountReplies(ids)
		for i := range messages {
			if cnt, ok := replyMap[messages[i].ID]; ok {
				messages[i].ReplyCount = cnt
			}
		}
	}

	util.JSON(w, http.StatusOK, messages)
}

func (d *Deps) CreateMessage(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	channelID := r.PathValue("id")

	// Verify the channel exists
	ch, err := d.Channels.GetByID(channelID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "channel not found")
		return
	}

	if err := d.requireChannelPermission(user, channelID, models.PermSendMessages); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	// Quarantine enforcement
	if err := d.checkQuarantine(user.ID, "send_messages"); err != nil {
		util.Error(w, http.StatusForbidden, err.Error())
		return
	}

	// Slow mode enforcement — owners and admins bypass
	if ch.SlowModeSeconds > 0 && !user.IsOwner {
		perms, _ := d.Roles.GetUserPermissions(user.ID)
		if perms&models.PermAdmin == 0 {
			lastTime, err := d.Messages.GetLastMessageTime(channelID, user.ID)
			if err == nil {
				elapsed := time.Since(lastTime)
				cooldown := time.Duration(ch.SlowModeSeconds) * time.Second
				if elapsed < cooldown {
					remaining := int(cooldown.Seconds() - elapsed.Seconds())
					util.Error(w, http.StatusTooManyRequests, fmt.Sprintf("slow mode: wait %d seconds", remaining))
					return
				}
			}
		}
	}

	var req createMessageRequest
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Empty content is OK if there are attachments or a poll
	if len(req.Attachments) == 0 && req.Poll == nil {
		if msg := util.ValidateMessageContent(req.Content); msg != "" {
			util.Error(w, http.StatusBadRequest, msg)
			return
		}
	}

	// Auto-moderation — owner and admin bypass
	var automodHide bool
	if d.AutoMod != nil && d.AutoMod.Enabled && !user.IsOwner {
		perms, _ := d.Roles.GetUserPermissions(user.ID)
		if perms&models.PermAdmin == 0 {
			if blocked, matched := d.AutoMod.CheckContent(req.Content); blocked {
				d.logAudit("system", "automod.word_filter", "message", "", map[string]string{
					"user_id": user.ID,
					"matched": matched,
				})
				automodHide = true
			}
			if d.AutoMod.TrackMessage(user.ID) {
				timeoutSec := d.AutoMod.SpamTimeoutSeconds
				if timeoutSec > 0 && d.AutoMod.OwnerID != "" {
					toID, _ := uuid.NewV7()
					timeout := &models.Timeout{
						ID:        toID.String(),
						UserID:    user.ID,
						Reason:    "auto-moderation: spam detected",
						IssuedBy:  d.AutoMod.OwnerID,
						ExpiresAt: time.Now().Add(time.Duration(timeoutSec) * time.Second),
					}
					d.Timeouts.Create(timeout)
					d.RefreshTokens.DeleteByUserID(user.ID)
					timeoutEvt, _ := ws.NewEvent(ws.EventMemberTimeout, map[string]string{"id": user.ID})
					d.Hub.Broadcast(timeoutEvt)
					d.logAudit("system", "automod.spam_timeout", "user", user.ID, map[string]any{
						"timeout_seconds": timeoutSec,
					})
				}
				util.Error(w, http.StatusTooManyRequests, "spam detected, you have been timed out")
				return
			}
		}
	}

	// Poll validation
	if req.Poll != nil {
		if req.Poll.Question == "" || len(req.Poll.Question) > 500 {
			util.Error(w, http.StatusBadRequest, "poll question required (max 500 chars)")
			return
		}
		if len(req.Poll.Options) < 2 || len(req.Poll.Options) > 10 {
			util.Error(w, http.StatusBadRequest, "poll needs 2-10 options")
			return
		}
		validTypes := map[string]bool{"simple": true, "multi": true, "anonymous": true}
		if !validTypes[req.Poll.PollType] {
			util.Error(w, http.StatusBadRequest, "invalid poll type")
			return
		}
		for _, opt := range req.Poll.Options {
			if opt == "" || len(opt) > 200 {
				util.Error(w, http.StatusBadRequest, "option label required (max 200 chars)")
				return
			}
		}
	}

	id, _ := uuid.NewV7()
	msg := &models.Message{
		ID:        id.String(),
		ChannelID: channelID,
		UserID:    user.ID,
		Content:   req.Content,
		CreatedAt: time.Now().UTC(),
	}

	// Reply validation
	if req.ReplyToID != "" {
		replyMsg, err := d.Messages.GetByID(req.ReplyToID)
		if err != nil || replyMsg.ChannelID != channelID {
			util.Error(w, http.StatusBadRequest, "invalid reply_to_id")
			return
		}
		msg.ReplyToID = &req.ReplyToID
	}

	if err := d.Messages.Create(msg); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to create message")
		return
	}

	// Auto-moderation: hide message (word filter match)
	if automodHide {
		d.Messages.SetHidden(msg.ID, true, d.AutoMod.OwnerID, 999)
		msg.IsHidden = true
		msg.HiddenBy = &d.AutoMod.OwnerID
		msg.HiddenByPosition = 999
	}

	// Save attachments (deduplication already handled in upload handler)
	// Allowed URL prefixes for attachments
	allowedPrefixes := []string{"/api/uploads/", "/api/gameservers/"}
	for _, a := range req.Attachments {
		// Validate URL — must start with an allowed prefix
		urlValid := false
		for _, prefix := range allowedPrefixes {
			if strings.HasPrefix(a.URL, prefix) {
				urlValid = true
				break
			}
		}
		if !urlValid {
			util.Error(w, http.StatusBadRequest, "invalid attachment URL")
			return
		}

		attID, _ := uuid.NewV7()

		// Distinguish upload files vs. external URLs (game server files etc.)
		fp := a.URL
		isUpload := strings.HasPrefix(a.URL, "/api/uploads/")
		if isUpload {
			fp = a.URL[len("/api/uploads/"):]
		}

		// Verify content_hash only for upload files (not for game server files)
		contentHash := a.ContentHash
		if isUpload && contentHash != "" && len(contentHash) == 64 {
			if f, err := os.Open(filepath.Join(d.UploadsDir, fp)); err == nil {
				h := sha256.New()
				io.Copy(h, f)
				f.Close()
				actual := hex.EncodeToString(h.Sum(nil))
				if actual != contentHash {
					contentHash = actual
				}
			}
		}

		attURL := a.URL
		if isUpload {
			attURL = "/api/uploads/" + fp
		}

		att := &models.Attachment{
			ID:          attID.String(),
			MessageID:   msg.ID,
			Filepath:    fp,
			Filename:    a.Filename,
			MimeType:    a.MimeType,
			Size:        a.Size,
			URL:         attURL,
			ContentHash: contentHash,
		}
		if err := d.Attachments.Create(att); err == nil {
			msg.Attachments = append(msg.Attachments, *att)
		}
	}

	// Create poll if included in the request
	if req.Poll != nil {
		pollID, _ := uuid.NewV7()
		poll := &models.Poll{
			ID:        pollID.String(),
			MessageID: msg.ID,
			Question:  req.Poll.Question,
			PollType:  req.Poll.PollType,
			CreatedAt: time.Now().UTC(),
			ExpiresAt: req.Poll.ExpiresAt,
		}
		if err := d.Polls.Create(poll); err == nil {
			for i, label := range req.Poll.Options {
				optID, _ := uuid.NewV7()
				opt := &models.PollOption{
					ID:       optID.String(),
					PollID:   poll.ID,
					Label:    label,
					Position: i,
				}
				d.Polls.CreateOption(opt)
				poll.Options = append(poll.Options, *opt)
			}
			msg.Poll = poll
		}
	}

	// Load message with author for response + broadcast
	author, _ := d.Users.GetByID(user.ID)
	if author != nil {
		msg.Author = author
	}

	// Load reply_to data for broadcast
	if msg.ReplyToID != nil {
		if replyMsg, err := d.Messages.GetByID(*msg.ReplyToID); err == nil {
			msg.ReplyTo = &models.Message{
				ID:      replyMsg.ID,
				Content: replyMsg.Content,
				Author:  replyMsg.Author,
			}
		}
	}

	// For automod-hidden messages: broadcast with stripped content.
	// Admins will see the content when loading messages from API (server returns full content to them).
	if automodHide {
		stripped := *msg
		stripped.Content = ""
		stripped.Attachments = nil
		stripped.Poll = nil
		stripped.LinkPreview = nil
		event, _ := ws.NewEvent(ws.EventMessageNew, &stripped)
		d.broadcastToChannelReaders(channelID, event)
	} else {
		event, _ := ws.NewEvent(ws.EventMessageNew, msg)
		d.broadcastToChannelReaders(channelID, event)
	}

	// Asynchronously fetch link preview from the first URL in the message
	go d.fetchAndStoreLinkPreview(msg.ID, msg.Content)

	// HTTP response: for the author (regular user) also stripped content
	if automodHide {
		msg.Content = ""
		msg.Attachments = nil
		msg.Poll = nil
		msg.LinkPreview = nil
	}
	util.JSON(w, http.StatusCreated, msg)
}

func (d *Deps) UpdateMessage(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	msgID := r.PathValue("id")

	ownerID, err := d.Messages.GetOwnerID(msgID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "message not found")
		return
	}

	// Owner or MANAGE_MESSAGES
	if ownerID != user.ID {
		if err := d.requirePermission(user, models.PermManageMessages); err != nil {
			util.Error(w, http.StatusForbidden, "insufficient permissions")
			return
		}
	}

	var req updateMessageRequest
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if msg := util.ValidateMessageContent(req.Content); msg != "" {
		util.Error(w, http.StatusBadRequest, msg)
		return
	}

	// Save old content to edit history
	oldMsg, err := d.Messages.GetByID(msgID)
	if err == nil && oldMsg.Content != req.Content {
		d.Messages.SaveEditHistory(msgID, oldMsg.Content, user.ID)
	}

	if err := d.Messages.Update(msgID, req.Content); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to update message")
		return
	}

	event, _ := ws.NewEvent(ws.EventMessageEdit, map[string]string{
		"id":      msgID,
		"content": req.Content,
	})
	if oldMsg != nil {
		d.broadcastToChannelReaders(oldMsg.ChannelID, event)
	} else {
		d.Hub.Broadcast(event)
	}

	// Re-fetch link preview after edit
	go d.fetchAndStoreLinkPreview(msgID, req.Content)

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// GetMessageEditHistory returns the edit history of a message.
func (d *Deps) GetMessageEditHistory(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	msgID := r.PathValue("id")

	// Get message to check channel permission
	msg, err := d.Messages.GetByID(msgID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "message not found")
		return
	}
	if err := d.requireChannelPermission(user, msg.ChannelID, models.PermRead); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	edits, err := d.Messages.GetEditHistory(msgID)
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to get edit history")
		return
	}
	if edits == nil {
		edits = []models.MessageEdit{}
	}

	util.JSON(w, http.StatusOK, edits)
}

func (d *Deps) DeleteMessage(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	msgID := r.PathValue("id")

	ownerID, err := d.Messages.GetOwnerID(msgID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "message not found")
		return
	}

	if ownerID != user.ID {
		if err := d.requirePermission(user, models.PermManageMessages); err != nil {
			util.Error(w, http.StatusForbidden, "insufficient permissions")
			return
		}
		if err := d.canActOn(user, ownerID); err != nil {
			util.Error(w, http.StatusForbidden, "cannot delete messages from higher-ranked users")
			return
		}
	}

	// Get channel ID before deletion for scoped broadcast
	msgChannelID, _ := d.Messages.GetChannelID(msgID)

	// Load attachments before deletion (CASCADE deletes attachment rows from DB)
	atts, _ := d.Attachments.ListByMessageID(msgID)

	if err := d.Messages.Delete(msgID); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to delete message")
		return
	}

	// Delete files from disk (only upload files, not external URLs)
	for _, a := range atts {
		if strings.HasPrefix(a.Filepath, "/api/") {
			continue // external URL (game server files etc.) — do not delete
		}
		count, err := d.Attachments.CountByFilepath(a.Filepath)
		if err != nil || count > 0 {
			continue
		}
		p := filepath.Join(d.UploadsDir, a.Filepath)
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			slog.Error("failed to delete attachment file", "path", p, "error", err)
		}
	}

	event, _ := ws.NewEvent(ws.EventMessageDelete, map[string]string{"id": msgID})
	if msgChannelID != "" {
		d.broadcastToChannelReaders(msgChannelID, event)
	} else {
		d.Hub.Broadcast(event)
	}

	// Log only moderation deletes (not own messages)
	if ownerID != user.ID {
		d.logAudit(user.ID, "message.delete", "message", msgID, map[string]string{"author_id": ownerID})
	}

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (d *Deps) PinMessage(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	msgID := r.PathValue("id")

	// Verify permissions
	if err := d.requirePermission(user, models.PermManageMessages); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	msg, err := d.Messages.GetByID(msgID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "message not found")
		return
	}

	// Hierarchy: if pinner is not the author → canActOn
	if msg.UserID != user.ID {
		if err := d.canActOn(user, msg.UserID); err != nil {
			util.Error(w, http.StatusForbidden, "cannot pin messages from higher-ranked users")
			return
		}
	}

	var req pinMessageRequest
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := d.Messages.SetPinned(msgID, req.Pinned, user.ID); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to update pin")
		return
	}

	evtType := ws.EventMessagePin
	if !req.Pinned {
		evtType = ws.EventMessageUnpin
	}
	event, _ := ws.NewEvent(evtType, map[string]any{
		"id":         msgID,
		"channel_id": msg.ChannelID,
		"is_pinned":  req.Pinned,
		"pinned_by":  user.ID,
	})
	d.Hub.Broadcast(event)

	action := "message.pin"
	if !req.Pinned {
		action = "message.unpin"
	}
	d.logAudit(user.ID, action, "message", msgID, map[string]string{"channel_id": msg.ChannelID})

	util.JSON(w, http.StatusOK, map[string]any{"status": "ok", "is_pinned": req.Pinned})
}

type hideMessageRequest struct {
	Hidden bool `json:"hidden"`
}

func (d *Deps) HideMessage(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	msgID := r.PathValue("id")

	if err := d.requirePermission(user, models.PermManageMessages); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	msg, err := d.Messages.GetByID(msgID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "message not found")
		return
	}

	if msg.UserID != user.ID {
		if err := d.canActOn(user, msg.UserID); err != nil {
			util.Error(w, http.StatusForbidden, "cannot hide messages from higher-ranked users")
			return
		}
	}

	var req hideMessageRequest
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	actorPos, _ := d.Roles.GetHighestPosition(user.ID, user.IsOwner)

	if err := d.Messages.SetHidden(msgID, req.Hidden, user.ID, actorPos); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to hide message")
		return
	}

	hidePayload := map[string]any{
		"id":                 msgID,
		"channel_id":         msg.ChannelID,
		"is_hidden":          req.Hidden,
		"hidden_by_position": actorPos,
	}
	// On unhide, include content so clients can display the message
	if !req.Hidden {
		hidePayload["content"] = msg.Content
	}
	event, _ := ws.NewEvent(ws.EventMessageHide, hidePayload)
	d.Hub.Broadcast(event)

	action := "message.hide"
	if !req.Hidden {
		action = "message.unhide"
	}
	d.logAudit(user.ID, action, "message", msgID, map[string]string{"author_id": msg.UserID})

	util.JSON(w, http.StatusOK, map[string]any{"status": "ok", "is_hidden": req.Hidden})
}

func (d *Deps) HideUserMessages(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	targetID := r.PathValue("id")

	if err := d.requirePermission(user, models.PermManageMessages); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	if err := d.canActOn(user, targetID); err != nil {
		util.Error(w, http.StatusForbidden, "cannot hide messages from higher-ranked users")
		return
	}

	actorPos, _ := d.Roles.GetHighestPosition(user.ID, user.IsOwner)

	if err := d.Messages.HideByUserID(targetID, user.ID, actorPos); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to hide messages")
		return
	}

	event, _ := ws.NewEvent(ws.EventMessagesHide, map[string]any{
		"user_id":            targetID,
		"hidden_by_position": actorPos,
	})
	d.Hub.Broadcast(event)

	d.logAudit(user.ID, "messages.hide", "user", targetID, nil)

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (d *Deps) DeleteUserMessages(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	targetID := r.PathValue("id")

	if err := d.requirePermission(user, models.PermManageMessages); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	if err := d.canActOn(user, targetID); err != nil {
		util.Error(w, http.StatusForbidden, "cannot delete messages from higher-ranked users")
		return
	}

	// Load attachments before deletion (CASCADE deletes attachment rows from DB)
	atts, _ := d.Attachments.ListByUserMessages(targetID)

	if err := d.Messages.DeleteByUserID(targetID); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to delete messages")
		return
	}

	// Delete files from disk (with ref counting, skip external URLs)
	for _, a := range atts {
		if strings.HasPrefix(a.Filepath, "/api/") {
			continue
		}
		count, err := d.Attachments.CountByFilepath(a.Filepath)
		if err != nil || count > 0 {
			continue
		}
		p := filepath.Join(d.UploadsDir, a.Filepath)
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			slog.Error("failed to delete attachment file", "path", p, "error", err)
		}
	}

	event, _ := ws.NewEvent(ws.EventMessagesBulkDelete, map[string]string{
		"user_id": targetID,
	})
	d.Hub.Broadcast(event)

	d.logAudit(user.ID, "messages.bulk_delete", "user", targetID, nil)

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (d *Deps) SearchMessages(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	channelID := r.PathValue("id")

	// Per-channel read permission check
	if err := d.requireChannelPermission(user, channelID, models.PermRead); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	query := r.URL.Query().Get("q")
	if query == "" {
		util.Error(w, http.StatusBadRequest, "missing search query")
		return
	}

	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 100 {
		limit = 100
	}
	offset := 0
	if o := r.URL.Query().Get("offset"); o != "" {
		if n, err := strconv.Atoi(o); err == nil {
			offset = n
		}
	}

	// Parse extended filters from the query string
	textQuery, filters := parseSearchFilters(query)

	callerPos, _ := d.Roles.GetHighestPosition(user.ID, user.IsOwner)
	messages, err := d.Messages.Search(channelID, textQuery, limit, offset, callerPos, filters)
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to search messages")
		return
	}
	if messages == nil {
		messages = []models.Message{}
	}

	// Load attachments, reactions, polls
	if len(messages) > 0 {
		ids := make([]string, len(messages))
		for i, m := range messages {
			ids[i] = m.ID
		}
		attMap, _ := d.Attachments.ListByMessageIDs(ids)
		for i := range messages {
			if atts, ok := attMap[messages[i].ID]; ok {
				for j := range atts {
					if strings.HasPrefix(atts[j].Filepath, "/api/") {
						atts[j].URL = atts[j].Filepath
					} else {
						atts[j].URL = "/api/uploads/" + atts[j].Filepath
					}
				}
				messages[i].Attachments = atts
			}
		}

		reactMap, _ := d.Reactions.ListByMessageIDs(ids)
		for i := range messages {
			if reacts, ok := reactMap[messages[i].ID]; ok {
				messages[i].Reactions = reacts
			}
		}

		pollMap, _ := d.Polls.GetByMessageIDs(ids)
		for i := range messages {
			if poll, ok := pollMap[messages[i].ID]; ok {
				messages[i].Poll = poll
			}
		}

		previewMap, _ := d.LinkPreviews.GetByMessageIDs(ids)
		for i := range messages {
			if lp, ok := previewMap[messages[i].ID]; ok {
				messages[i].LinkPreview = lp
			}
		}
	}

	util.JSON(w, http.StatusOK, messages)
}

// parseSearchFilters parses extended filters from the search query.
// Supported filters: from:username, has:image, has:link, has:file, before:YYYY-MM-DD, after:YYYY-MM-DD.
// Returns the text part of the query (without filters) and a filter struct.
func parseSearchFilters(raw string) (string, *queries.SearchFilters) {
	filters := &queries.SearchFilters{}
	hasFilter := false

	// Split into tokens and separate filters from text
	var textParts []string
	parts := strings.Fields(raw)
	for _, part := range parts {
		lower := strings.ToLower(part)
		switch {
		case strings.HasPrefix(lower, "from:"):
			filters.FromUsername = part[5:]
			hasFilter = true
		case lower == "has:image":
			filters.HasImage = true
			hasFilter = true
		case lower == "has:link":
			filters.HasLink = true
			hasFilter = true
		case lower == "has:file":
			filters.HasFile = true
			hasFilter = true
		case strings.HasPrefix(lower, "before:"):
			date := part[7:]
			// Validate YYYY-MM-DD format
			if len(date) == 10 && date[4] == '-' && date[7] == '-' {
				filters.Before = date
				hasFilter = true
			} else {
				textParts = append(textParts, part)
			}
		case strings.HasPrefix(lower, "after:"):
			date := part[6:]
			// Validate YYYY-MM-DD format
			if len(date) == 10 && date[4] == '-' && date[7] == '-' {
				filters.After = date
				hasFilter = true
			} else {
				textParts = append(textParts, part)
			}
		default:
			textParts = append(textParts, part)
		}
	}

	if !hasFilter {
		filters = nil
	}

	return strings.Join(textParts, " "), filters
}

func (d *Deps) ListPinnedMessages(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	channelID := r.PathValue("id")

	if err := d.requirePermission(user, models.PermRead); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	messages, err := d.Messages.ListPinned(channelID)
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to list pinned messages")
		return
	}
	if messages == nil {
		messages = []models.Message{}
	}

	// Load attachments
	if len(messages) > 0 {
		ids := make([]string, len(messages))
		for i, m := range messages {
			ids[i] = m.ID
		}
		attMap, _ := d.Attachments.ListByMessageIDs(ids)
		for i := range messages {
			if atts, ok := attMap[messages[i].ID]; ok {
				for j := range atts {
					if strings.HasPrefix(atts[j].Filepath, "/api/") {
						atts[j].URL = atts[j].Filepath
					} else {
						atts[j].URL = "/api/uploads/" + atts[j].Filepath
					}
				}
				messages[i].Attachments = atts
			}
		}

		reactMap, _ := d.Reactions.ListByMessageIDs(ids)
		for i := range messages {
			if reacts, ok := reactMap[messages[i].ID]; ok {
				messages[i].Reactions = reacts
			}
		}

		pollMap, _ := d.Polls.GetByMessageIDs(ids)
		for i := range messages {
			if poll, ok := pollMap[messages[i].ID]; ok {
				messages[i].Poll = poll
			}
		}

		previewMap, _ := d.LinkPreviews.GetByMessageIDs(ids)
		for i := range messages {
			if lp, ok := previewMap[messages[i].ID]; ok {
				messages[i].LinkPreview = lp
			}
		}
	}

	util.JSON(w, http.StatusOK, messages)
}

func (d *Deps) GetMessageThread(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	msgID := r.PathValue("id")

	// Load parent message
	parent, err := d.Messages.GetByID(msgID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "message not found")
		return
	}

	// Per-channel read permission check
	if err := d.requireChannelPermission(user, parent.ChannelID, models.PermRead); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	callerPos, _ := d.Roles.GetHighestPosition(user.ID, user.IsOwner)

	// Load replies
	replies, err := d.Messages.ListReplies(msgID, callerPos)
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to list replies")
		return
	}

	// Build result: parent + replies
	result := make([]models.Message, 0, 1+len(replies))
	result = append(result, *parent)
	result = append(result, replies...)

	// Enrichment — attachments, reactions, polls, link previews
	ids := make([]string, len(result))
	for i, m := range result {
		ids[i] = m.ID
	}

	attMap, _ := d.Attachments.ListByMessageIDs(ids)
	for i := range result {
		if atts, ok := attMap[result[i].ID]; ok {
			for j := range atts {
				if strings.HasPrefix(atts[j].Filepath, "/api/") {
					atts[j].URL = atts[j].Filepath
				} else {
					atts[j].URL = "/api/uploads/" + atts[j].Filepath
				}
			}
			result[i].Attachments = atts
		}
	}

	reactMap, _ := d.Reactions.ListByMessageIDs(ids)
	for i := range result {
		if reacts, ok := reactMap[result[i].ID]; ok {
			result[i].Reactions = reacts
		}
	}

	pollMap, _ := d.Polls.GetByMessageIDs(ids)
	for i := range result {
		if poll, ok := pollMap[result[i].ID]; ok {
			result[i].Poll = poll
		}
	}

	previewMap, _ := d.LinkPreviews.GetByMessageIDs(ids)
	for i := range result {
		if lp, ok := previewMap[result[i].ID]; ok {
			result[i].LinkPreview = lp
		}
	}

	util.JSON(w, http.StatusOK, result)
}

var urlRe = regexp.MustCompile(`https?://[^\s<>\]\)]+`)

// fetchAndStoreLinkPreview asynchronously fetches OG metadata from the first URL in the message.
func (d *Deps) fetchAndStoreLinkPreview(msgID, content string) {
	match := urlRe.FindString(content)
	if match == "" {
		return
	}

	result, err := linkpreview.Fetch(match)
	if err != nil || result == nil {
		return
	}

	id, _ := uuid.NewV7()
	lp := &models.LinkPreview{
		ID:          id.String(),
		MessageID:   msgID,
		URL:         result.URL,
		Title:       result.Title,
		Description: result.Description,
		ImageURL:    result.ImageURL,
		SiteName:    result.SiteName,
	}

	if err := d.LinkPreviews.Create(lp); err != nil {
		slog.Error("link preview: save failed", "message_id", msgID, "error", err)
		return
	}

	event, _ := ws.NewEvent(ws.EventMessageLinkPreview, map[string]any{
		"message_id": msgID,
		"preview":    lp,
	})
	d.Hub.Broadcast(event)
}
