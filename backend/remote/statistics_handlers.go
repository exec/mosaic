package remote

import "net/http"

func (h *Handlers) GetTransferStatistics(w http.ResponseWriter, r *http.Request) {
	stats, err := h.svc.GetTransferStatistics(r.Context())
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, stats)
}
