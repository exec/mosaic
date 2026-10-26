package remote

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"mosaic/backend/api"
)

func (h *Handlers) PrepareTorrent(w http.ResponseWriter, r *http.Request) {
	var req api.PrepareTorrentRequest
	r.Body = http.MaxBytesReader(w, r.Body, (maxTorrentUploadBytes*4/3)+(1<<16))
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeServiceErr(w, err)
		return
	}
	id, err := h.svc.PrepareTorrent(r.Context(), req)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": id})
}
func (h *Handlers) GetTorrentDetail(w http.ResponseWriter, r *http.Request) {
	d, err := h.svc.GetTorrentDetail(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}
func (h *Handlers) GetStorage(w http.ResponseWriter, r *http.Request) {
	d, err := h.svc.GetStorage(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}
func (h *Handlers) SetPreallocate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeServiceErr(w, err)
		return
	}
	if err := h.svc.SetPreallocate(r.Context(), req.Enabled); err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
func (h *Handlers) RelocateTorrent(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
		Move bool   `json:"move"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeServiceErr(w, err)
		return
	}
	if err := h.svc.RelocateTorrent(r.Context(), chi.URLParam(r, "id"), req.Path, req.Move); err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
