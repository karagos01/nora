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

// ListKanbanBoards — GET /api/kanban
func (d *Deps) ListKanbanBoards(w http.ResponseWriter, r *http.Request) {
	boards, err := d.KanbanQ.ListBoards()
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to list boards")
		return
	}
	if boards == nil {
		boards = []models.KanbanBoard{}
	}
	util.JSON(w, http.StatusOK, boards)
}

// CreateKanbanBoard — POST /api/kanban
func (d *Deps) CreateKanbanBoard(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermSendMessages); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}
	if err := d.checkQuarantine(user.ID, "send_messages"); err != nil {
		util.Error(w, http.StatusForbidden, err.Error())
		return
	}

	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" || len(req.Name) > 64 {
		util.Error(w, http.StatusBadRequest, "name must be 1-64 characters")
		return
	}

	now := time.Now().UTC()
	boardID, _ := uuid.NewV7()
	board := &models.KanbanBoard{
		ID:          boardID.String(),
		Name:        req.Name,
		Description: req.Description,
		CreatorID:   user.ID,
		CreatedAt:   now,
	}

	if err := d.KanbanQ.CreateBoard(board); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to create board")
		return
	}

	// Default columns: To Do, In Progress, Done
	defaults := []struct {
		title string
		color string
		pos   int
	}{
		{"To Do", "#808080", 0},
		{"In Progress", "#3399ff", 1},
		{"Done", "#33cc33", 2},
	}
	for _, d2 := range defaults {
		colID, _ := uuid.NewV7()
		col := &models.KanbanColumn{
			ID:        colID.String(),
			BoardID:   board.ID,
			Title:     d2.title,
			Position:  d2.pos,
			Color:     d2.color,
			CreatedAt: now,
		}
		d.KanbanQ.CreateColumn(col)
	}

	// Vrátit board s columns
	fullBoard, _ := d.KanbanQ.GetBoard(board.ID)
	if fullBoard == nil {
		fullBoard = board
	}

	event, _ := ws.NewEvent(ws.EventKanbanBoardCreate, fullBoard)
	d.Hub.Broadcast(event)

	util.JSON(w, http.StatusCreated, fullBoard)
}

// GetKanbanBoard — GET /api/kanban/{id}
func (d *Deps) GetKanbanBoard(w http.ResponseWriter, r *http.Request) {
	boardID := r.PathValue("id")

	board, err := d.KanbanQ.GetBoard(boardID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "board not found")
		return
	}

	util.JSON(w, http.StatusOK, board)
}

// DeleteKanbanBoard — DELETE /api/kanban/{id}
func (d *Deps) DeleteKanbanBoard(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	boardID := r.PathValue("id")

	board, err := d.KanbanQ.GetBoard(boardID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "board not found")
		return
	}

	// Jen creator nebo admin/owner
	if board.CreatorID != user.ID {
		if err := d.requirePermission(user, models.PermManageChannels); err != nil {
			util.Error(w, http.StatusForbidden, "only creator or admin can delete board")
			return
		}
	}

	if err := d.KanbanQ.DeleteBoard(boardID); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to delete board")
		return
	}

	event, _ := ws.NewEvent(ws.EventKanbanBoardDelete, map[string]string{"id": boardID})
	d.Hub.Broadcast(event)

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// CreateKanbanColumn — POST /api/kanban/{id}/columns
func (d *Deps) CreateKanbanColumn(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermSendMessages); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}
	if err := d.checkQuarantine(user.ID, "send_messages"); err != nil {
		util.Error(w, http.StatusForbidden, err.Error())
		return
	}

	boardID := r.PathValue("id")

	if _, err := d.KanbanQ.GetBoard(boardID); err != nil {
		util.Error(w, http.StatusNotFound, "board not found")
		return
	}

	var req struct {
		Title string `json:"title"`
		Color string `json:"color"`
	}
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Title == "" || len(req.Title) > 64 {
		util.Error(w, http.StatusBadRequest, "title must be 1-64 characters")
		return
	}
	if req.Color == "" {
		req.Color = "#555555"
	}

	pos := d.KanbanQ.GetMaxColumnPosition(boardID) + 1
	colID, _ := uuid.NewV7()
	col := &models.KanbanColumn{
		ID:        colID.String(),
		BoardID:   boardID,
		Title:     req.Title,
		Position:  pos,
		Color:     req.Color,
		CreatedAt: time.Now().UTC(),
	}

	if err := d.KanbanQ.CreateColumn(col); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to create column")
		return
	}

	event, _ := ws.NewEvent(ws.EventKanbanColumnCreate, col)
	d.Hub.Broadcast(event)

	util.JSON(w, http.StatusCreated, col)
}

// UpdateKanbanColumn — PATCH /api/kanban/{id}/columns/{colId}
func (d *Deps) UpdateKanbanColumn(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermSendMessages); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	boardID := r.PathValue("id")
	colID := r.PathValue("colId")

	if _, err := d.KanbanQ.GetBoard(boardID); err != nil {
		util.Error(w, http.StatusNotFound, "board not found")
		return
	}

	col, err := d.KanbanQ.GetColumn(colID)
	if err != nil || col.BoardID != boardID {
		util.Error(w, http.StatusNotFound, "column not found")
		return
	}

	var req struct {
		Title string `json:"title"`
		Color string `json:"color"`
	}
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Title == "" {
		req.Title = col.Title
	}
	if req.Color == "" {
		req.Color = col.Color
	}

	if err := d.KanbanQ.UpdateColumn(colID, req.Title, req.Color); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to update column")
		return
	}

	col.Title = req.Title
	col.Color = req.Color

	event, _ := ws.NewEvent(ws.EventKanbanColumnUpdate, col)
	d.Hub.Broadcast(event)

	util.JSON(w, http.StatusOK, col)
}

// ReorderKanbanColumns — POST /api/kanban/{id}/columns/reorder
func (d *Deps) ReorderKanbanColumns(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermSendMessages); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	boardID := r.PathValue("id")

	if _, err := d.KanbanQ.GetBoard(boardID); err != nil {
		util.Error(w, http.StatusNotFound, "board not found")
		return
	}

	var req struct {
		ColumnIDs []string `json:"column_ids"`
	}
	if err := util.DecodeJSON(r, &req); err != nil || len(req.ColumnIDs) == 0 {
		util.Error(w, http.StatusBadRequest, "column_ids required")
		return
	}

	if err := d.KanbanQ.ReorderColumns(boardID, req.ColumnIDs); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to reorder columns")
		return
	}

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// DeleteKanbanColumn — DELETE /api/kanban/{id}/columns/{colId}
func (d *Deps) DeleteKanbanColumn(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermSendMessages); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	boardID := r.PathValue("id")
	colID := r.PathValue("colId")

	col, err := d.KanbanQ.GetColumn(colID)
	if err != nil || col.BoardID != boardID {
		util.Error(w, http.StatusNotFound, "column not found")
		return
	}

	if err := d.KanbanQ.DeleteColumn(colID); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to delete column")
		return
	}

	event, _ := ws.NewEvent(ws.EventKanbanColumnDelete, map[string]string{"id": colID, "board_id": boardID})
	d.Hub.Broadcast(event)

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// CreateKanbanCard — POST /api/kanban/{id}/cards
func (d *Deps) CreateKanbanCard(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermSendMessages); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}
	if err := d.checkQuarantine(user.ID, "send_messages"); err != nil {
		util.Error(w, http.StatusForbidden, err.Error())
		return
	}

	boardID := r.PathValue("id")

	if _, err := d.KanbanQ.GetBoard(boardID); err != nil {
		util.Error(w, http.StatusNotFound, "board not found")
		return
	}

	var req struct {
		ColumnID    string  `json:"column_id"`
		Title       string  `json:"title"`
		Description string  `json:"description"`
		Color       string  `json:"color"`
		AssignedTo  *string `json:"assigned_to,omitempty"`
	}
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Title == "" || len(req.Title) > 256 {
		util.Error(w, http.StatusBadRequest, "title must be 1-256 characters")
		return
	}
	if req.ColumnID == "" {
		util.Error(w, http.StatusBadRequest, "column_id is required")
		return
	}

	// Ověřit že sloupec patří boardu
	col, err := d.KanbanQ.GetColumn(req.ColumnID)
	if err != nil || col.BoardID != boardID {
		util.Error(w, http.StatusBadRequest, "column not found in this board")
		return
	}

	pos := d.KanbanQ.GetMaxCardPosition(req.ColumnID) + 1
	cardID, _ := uuid.NewV7()
	card := &models.KanbanCard{
		ID:          cardID.String(),
		ColumnID:    req.ColumnID,
		BoardID:     boardID,
		Title:       req.Title,
		Description: req.Description,
		Position:    pos,
		AssignedTo:  req.AssignedTo,
		CreatedBy:   user.ID,
		Color:       req.Color,
		CreatedAt:   time.Now().UTC(),
	}

	if err := d.KanbanQ.CreateCard(card); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to create card")
		return
	}

	// Přidat author info
	card.Author = &models.User{
		ID:       user.ID,
		Username: user.Username,
	}

	event, _ := ws.NewEvent(ws.EventKanbanCardCreate, card)
	d.Hub.Broadcast(event)

	util.JSON(w, http.StatusCreated, card)
}

// UpdateKanbanCard — PATCH /api/kanban/cards/{cardId}
func (d *Deps) UpdateKanbanCard(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermSendMessages); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	cardID := r.PathValue("cardId")

	card, err := d.KanbanQ.GetCard(cardID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "card not found")
		return
	}

	var req struct {
		Title       *string `json:"title,omitempty"`
		Description *string `json:"description,omitempty"`
		AssignedTo  *string `json:"assigned_to,omitempty"`
		Color       *string `json:"color,omitempty"`
		DueDate     *string `json:"due_date,omitempty"`
		ClearAssign bool    `json:"clear_assign,omitempty"`
		ClearDue    bool    `json:"clear_due,omitempty"`
	}
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Title != nil {
		card.Title = *req.Title
	}
	if req.Description != nil {
		card.Description = *req.Description
	}
	if req.Color != nil {
		card.Color = *req.Color
	}
	if req.AssignedTo != nil {
		card.AssignedTo = req.AssignedTo
	}
	if req.ClearAssign {
		card.AssignedTo = nil
	}
	if req.DueDate != nil && *req.DueDate != "" {
		t, err := time.Parse(time.RFC3339, *req.DueDate)
		if err == nil {
			card.DueDate = &t
		}
	}
	if req.ClearDue {
		card.DueDate = nil
	}

	now := time.Now().UTC()
	card.UpdatedAt = &now

	if err := d.KanbanQ.UpdateCard(card); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to update card")
		return
	}

	event, _ := ws.NewEvent(ws.EventKanbanCardUpdate, card)
	d.Hub.Broadcast(event)

	util.JSON(w, http.StatusOK, card)
}

// MoveKanbanCard — POST /api/kanban/cards/{cardId}/move
func (d *Deps) MoveKanbanCard(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermSendMessages); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	cardID := r.PathValue("cardId")

	if _, err := d.KanbanQ.GetCard(cardID); err != nil {
		util.Error(w, http.StatusNotFound, "card not found")
		return
	}

	var req struct {
		ColumnID string `json:"column_id"`
		Position int    `json:"position"`
	}
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.ColumnID == "" {
		util.Error(w, http.StatusBadRequest, "column_id is required")
		return
	}

	if err := d.KanbanQ.MoveCard(cardID, req.ColumnID, req.Position); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to move card")
		return
	}

	event, _ := ws.NewEvent(ws.EventKanbanCardMove, map[string]interface{}{
		"card_id":   cardID,
		"column_id": req.ColumnID,
		"position":  req.Position,
	})
	d.Hub.Broadcast(event)

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// DeleteKanbanCard — DELETE /api/kanban/cards/{cardId}
func (d *Deps) DeleteKanbanCard(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	cardID := r.PathValue("cardId")

	card, err := d.KanbanQ.GetCard(cardID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "card not found")
		return
	}

	// Creator karty nebo admin
	if card.CreatedBy != user.ID {
		if err := d.requirePermission(user, models.PermManageChannels); err != nil {
			util.Error(w, http.StatusForbidden, "only creator or admin can delete card")
			return
		}
	}

	if err := d.KanbanQ.DeleteCard(cardID); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to delete card")
		return
	}

	event, _ := ws.NewEvent(ws.EventKanbanCardDelete, map[string]string{
		"id":       cardID,
		"board_id": card.BoardID,
	})
	d.Hub.Broadcast(event)

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
