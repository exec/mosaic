package remote

import (
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"

	"mosaic/backend/api"
)

// MountOptions tunes optional security knobs on the router. The zero value is
// the safe default: no trusted proxies (X-Forwarded-For ignored) and no
// X-Forwarded-Proto trust (cookie Secure only when the listener itself is TLS).
type MountOptions struct {
	// TrustedProxies is the set of CIDR ranges whose RemoteAddr the server
	// treats as a trusted reverse proxy. When a request arrives from inside
	// one of these, the X-Forwarded-For header is parsed (rightmost
	// untrusted hop wins) so per-IP rate limiters can attribute load to the
	// real client. Empty disables XFF entirely.
	TrustedProxies []*net.IPNet

	// TrustForwardedProto, when true, makes the server consider a request
	// secure (for cookie-Secure decisions) if it carries
	// `X-Forwarded-Proto: https` — used by deployments behind a TLS-
	// terminating reverse proxy. Off by default because a client-supplied
	// header on a directly-reachable port would otherwise be a downgrade
	// vector.
	TrustForwardedProto bool
}

// Server flavor identifiers, reported by /api/bootstrap. "daemon" is the
// headless multi-user mosaicd; "desktop" is the Wails app's optional single-
// user web server.
const (
	FlavorDaemon  = "daemon"
	FlavorDesktop = "desktop"
)

// Mount wires all REST routes onto a chi.Router. The /api/login and
// /api/bootstrap routes and the optional static SPA tree skip the auth
// middleware; everything else under /api/* is gated.
//
// hub, if non-nil, exposes a WebSocket upgrade at /api/ws.
// staticFS, if non-nil, is served at "/". Pass nil during tests.
// flavor is FlavorDaemon or FlavorDesktop.
func Mount(svc *api.Service, sessions *SessionStore, hub *Hub, staticFS fs.FS, secure bool, flavor string) chi.Router {
	return MountWithOptions(svc, sessions, hub, staticFS, secure, flavor, MountOptions{})
}

// MountWithOptions is the configurable form of Mount. See MountOptions.
func MountWithOptions(svc *api.Service, sessions *SessionStore, hub *Hub, staticFS fs.FS, secure bool, flavor string, opts MountOptions) chi.Router {
	r := chi.NewRouter()
	h := NewHandlers(svc, sessions, secure, flavor)
	h.SetTrustedProxies(opts.TrustedProxies)
	h.SetTrustForwardedProto(opts.TrustForwardedProto)
	gate := AuthGate(sessions, svc)
	csrf := OriginGuard()

	r.Route("/api", func(api chi.Router) {
		// public — Login still gets the OriginGuard: a cross-site form post
		// could otherwise silently log the victim's browser into an
		// attacker-controlled account (login CSRF). The SPA is same-origin so
		// legitimate logins always carry a matching Origin.
		api.With(csrf).Post("/login", h.Login)
		api.Get("/bootstrap", h.Bootstrap)
		// gated
		api.Group(func(g chi.Router) {
			g.Use(gate)
			g.Use(csrf)
			g.Post("/logout", h.Logout)

			g.Get("/me", h.Me)
			g.Post("/me/password", h.ChangeMyPassword)
			g.Post("/me/api_key/rotate", h.RotateMyAPIKey)

			g.Get("/users", h.ListUsers)
			g.Post("/users", h.CreateUser)
			g.Put("/users/{id}", h.UpdateUser)
			g.Delete("/users/{id}", h.DeleteUser)
			g.Post("/users/{id}/password", h.ResetUserPassword)

			g.Get("/torrents/{id}/shares", h.ListTorrentShares)
			g.Post("/torrents/{id}/shares", h.ShareTorrent)
			g.Delete("/torrents/{id}/shares/{userID}", h.UnshareTorrent)

			g.Get("/torrents", h.ListTorrents)
			g.Post("/torrents/magnet", h.AddMagnet)
			g.Post("/torrents/file", h.AddTorrentFile)
			g.Post("/torrents/{id}/pause", h.Pause)
			g.Post("/torrents/{id}/resume", h.Resume)
			g.Post("/torrents/{id}/recheck", h.Recheck)
			g.Delete("/torrents/{id}", h.Remove)
			g.Post("/torrents/category", h.SetTorrentCategory)
			g.Post("/torrents/file_priorities", h.SetFilePriorities)
			g.Post("/torrents/queue_position", h.SetQueuePosition)
			g.Post("/torrents/force_start", h.SetForceStart)
			g.Post("/torrents/{id}/trackers", h.AddTracker)
			g.Delete("/torrents/{id}/trackers", h.RemoveTracker)
			g.Post("/torrents/sequential", h.SetSequential)

			g.Get("/stats", h.GlobalStats)

			g.Post("/inspector/focus", h.SetInspectorFocus)
			g.Post("/inspector/clear", h.ClearInspectorFocus)

			g.Get("/categories", h.ListCategories)
			g.Post("/categories", h.CreateCategory)
			g.Put("/categories", h.UpdateCategory)
			g.Delete("/categories/{id}", h.DeleteCategory)

			g.Get("/tags", h.ListTags)
			g.Post("/tags", h.CreateTag)
			g.Delete("/tags/{id}", h.DeleteTag)
			g.Post("/tags/assign", h.AssignTag)
			g.Post("/tags/unassign", h.UnassignTag)

			g.Get("/settings/save_path", h.GetDefaultSavePath)
			g.Put("/settings/save_path", h.SetDefaultSavePath)
			g.Get("/settings/limits", h.GetLimits)
			g.Put("/settings/limits", h.SetLimits)
			g.Post("/settings/alt_speed/toggle", h.ToggleAltSpeed)
			g.Get("/settings/queue_limits", h.GetQueueLimits)
			g.Put("/settings/queue_limits", h.SetQueueLimits)
			g.Get("/settings/peer_limits", h.GetPeerLimits)
			g.Put("/settings/peer_limits", h.SetPeerLimits)
			g.Get("/settings/seeding_defaults", h.GetSeedingDefaults)
			g.Put("/settings/seeding_defaults", h.SetSeedingDefaults)
			g.Get("/torrents/{id}/seed_policy", h.GetTorrentSeedPolicy)
			g.Put("/torrents/{id}/seed_policy", h.SetTorrentSeedPolicy)
			g.Get("/torrents/{id}/rate_limits", h.GetTorrentRateLimits)
			g.Put("/torrents/{id}/rate_limits", h.SetTorrentRateLimits)
			g.Get("/settings/watch_folder", h.GetWatchFolder)
			g.Put("/settings/watch_folder", h.SetWatchFolder)
			g.Get("/settings/blocklist", h.GetBlocklist)
			g.Put("/settings/blocklist", h.SetBlocklist)
			g.Post("/settings/blocklist/refresh", h.RefreshBlocklist)
			g.Get("/settings/web", h.GetWebConfig)
			g.Put("/settings/web", h.SetWebConfig)
			g.Put("/settings/web/password", h.SetWebPassword)
			g.Post("/settings/web/api_key/rotate", h.RotateAPIKey)
			g.Get("/settings/updater", h.GetUpdaterConfig)
			g.Put("/settings/updater", h.SetUpdaterConfig)
			g.Post("/updater/check", h.CheckForUpdate)
			g.Post("/updater/install", h.InstallUpdate)
			g.Get("/version", h.GetAppVersion)

			g.Get("/schedule_rules", h.ListScheduleRules)
			g.Post("/schedule_rules", h.CreateScheduleRule)
			g.Put("/schedule_rules", h.UpdateScheduleRule)
			g.Delete("/schedule_rules/{id}", h.DeleteScheduleRule)

			g.Get("/feeds", h.ListFeeds)
			g.Post("/feeds", h.CreateFeed)
			g.Put("/feeds", h.UpdateFeed)
			g.Delete("/feeds/{id}", h.DeleteFeed)
			g.Get("/feeds/{feedID}/items", h.GetFeedItems)
			g.Post("/torrents/url", h.AddFeedItem)
			g.Get("/feeds/{feedID}/filters", h.ListFiltersByFeed)
			g.Post("/filters", h.CreateFilter)
			g.Put("/filters", h.UpdateFilter)
			g.Delete("/filters/{id}", h.DeleteFilter)
		})
		if hub != nil {
			// WS upgrade — auth is checked inline so the upgrade response is correct.
			api.Get("/ws", hub.HandleUpgrade(sessions, svc))
		}
	})

	if staticFS != nil {
		r.Mount("/", http.FileServer(http.FS(staticFS)))
	}
	return r
}

// OriginGuard rejects cookie-authenticated state-changing requests (POST /
// PUT / DELETE / PATCH) whose Origin (or Referer) header host does not match
// the request Host. It is the CSRF defense for the session-cookie auth path.
//
// Bearer-API-key requests (Authorization: Bearer …) are skipped: browsers do
// not attach Authorization headers cross-origin on their own, so those
// requests are not CSRF-vulnerable.
//
// Both headers being absent on a state-changing request from a cookie-auth
// caller is treated as suspicious and rejected — a legitimate browser fetch
// always sends at least one of Origin/Referer on POST/PUT/DELETE/PATCH, and
// allowing the unset case would let an attacker bypass the guard by
// constructing a request that suppresses both (e.g. via a referrer-policy
// trick combined with a request type that lacks Origin in older browsers).
//
// GET / HEAD / OPTIONS are never gated — they are not supposed to mutate
// state and disabling them would break the SPA's status polling.
func OriginGuard() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch:
			default:
				next.ServeHTTP(w, r)
				return
			}
			if hasBearerHeader(r) {
				next.ServeHTTP(w, r)
				return
			}
			origin := r.Header.Get("Origin")
			referer := r.Header.Get("Referer")
			if origin == "" && referer == "" {
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "missing origin"})
				return
			}
			if origin != "" && !originHostMatches(origin, r.Host) {
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "origin mismatch"})
				return
			}
			if origin == "" && referer != "" && !originHostMatches(referer, r.Host) {
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "origin mismatch"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// hasBearerHeader reports whether the request carries an Authorization: Bearer
// header. Detecting via header presence (not BearerTokenFromRequest) is
// important: ?key= URL bearer support was removed for security, so we must
// not treat a URL param as a CSRF-immunity hint.
func hasBearerHeader(r *http.Request) bool {
	auth := r.Header.Get("Authorization")
	return strings.HasPrefix(auth, "Bearer ")
}

// originHostMatches returns true iff the host portion of `origin` (a full URL,
// e.g. "https://example.com:8080") equals `host` (e.g. "example.com:8080").
func originHostMatches(origin, host string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	// Compare case-insensitively; hosts are ASCII-only.
	return strings.EqualFold(u.Host, host)
}
