package handlers

import (
	"net/http"
	"nora/auth"
	"nora/models"
	"nora/util"
	"nora/ws"
	"regexp"

	"github.com/google/uuid"
)

var hexColorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func (d *Deps) ListCategories(w http.ResponseWriter, r *http.Request) {
	categories, err := d.Categories.List()
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to list categories")
		return
	}
	if categories == nil {
		categories = []models.ChannelCategory{}
	}
	util.JSON(w, http.StatusOK, categories)
}

type createCategoryRequest struct {
	Name     string  `json:"name"`
	Color    string  `json:"color"`
	ParentID *string `json:"parent_id,omitempty"`
}

func (d *Deps) CreateCategory(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermManageChannels); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	var req createCategoryRequest
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if msg := util.ValidateCategoryName(req.Name); msg != "" {
		util.Error(w, http.StatusBadRequest, msg)
		return
	}

	if req.Color == "" {
		req.Color = "#555555"
	}
	if !hexColorRe.MatchString(req.Color) {
		util.Error(w, http.StatusBadRequest, "color must be hex #RRGGBB")
		return
	}

	// Validace parent_id — max 2 úrovně (root → child)
	if req.ParentID != nil && *req.ParentID != "" {
		parent, err := d.Categories.GetByID(*req.ParentID)
		if err != nil {
			util.Error(w, http.StatusBadRequest, "parent category not found")
			return
		}
		if parent.ParentID != nil && *parent.ParentID != "" {
			util.Error(w, http.StatusBadRequest, "max 2 levels of nesting allowed")
			return
		}
	}

	pos, _ := d.Categories.NextPosition()
	id, _ := uuid.NewV7()
	cat := &models.ChannelCategory{
		ID:       id.String(),
		Name:     req.Name,
		Color:    req.Color,
		Position: pos,
		ParentID: req.ParentID,
	}

	if err := d.Categories.Create(cat); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to create category")
		return
	}

	cat, _ = d.Categories.GetByID(cat.ID)
	msg, _ := ws.NewEvent(ws.EventCategoryCreate, cat)
	d.Hub.Broadcast(msg)

	d.logAudit(user.ID, "category.create", "category", cat.ID, map[string]string{"name": cat.Name})

	util.JSON(w, http.StatusCreated, cat)
}

type updateCategoryRequest struct {
	Name     *string `json:"name,omitempty"`
	Color    *string `json:"color,omitempty"`
	Position *int    `json:"position,omitempty"`
	ParentID *string `json:"parent_id,omitempty"`
}

func (d *Deps) UpdateCategory(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermManageChannels); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	id := r.PathValue("id")
	cat, err := d.Categories.GetByID(id)
	if err != nil {
		util.Error(w, http.StatusNotFound, "category not found")
		return
	}

	var req updateCategoryRequest
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name != nil {
		if msg := util.ValidateCategoryName(*req.Name); msg != "" {
			util.Error(w, http.StatusBadRequest, msg)
			return
		}
		cat.Name = *req.Name
	}
	if req.Color != nil {
		if !hexColorRe.MatchString(*req.Color) {
			util.Error(w, http.StatusBadRequest, "color must be hex #RRGGBB")
			return
		}
		cat.Color = *req.Color
	}
	if req.Position != nil {
		cat.Position = *req.Position
	}

	// Validace parent_id
	if req.ParentID != nil {
		newParentID := *req.ParentID
		if newParentID == "" {
			// Odebrat parenta (stát se top-level)
			cat.ParentID = nil
		} else {
			// Nesmí nastavit sebe jako parenta
			if newParentID == cat.ID {
				util.Error(w, http.StatusBadRequest, "category cannot be its own parent")
				return
			}
			// Parent musí existovat
			parent, err := d.Categories.GetByID(newParentID)
			if err != nil {
				util.Error(w, http.StatusBadRequest, "parent category not found")
				return
			}
			// Max 2 úrovně — parent nesmí mít sám parenta
			if parent.ParentID != nil && *parent.ParentID != "" {
				util.Error(w, http.StatusBadRequest, "max 2 levels of nesting allowed")
				return
			}
			// Root s dětmi se nesmí stát child
			cats, _ := d.Categories.List()
			for _, c := range cats {
				if c.ID == cat.ID && len(c.Children) > 0 {
					util.Error(w, http.StatusBadRequest, "category with children cannot become a child")
					return
				}
			}
			cat.ParentID = &newParentID
		}
	}

	if err := d.Categories.Update(cat); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to update category")
		return
	}

	msg, _ := ws.NewEvent(ws.EventCategoryUpdate, cat)
	d.Hub.Broadcast(msg)

	d.logAudit(user.ID, "category.update", "category", cat.ID, map[string]string{"name": cat.Name})

	util.JSON(w, http.StatusOK, cat)
}

func (d *Deps) DeleteCategory(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermManageChannels); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	id := r.PathValue("id")
	cat, err := d.Categories.GetByID(id)
	if err != nil {
		util.Error(w, http.StatusNotFound, "category not found")
		return
	}
	catName := cat.Name

	if err := d.Categories.Delete(id); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to delete category")
		return
	}

	msg, _ := ws.NewEvent(ws.EventCategoryDelete, map[string]string{"id": id})
	d.Hub.Broadcast(msg)

	d.logAudit(user.ID, "category.delete", "category", id, map[string]string{"name": catName})

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type reorderCategoriesRequest struct {
	CategoryIDs []string `json:"category_ids"`
}

func (d *Deps) ReorderCategories(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermManageChannels); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	var req reorderCategoriesRequest
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if len(req.CategoryIDs) == 0 {
		util.Error(w, http.StatusBadRequest, "category_ids required")
		return
	}

	for _, id := range req.CategoryIDs {
		if _, err := d.Categories.GetByID(id); err != nil {
			util.Error(w, http.StatusBadRequest, "category not found: "+id)
			return
		}
	}

	if err := d.Categories.Reorder(req.CategoryIDs); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to reorder categories")
		return
	}

	// Broadcast update pro každou kategorii
	for _, id := range req.CategoryIDs {
		cat, err := d.Categories.GetByID(id)
		if err != nil {
			continue
		}
		msg, _ := ws.NewEvent(ws.EventCategoryUpdate, cat)
		d.Hub.Broadcast(msg)
	}

	categories, _ := d.Categories.List()
	if categories == nil {
		categories = []models.ChannelCategory{}
	}
	util.JSON(w, http.StatusOK, categories)
}
