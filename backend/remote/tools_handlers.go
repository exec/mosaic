package remote

import (
	"encoding/json"
	"fmt"
	"github.com/go-chi/chi/v5"
	"mosaic/backend/api"
	"net/http"
)

func (h *Handlers) PreviewRSSFilter(w http.ResponseWriter, r *http.Request) {
	var f api.FilterDTO
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&f); err != nil {
		writeServiceErr(w, err)
		return
	}
	result, err := h.svc.PreviewRSSFilter(r.Context(), f)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
func (h *Handlers) ExportSettings(w http.ResponseWriter, r *http.Request) {
	b, err := h.svc.ExportSettings(r.Context())
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, b)
}
func (h *Handlers) ImportSettings(w http.ResponseWriter, r *http.Request) {
	var b api.SettingsBackupDTO
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&b); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("invalid request body"))
		return
	}
	if err := h.svc.ImportSettings(r.Context(), b); err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
func (h *Handlers) ExportTorrent(w http.ResponseWriter, r *http.Request) {
	data, err := h.svc.ExportTorrent(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"data": data})
}
func (h *Handlers) GetConnectionDiagnostics(w http.ResponseWriter, r *http.Request) {
	data, err := h.svc.GetConnectionDiagnostics(r.Context())
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, data)
}
