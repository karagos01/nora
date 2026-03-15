package handlers

import (
	"net/http"
	"nora/auth"
	"nora/models"
	"nora/util"
	"nora/ws"
	"time"

	"github.com/google/uuid"
)

// ListWhiteboards — GET /api/whiteboards
func (d *Deps) ListWhiteboards(w http.ResponseWriter, r *http.Request) {
	boards, err := d.Whiteboards.List()
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to list whiteboards")
		return
	}
	if boards == nil {
		boards = []models.Whiteboard{}
	}
	util.JSON(w, http.StatusOK, boards)
}

// CreateWhiteboard — POST /api/whiteboards
func (d *Deps) CreateWhiteboard(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)

	var req struct {
		Name      string  `json:"name"`
		ChannelID *string `json:"channel_id,omitempty"`
	}
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" || len(req.Name) > 64 {
		util.Error(w, http.StatusBadRequest, "name must be 1-64 characters")
		return
	}

	if req.ChannelID != nil {
		if _, err := d.Channels.GetByID(*req.ChannelID); err != nil {
			util.Error(w, http.StatusNotFound, "channel not found")
			return
		}
	}

	id, _ := uuid.NewV7()
	wb := &models.Whiteboard{
		ID:        id.String(),
		Name:      req.Name,
		ChannelID: req.ChannelID,
		CreatorID: user.ID,
		CreatedAt: time.Now().UTC(),
	}

	if err := d.Whiteboards.Create(wb); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to create whiteboard")
		return
	}

	util.JSON(w, http.StatusCreated, wb)
}

// DeleteWhiteboard — DELETE /api/whiteboards/{id}
func (d *Deps) DeleteWhiteboard(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	boardID := r.PathValue("id")

	wb, err := d.Whiteboards.GetByID(boardID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "whiteboard not found")
		return
	}

	// Jen creator nebo admin/owner
	if wb.CreatorID != user.ID {
		if err := d.requirePermission(user, models.PermAdmin); err != nil {
			util.Error(w, http.StatusForbidden, "only creator or admin can delete whiteboard")
			return
		}
	}

	if err := d.Whiteboards.Delete(boardID); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to delete whiteboard")
		return
	}

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// GetWhiteboardStrokes — GET /api/whiteboards/{id}/strokes
func (d *Deps) GetWhiteboardStrokes(w http.ResponseWriter, r *http.Request) {
	boardID := r.PathValue("id")

	if _, err := d.Whiteboards.GetByID(boardID); err != nil {
		util.Error(w, http.StatusNotFound, "whiteboard not found")
		return
	}

	strokes, err := d.Whiteboards.GetStrokes(boardID)
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to get strokes")
		return
	}
	if strokes == nil {
		strokes = []models.WhiteboardStroke{}
	}

	util.JSON(w, http.StatusOK, strokes)
}

// AddWhiteboardStroke — POST /api/whiteboards/{id}/strokes
func (d *Deps) AddWhiteboardStroke(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.checkQuarantine(user.ID, "send_messages"); err != nil {
		util.Error(w, http.StatusForbidden, err.Error())
		return
	}

	boardID := r.PathValue("id")

	if _, err := d.Whiteboards.GetByID(boardID); err != nil {
		util.Error(w, http.StatusNotFound, "whiteboard not found")
		return
	}

	var req struct {
		PathData string `json:"path_data"`
		Color    string `json:"color"`
		Width    int    `json:"width"`
		Tool     string `json:"tool"`
	}
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.PathData == "" {
		util.Error(w, http.StatusBadRequest, "path_data is required")
		return
	}
	if req.Color == "" {
		req.Color = "#ffffff"
	}
	if req.Width < 1 || req.Width > 20 {
		req.Width = 2
	}
	if req.Tool != "pen" && req.Tool != "eraser" {
		req.Tool = "pen"
	}

	id, _ := uuid.NewV7()
	stroke := &models.WhiteboardStroke{
		ID:           id.String(),
		WhiteboardID: boardID,
		UserID:       user.ID,
		PathData:     req.PathData,
		Color:        req.Color,
		Width:        req.Width,
		Tool:         req.Tool,
		CreatedAt:    time.Now().UTC(),
		Username:     user.Username,
	}

	if err := d.Whiteboards.AddStroke(stroke); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to add stroke")
		return
	}

	event, _ := ws.NewEvent(ws.EventWhiteboardStroke, stroke)
	d.Hub.Broadcast(event)

	util.JSON(w, http.StatusCreated, stroke)
}

// UndoWhiteboardStroke — POST /api/whiteboards/{id}/undo
func (d *Deps) UndoWhiteboardStroke(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	boardID := r.PathValue("id")

	if _, err := d.Whiteboards.GetByID(boardID); err != nil {
		util.Error(w, http.StatusNotFound, "whiteboard not found")
		return
	}

	strokeID, err := d.Whiteboards.DeleteLastStrokeByUser(boardID, user.ID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "no strokes to undo")
		return
	}

	event, _ := ws.NewEvent(ws.EventWhiteboardUndo, map[string]string{
		"whiteboard_id": boardID,
		"stroke_id":     strokeID,
	})
	d.Hub.Broadcast(event)

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok", "stroke_id": strokeID})
}

// ClearWhiteboard — POST /api/whiteboards/{id}/clear
func (d *Deps) ClearWhiteboard(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	boardID := r.PathValue("id")

	wb, err := d.Whiteboards.GetByID(boardID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "whiteboard not found")
		return
	}

	// Jen creator nebo admin/owner
	if wb.CreatorID != user.ID {
		if err := d.requirePermission(user, models.PermAdmin); err != nil {
			util.Error(w, http.StatusForbidden, "only creator or admin can clear whiteboard")
			return
		}
	}

	if err := d.Whiteboards.ClearStrokes(boardID); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to clear whiteboard")
		return
	}

	event, _ := ws.NewEvent(ws.EventWhiteboardClear, map[string]string{
		"whiteboard_id": boardID,
	})
	d.Hub.Broadcast(event)

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
