package remote

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"mosaic/backend/api"
	"mosaic/backend/engine"
)

// Handlers wraps an *api.Service with thin REST adapters. Each method maps 1:1
// to a Service method — no business logic lives here.
type Handlers struct {
	svc      *api.Service
	sessions *SessionStore
	secure   bool // controls Secure cookie attribute
}

func NewHandlers(svc *api.Service, sessions *SessionStore, secure bool) *Handlers {
	return &Handlers{svc: svc, sessions: sessions, secure: secure}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func decodeJSON[T any](r *http.Request, dst *T) error {
	if r.Body == nil {
		return errors.New("empty body")
	}
	return json.NewDecoder(r.Body).Decode(dst)
}

// ---- auth ----

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (h *Handlers) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if !h.svc.VerifyWebCredentials(r.Context(), req.Username, req.Password) {
		writeErr(w, http.StatusUnauthorized, errors.New("invalid credentials"))
		return
	}
	tok := h.sessions.Create()
	SetSessionCookie(w, tok, h.secure)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handlers) Logout(w http.ResponseWriter, r *http.Request) {
	if tok := SessionTokenFromRequest(r); tok != "" {
		h.sessions.Delete(tok)
	}
	ClearSessionCookie(w, h.secure)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---- torrents ----

func (h *Handlers) ListTorrents(w http.ResponseWriter, r *http.Request) {
	rows, err := h.svc.ListTorrents(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

type addMagnetRequest struct {
	Magnet   string `json:"magnet"`
	SavePath string `json:"save_path"`
}

func (h *Handlers) AddMagnet(w http.ResponseWriter, r *http.Request) {
	var req addMagnetRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	id, err := h.svc.AddMagnet(r.Context(), req.Magnet, req.SavePath)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": string(id)})
}

func (h *Handlers) AddTorrentFile(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(10 << 20); err != nil { // 10 MiB
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	defer file.Close()
	blob, err := io.ReadAll(file)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	savePath := r.FormValue("save_path")
	id, err := h.svc.AddTorrentBytes(r.Context(), blob, savePath)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": string(id)})
}

func (h *Handlers) Pause(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Pause(engine.TorrentID(chi.URLParam(r, "id"))); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handlers) Resume(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Resume(engine.TorrentID(chi.URLParam(r, "id"))); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handlers) Recheck(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Recheck(engine.TorrentID(chi.URLParam(r, "id"))); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handlers) Remove(w http.ResponseWriter, r *http.Request) {
	deleteFiles := r.URL.Query().Get("delete") == "1"
	if err := h.svc.Remove(r.Context(), engine.TorrentID(chi.URLParam(r, "id")), deleteFiles); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---- inspector ----

type inspectorFocusRequest struct {
	ID   string   `json:"id"`
	Tabs []string `json:"tabs"`
}

func (h *Handlers) SetInspectorFocus(w http.ResponseWriter, r *http.Request) {
	var req inspectorFocusRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := h.svc.SetInspectorFocus(req.ID, req.Tabs); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handlers) ClearInspectorFocus(w http.ResponseWriter, r *http.Request) {
	h.svc.ClearInspectorFocus()
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---- stats ----

func (h *Handlers) GlobalStats(w http.ResponseWriter, r *http.Request) {
	st, err := h.svc.GlobalStats(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// ---- categories / tags ----

type createCategoryRequest struct {
	Name            string `json:"name"`
	DefaultSavePath string `json:"default_save_path"`
	Color           string `json:"color"`
}

func (h *Handlers) ListCategories(w http.ResponseWriter, r *http.Request) {
	cats, err := h.svc.ListCategories(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, cats)
}

func (h *Handlers) CreateCategory(w http.ResponseWriter, r *http.Request) {
	var req createCategoryRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	id, err := h.svc.CreateCategory(r.Context(), req.Name, req.DefaultSavePath, req.Color)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"id": id})
}

type updateCategoryRequest struct {
	ID              int    `json:"id"`
	Name            string `json:"name"`
	DefaultSavePath string `json:"default_save_path"`
	Color           string `json:"color"`
}

func (h *Handlers) UpdateCategory(w http.ResponseWriter, r *http.Request) {
	var req updateCategoryRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := h.svc.UpdateCategory(r.Context(), req.ID, req.Name, req.DefaultSavePath, req.Color); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handlers) DeleteCategory(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := h.svc.DeleteCategory(r.Context(), id); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type createTagRequest struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

func (h *Handlers) ListTags(w http.ResponseWriter, r *http.Request) {
	tags, err := h.svc.ListTags(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, tags)
}

func (h *Handlers) CreateTag(w http.ResponseWriter, r *http.Request) {
	var req createTagRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	id, err := h.svc.CreateTag(r.Context(), req.Name, req.Color)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"id": id})
}

func (h *Handlers) DeleteTag(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := h.svc.DeleteTag(r.Context(), id); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type assignTagRequest struct {
	InfoHash string `json:"infohash"`
	TagID    int    `json:"tag_id"`
}

func (h *Handlers) AssignTag(w http.ResponseWriter, r *http.Request) {
	var req assignTagRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := h.svc.AssignTag(r.Context(), req.InfoHash, req.TagID); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handlers) UnassignTag(w http.ResponseWriter, r *http.Request) {
	var req assignTagRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := h.svc.UnassignTag(r.Context(), req.InfoHash, req.TagID); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type setTorrentCategoryRequest struct {
	InfoHash   string `json:"infohash"`
	CategoryID *int   `json:"category_id"`
}

func (h *Handlers) SetTorrentCategory(w http.ResponseWriter, r *http.Request) {
	var req setTorrentCategoryRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := h.svc.SetTorrentCategory(r.Context(), req.InfoHash, req.CategoryID); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---- limits / queue / paths / blocklist / schedule / feeds / filters ----

func (h *Handlers) GetDefaultSavePath(w http.ResponseWriter, r *http.Request) {
	v, err := h.svc.GetDefaultSavePath(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"path": v})
}

type setDefaultSavePathRequest struct {
	Path string `json:"path"`
}

func (h *Handlers) SetDefaultSavePath(w http.ResponseWriter, r *http.Request) {
	var req setDefaultSavePathRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := h.svc.SetDefaultSavePath(r.Context(), req.Path); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handlers) GetLimits(w http.ResponseWriter, r *http.Request) {
	l, err := h.svc.GetLimits(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, l)
}

func (h *Handlers) SetLimits(w http.ResponseWriter, r *http.Request) {
	var l api.LimitsDTO
	if err := decodeJSON(r, &l); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := h.svc.SetLimits(r.Context(), l); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handlers) ToggleAltSpeed(w http.ResponseWriter, r *http.Request) {
	on, err := h.svc.ToggleAltSpeed(r.Context())
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"alt_active": on})
}

func (h *Handlers) GetQueueLimits(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.svc.GetQueueLimits(r.Context()))
}

func (h *Handlers) SetQueueLimits(w http.ResponseWriter, r *http.Request) {
	var q api.QueueLimitsDTO
	if err := decodeJSON(r, &q); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := h.svc.SetQueueLimits(r.Context(), q); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handlers) GetPeerLimits(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.svc.GetPeerLimits(r.Context()))
}

func (h *Handlers) SetPeerLimits(w http.ResponseWriter, r *http.Request) {
	var p api.PeerLimitsDTO
	if err := decodeJSON(r, &p); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := h.svc.SetPeerLimits(r.Context(), p); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type queuePosRequest struct {
	InfoHash string `json:"infohash"`
	Pos      int    `json:"pos"`
}

func (h *Handlers) SetQueuePosition(w http.ResponseWriter, r *http.Request) {
	var req queuePosRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := h.svc.SetQueuePosition(r.Context(), req.InfoHash, req.Pos); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type forceStartRequest struct {
	InfoHash string `json:"infohash"`
	Force    bool   `json:"force"`
}

func (h *Handlers) SetForceStart(w http.ResponseWriter, r *http.Request) {
	var req forceStartRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := h.svc.SetForceStart(r.Context(), req.InfoHash, req.Force); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handlers) GetBlocklist(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.svc.GetBlocklist(r.Context()))
}

type setBlocklistRequest struct {
	URL     string `json:"url"`
	Enabled bool   `json:"enabled"`
}

func (h *Handlers) SetBlocklist(w http.ResponseWriter, r *http.Request) {
	var req setBlocklistRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := h.svc.SetBlocklistURL(r.Context(), req.URL, req.Enabled); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handlers) RefreshBlocklist(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.RefreshBlocklist(r.Context()); err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type setFilePrioritiesRequest struct {
	InfoHash   string         `json:"infohash"`
	Priorities map[int]string `json:"priorities"`
}

func (h *Handlers) SetFilePriorities(w http.ResponseWriter, r *http.Request) {
	var req setFilePrioritiesRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := h.svc.SetFilePriorities(r.Context(), req.InfoHash, req.Priorities); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handlers) ListScheduleRules(w http.ResponseWriter, r *http.Request) {
	rules, err := h.svc.ListScheduleRules(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, rules)
}

func (h *Handlers) CreateScheduleRule(w http.ResponseWriter, r *http.Request) {
	var rule api.ScheduleRuleDTO
	if err := decodeJSON(r, &rule); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	id, err := h.svc.CreateScheduleRule(r.Context(), rule)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"id": id})
}

func (h *Handlers) UpdateScheduleRule(w http.ResponseWriter, r *http.Request) {
	var rule api.ScheduleRuleDTO
	if err := decodeJSON(r, &rule); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := h.svc.UpdateScheduleRule(r.Context(), rule); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handlers) DeleteScheduleRule(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := h.svc.DeleteScheduleRule(r.Context(), id); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handlers) ListFeeds(w http.ResponseWriter, r *http.Request) {
	feeds, err := h.svc.ListFeeds(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, feeds)
}

func (h *Handlers) CreateFeed(w http.ResponseWriter, r *http.Request) {
	var feed api.FeedDTO
	if err := decodeJSON(r, &feed); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	id, err := h.svc.CreateFeed(r.Context(), feed)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"id": id})
}

func (h *Handlers) UpdateFeed(w http.ResponseWriter, r *http.Request) {
	var feed api.FeedDTO
	if err := decodeJSON(r, &feed); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := h.svc.UpdateFeed(r.Context(), feed); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handlers) DeleteFeed(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := h.svc.DeleteFeed(r.Context(), id); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handlers) ListFiltersByFeed(w http.ResponseWriter, r *http.Request) {
	feedID, err := strconv.Atoi(chi.URLParam(r, "feedID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	filters, err := h.svc.ListFiltersByFeed(r.Context(), feedID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, filters)
}

func (h *Handlers) CreateFilter(w http.ResponseWriter, r *http.Request) {
	var filter api.FilterDTO
	if err := decodeJSON(r, &filter); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	id, err := h.svc.CreateFilter(r.Context(), filter)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"id": id})
}

func (h *Handlers) UpdateFilter(w http.ResponseWriter, r *http.Request) {
	var filter api.FilterDTO
	if err := decodeJSON(r, &filter); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := h.svc.UpdateFilter(r.Context(), filter); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handlers) DeleteFilter(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := h.svc.DeleteFilter(r.Context(), id); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---- web config + credentials ----

func (h *Handlers) GetWebConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.svc.GetWebConfig(r.Context()))
}

func (h *Handlers) SetWebConfig(w http.ResponseWriter, r *http.Request) {
	var c api.WebConfigDTO
	if err := decodeJSON(r, &c); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := h.svc.SetWebConfig(r.Context(), c); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type setWebPasswordRequest struct {
	Password string `json:"password"`
}

func (h *Handlers) SetWebPassword(w http.ResponseWriter, r *http.Request) {
	var req setWebPasswordRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := h.svc.SetWebPassword(r.Context(), req.Password); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handlers) RotateAPIKey(w http.ResponseWriter, r *http.Request) {
	key, err := h.svc.RotateAPIKey(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"api_key": key})
}

// ---- updater + version ----

func (h *Handlers) GetUpdaterConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.svc.GetUpdaterConfig(r.Context()))
}

func (h *Handlers) SetUpdaterConfig(w http.ResponseWriter, r *http.Request) {
	var c api.UpdaterConfigDTO
	if err := decodeJSON(r, &c); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := h.svc.SetUpdaterConfig(r.Context(), c); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handlers) CheckForUpdate(w http.ResponseWriter, r *http.Request) {
	info, err := h.svc.CheckForUpdate(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (h *Handlers) InstallUpdate(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.InstallUpdate(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// GetAppVersion returns the build-time version. Used by the browser-mode
// transport to populate the AboutPane and the update toast comparison.
func (h *Handlers) GetAppVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"version": h.svc.AppVersion()})
}
