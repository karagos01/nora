package handlers

import (
	"net/http"
	"nora/auth"
	"nora/models"
	"nora/util"
	"strconv"
)

// Gallery — GET /api/gallery?type=image&channel_id=...&user_id=...&before=...&limit=...&search=...
func (d *Deps) Gallery(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermRead); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	q := r.URL.Query()
	mimePrefix := q.Get("type")
	channelID := q.Get("channel_id")
	userID := q.Get("user_id")
	before := q.Get("before")
	search := q.Get("search")
	limit := 50
	if l := q.Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}

	// Mapování zkrácených typů na MIME prefix
	switch mimePrefix {
	case "image":
		mimePrefix = "image/"
	case "video":
		mimePrefix = "video/"
	case "audio":
		mimePrefix = "audio/"
	case "":
		// Bez filtru — všechny typy
	default:
		mimePrefix = mimePrefix + "/"
	}

	var items []models.GalleryItem
	var err error

	if search != "" {
		items, err = d.GalleryQ.Search(search, limit)
	} else {
		items, err = d.GalleryQ.ListMedia(mimePrefix, channelID, userID, before, limit)
	}

	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to query gallery")
		return
	}
	if items == nil {
		items = []models.GalleryItem{}
	}

	util.JSON(w, http.StatusOK, items)
}
