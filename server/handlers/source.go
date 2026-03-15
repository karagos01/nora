package handlers

import (
	"net/http"
	"nora/util"
)

func (d *Deps) SourceInfo(w http.ResponseWriter, r *http.Request) {
	util.JSON(w, http.StatusOK, map[string]string{
		"license":    "AGPL-3.0",
		"source_url": d.SourceURL,
		"version":    "2.0.0",
	})
}

func (d *Deps) SourceDownload(w http.ResponseWriter, r *http.Request) {
	if d.SourceURL != "" {
		http.Redirect(w, r, d.SourceURL, http.StatusFound)
		return
	}
	util.Error(w, http.StatusNotFound, "source url not configured")
}
