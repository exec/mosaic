package remote

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/time/rate"

	"mosaic/backend/api"
	"mosaic/backend/engine"
	"mosaic/backend/persistence"
)

// Handlers wraps an *api.Service with thin REST adapters. Each method maps 1:1
// to a Service method — no business logic lives here.
type Handlers struct {
	svc      *api.Service
	sessions *SessionStore
	secure   bool   // controls Secure cookie attribute
	flavor   string // "daemon" | "desktop" — reported by /api/bootstrap

	loginLimiter *loginRateLimiter
}

func NewHandlers(svc *api.Service, sessions *SessionStore, secure bool, flavor string) *Handlers {
	return &Handlers{
		svc:          svc,
		sessions:     sessions,
		secure:       secure,
		flavor:       flavor,
		loginLimiter: newLoginRateLimiter(),
	}
}

// ---- login rate limiter ----

// loginRateLimiter is a per-IP token bucket guarding /api/login. Allows ~5
// attempts per minute (rate.Every(12s), burst 5). Idle limiters are evicted
// by a background janitor so the map can't grow without bound.
type loginRateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*loginBucket
}

type loginBucket struct {
	lim  *rate.Limiter
	seen time.Time
}

const (
	loginRateInterval   = 12 * time.Second
	loginRateBurst      = 5
	loginBucketIdleTTL  = 10 * time.Minute
	loginJanitorPeriod  = 5 * time.Minute
	loginRetryAfterSecs = 12
)

func newLoginRateLimiter() *loginRateLimiter {
	l := &loginRateLimiter{buckets: make(map[string]*loginBucket)}
	go l.janitor()
	return l
}

// allow consults the per-IP bucket. Returns true if the attempt is permitted.
func (l *loginRateLimiter) allow(ip string) bool {
	l.mu.Lock()
	b, ok := l.buckets[ip]
	if !ok {
		b = &loginBucket{lim: rate.NewLimiter(rate.Every(loginRateInterval), loginRateBurst)}
		l.buckets[ip] = b
	}
	b.seen = time.Now()
	l.mu.Unlock()
	return b.lim.Allow()
}

// janitor drops buckets idle longer than loginBucketIdleTTL. Runs forever; the
// process exits with the program so we don't bother with shutdown plumbing.
func (l *loginRateLimiter) janitor() {
	t := time.NewTicker(loginJanitorPeriod)
	defer t.Stop()
	for range t.C {
		cutoff := time.Now().Add(-loginBucketIdleTTL)
		l.mu.Lock()
		for ip, b := range l.buckets {
			if b.seen.Before(cutoff) {
				delete(l.buckets, ip)
			}
		}
		l.mu.Unlock()
	}
}

// clientIP extracts the host portion of r.RemoteAddr, handling IPv6 brackets
// gracefully. Falls back to the raw RemoteAddr if parsing fails.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
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
	ip := clientIP(r)
	if !h.loginLimiter.allow(ip) {
		w.Header().Set("Retry-After", strconv.Itoa(loginRetryAfterSecs))
		writeErr(w, http.StatusTooManyRequests, errors.New("too many login attempts"))
		return
	}
	var req loginRequest
	if err := decodeJSON(r, &req); err != nil {
		writeServiceErr(w, err)
		return
	}
	caller, err := h.svc.Authenticate(r.Context(), req.Username, req.Password)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, errors.New("invalid credentials"))
		return
	}
	tok, err := h.sessions.Create(caller.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, errors.New("session create failed"))
		return
	}
	SetSessionCookie(w, tok, h.secure)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// writeServiceErr translates a Service-layer error into the right HTTP status:
// permission failures → 403, auth failures → 401, missing rows → 404, and
// everything else (validation, bad input) → 400.
func writeServiceErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, api.ErrForbidden):
		writeErr(w, http.StatusForbidden, err)
	case errors.Is(err, api.ErrUnauthorized):
		writeErr(w, http.StatusUnauthorized, err)
	case errors.Is(err, persistence.ErrNotFound):
		writeErr(w, http.StatusNotFound, err)
	default:
		writeErr(w, http.StatusBadRequest, err)
	}
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
		writeServiceErr(w, err)
		return
	}
	id, err := h.svc.AddMagnet(r.Context(), req.Magnet, req.SavePath)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": string(id)})
}

func (h *Handlers) AddTorrentFile(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(10 << 20); err != nil { // 10 MiB
		writeServiceErr(w, err)
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	defer file.Close()
	blob, err := io.ReadAll(file)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	savePath := r.FormValue("save_path")
	id, err := h.svc.AddTorrentBytes(r.Context(), blob, savePath)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": string(id)})
}

func (h *Handlers) Pause(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Pause(r.Context(), engine.TorrentID(chi.URLParam(r, "id"))); err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handlers) Resume(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Resume(r.Context(), engine.TorrentID(chi.URLParam(r, "id"))); err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handlers) Recheck(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Recheck(r.Context(), engine.TorrentID(chi.URLParam(r, "id"))); err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handlers) Remove(w http.ResponseWriter, r *http.Request) {
	deleteFiles := r.URL.Query().Get("delete") == "1"
	if err := h.svc.Remove(r.Context(), engine.TorrentID(chi.URLParam(r, "id")), deleteFiles); err != nil {
		writeServiceErr(w, err)
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
		writeServiceErr(w, err)
		return
	}
	if err := h.svc.SetInspectorFocus(r.Context(), req.ID, req.Tabs); err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handlers) ClearInspectorFocus(w http.ResponseWriter, r *http.Request) {
	h.svc.ClearInspectorFocus(r.Context())
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
		writeServiceErr(w, err)
		return
	}
	id, err := h.svc.CreateCategory(r.Context(), req.Name, req.DefaultSavePath, req.Color)
	if err != nil {
		writeServiceErr(w, err)
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
		writeServiceErr(w, err)
		return
	}
	if err := h.svc.UpdateCategory(r.Context(), req.ID, req.Name, req.DefaultSavePath, req.Color); err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handlers) DeleteCategory(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	if err := h.svc.DeleteCategory(r.Context(), id); err != nil {
		writeServiceErr(w, err)
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
		writeServiceErr(w, err)
		return
	}
	id, err := h.svc.CreateTag(r.Context(), req.Name, req.Color)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"id": id})
}

func (h *Handlers) DeleteTag(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	if err := h.svc.DeleteTag(r.Context(), id); err != nil {
		writeServiceErr(w, err)
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
		writeServiceErr(w, err)
		return
	}
	if err := h.svc.AssignTag(r.Context(), req.InfoHash, req.TagID); err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handlers) UnassignTag(w http.ResponseWriter, r *http.Request) {
	var req assignTagRequest
	if err := decodeJSON(r, &req); err != nil {
		writeServiceErr(w, err)
		return
	}
	if err := h.svc.UnassignTag(r.Context(), req.InfoHash, req.TagID); err != nil {
		writeServiceErr(w, err)
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
		writeServiceErr(w, err)
		return
	}
	if err := h.svc.SetTorrentCategory(r.Context(), req.InfoHash, req.CategoryID); err != nil {
		writeServiceErr(w, err)
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
		writeServiceErr(w, err)
		return
	}
	if err := h.svc.SetDefaultSavePath(r.Context(), req.Path); err != nil {
		writeServiceErr(w, err)
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
		writeServiceErr(w, err)
		return
	}
	if err := h.svc.SetLimits(r.Context(), l); err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handlers) ToggleAltSpeed(w http.ResponseWriter, r *http.Request) {
	on, err := h.svc.ToggleAltSpeed(r.Context())
	if err != nil {
		writeServiceErr(w, err)
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
		writeServiceErr(w, err)
		return
	}
	if err := h.svc.SetQueueLimits(r.Context(), q); err != nil {
		writeServiceErr(w, err)
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
		writeServiceErr(w, err)
		return
	}
	if err := h.svc.SetPeerLimits(r.Context(), p); err != nil {
		writeServiceErr(w, err)
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
		writeServiceErr(w, err)
		return
	}
	if err := h.svc.SetQueuePosition(r.Context(), req.InfoHash, req.Pos); err != nil {
		writeServiceErr(w, err)
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
		writeServiceErr(w, err)
		return
	}
	if err := h.svc.SetForceStart(r.Context(), req.InfoHash, req.Force); err != nil {
		writeServiceErr(w, err)
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
		writeServiceErr(w, err)
		return
	}
	if err := h.svc.SetBlocklistURL(r.Context(), req.URL, req.Enabled); err != nil {
		writeServiceErr(w, err)
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
		writeServiceErr(w, err)
		return
	}
	if err := h.svc.SetFilePriorities(r.Context(), req.InfoHash, req.Priorities); err != nil {
		writeServiceErr(w, err)
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
		writeServiceErr(w, err)
		return
	}
	id, err := h.svc.CreateScheduleRule(r.Context(), rule)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"id": id})
}

func (h *Handlers) UpdateScheduleRule(w http.ResponseWriter, r *http.Request) {
	var rule api.ScheduleRuleDTO
	if err := decodeJSON(r, &rule); err != nil {
		writeServiceErr(w, err)
		return
	}
	if err := h.svc.UpdateScheduleRule(r.Context(), rule); err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handlers) DeleteScheduleRule(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	if err := h.svc.DeleteScheduleRule(r.Context(), id); err != nil {
		writeServiceErr(w, err)
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
		writeServiceErr(w, err)
		return
	}
	id, err := h.svc.CreateFeed(r.Context(), feed)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"id": id})
}

func (h *Handlers) UpdateFeed(w http.ResponseWriter, r *http.Request) {
	var feed api.FeedDTO
	if err := decodeJSON(r, &feed); err != nil {
		writeServiceErr(w, err)
		return
	}
	if err := h.svc.UpdateFeed(r.Context(), feed); err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handlers) DeleteFeed(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	if err := h.svc.DeleteFeed(r.Context(), id); err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handlers) ListFiltersByFeed(w http.ResponseWriter, r *http.Request) {
	feedID, err := strconv.Atoi(chi.URLParam(r, "feedID"))
	if err != nil {
		writeServiceErr(w, err)
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
		writeServiceErr(w, err)
		return
	}
	id, err := h.svc.CreateFilter(r.Context(), filter)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"id": id})
}

func (h *Handlers) UpdateFilter(w http.ResponseWriter, r *http.Request) {
	var filter api.FilterDTO
	if err := decodeJSON(r, &filter); err != nil {
		writeServiceErr(w, err)
		return
	}
	if err := h.svc.UpdateFilter(r.Context(), filter); err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handlers) DeleteFilter(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	if err := h.svc.DeleteFilter(r.Context(), id); err != nil {
		writeServiceErr(w, err)
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
		writeServiceErr(w, err)
		return
	}
	if err := h.svc.SetWebConfig(r.Context(), c); err != nil {
		writeServiceErr(w, err)
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
		writeServiceErr(w, err)
		return
	}
	if err := h.svc.SetWebPassword(r.Context(), req.Password); err != nil {
		writeServiceErr(w, err)
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
		writeServiceErr(w, err)
		return
	}
	if err := h.svc.SetUpdaterConfig(r.Context(), c); err != nil {
		writeServiceErr(w, err)
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

// ---- bootstrap (unauthenticated) ----

// Bootstrap reports which build the SPA is talking to so it can pick the right
// settings panes (Web Interface vs Users) before the user logs in.
func (h *Handlers) Bootstrap(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, api.BootstrapDTO{
		Flavor:    h.flavor,
		Version:   h.svc.AppVersion(),
		MultiUser: h.flavor == FlavorDaemon,
	})
}

// ---- current user ----

// Me returns the authenticated caller's own profile.
func (h *Handlers) Me(w http.ResponseWriter, r *http.Request) {
	me, err := h.svc.Me(r.Context())
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, me)
}

type changePasswordRequest struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

// ChangeMyPassword changes the caller's own password.
func (h *Handlers) ChangeMyPassword(w http.ResponseWriter, r *http.Request) {
	var req changePasswordRequest
	if err := decodeJSON(r, &req); err != nil {
		writeServiceErr(w, err)
		return
	}
	if err := h.svc.ChangeMyPassword(r.Context(), req.OldPassword, req.NewPassword); err != nil {
		writeServiceErr(w, err)
		return
	}
	// The caller's sessions were just revoked — clear their cookie too.
	ClearSessionCookie(w, h.secure)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// RotateMyAPIKey mints a fresh API key for the caller and returns it once.
func (h *Handlers) RotateMyAPIKey(w http.ResponseWriter, r *http.Request) {
	key, err := h.svc.RotateMyAPIKey(r.Context())
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"api_key": key})
}

// ---- user administration ----

type userInputRequest struct {
	Username           string `json:"username"`
	Password           string `json:"password"`
	Role               string `json:"role"`
	PermAddTorrents    bool   `json:"perm_add_torrents"`
	PermManageRSS      bool   `json:"perm_manage_rss"`
	PermManageCatTags  bool   `json:"perm_manage_cat_tags"`
	PermChangeSettings bool   `json:"perm_change_settings"`
	PermShare          bool   `json:"perm_share"`
	Disabled           bool   `json:"disabled"`
}

func (req userInputRequest) toInput() api.UserInput {
	return api.UserInput{
		Username:           req.Username,
		Password:           req.Password,
		Role:               req.Role,
		PermAddTorrents:    req.PermAddTorrents,
		PermManageRSS:      req.PermManageRSS,
		PermManageCatTags:  req.PermManageCatTags,
		PermChangeSettings: req.PermChangeSettings,
		PermShare:          req.PermShare,
		Disabled:           req.Disabled,
	}
}

// ListUsers returns every account (admin only).
func (h *Handlers) ListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.svc.ListUsers(r.Context())
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, users)
}

// CreateUser adds a new account (admin only).
func (h *Handlers) CreateUser(w http.ResponseWriter, r *http.Request) {
	var req userInputRequest
	if err := decodeJSON(r, &req); err != nil {
		writeServiceErr(w, err)
		return
	}
	u, err := h.svc.CreateUser(r.Context(), req.toInput())
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, u)
}

// UpdateUser edits an account's username/role/permissions (admin only).
func (h *Handlers) UpdateUser(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, errors.New("bad user id"))
		return
	}
	var req userInputRequest
	if err := decodeJSON(r, &req); err != nil {
		writeServiceErr(w, err)
		return
	}
	u, err := h.svc.UpdateUser(r.Context(), id, req.toInput())
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, u)
}

// DeleteUser removes an account (admin only).
func (h *Handlers) DeleteUser(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, errors.New("bad user id"))
		return
	}
	if err := h.svc.DeleteUser(r.Context(), id); err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type resetPasswordRequest struct {
	NewPassword string `json:"new_password"`
}

// ResetUserPassword sets another user's password (admin only).
func (h *Handlers) ResetUserPassword(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, errors.New("bad user id"))
		return
	}
	var req resetPasswordRequest
	if err := decodeJSON(r, &req); err != nil {
		writeServiceErr(w, err)
		return
	}
	if err := h.svc.ResetUserPassword(r.Context(), id, req.NewPassword); err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---- torrent sharing ----

// ListTorrentShares returns every access grant on a torrent (owner only).
func (h *Handlers) ListTorrentShares(w http.ResponseWriter, r *http.Request) {
	shares, err := h.svc.ListTorrentShares(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, shares)
}

type shareRequest struct {
	UserID int    `json:"user_id"`
	Access string `json:"access"`
}

// ShareTorrent grants another user access to a torrent (owner only).
func (h *Handlers) ShareTorrent(w http.ResponseWriter, r *http.Request) {
	var req shareRequest
	if err := decodeJSON(r, &req); err != nil {
		writeServiceErr(w, err)
		return
	}
	if err := h.svc.ShareTorrent(r.Context(), chi.URLParam(r, "id"), req.UserID, req.Access); err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// UnshareTorrent revokes another user's access to a torrent (owner only).
func (h *Handlers) UnshareTorrent(w http.ResponseWriter, r *http.Request) {
	uid, err := strconv.Atoi(chi.URLParam(r, "userID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, errors.New("bad user id"))
		return
	}
	if err := h.svc.UnshareTorrent(r.Context(), chi.URLParam(r, "id"), uid); err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
