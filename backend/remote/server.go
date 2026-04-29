package remote

import (
	"io/fs"
	"net/http"

	"github.com/go-chi/chi/v5"

	"mosaic/backend/api"
)

// Mount wires all REST routes onto a chi.Router. The /api/login route and the
// optional static SPA tree skip the auth middleware; everything else under
// /api/* is gated.
//
// hub, if non-nil, exposes a WebSocket upgrade at /api/ws.
// staticFS, if non-nil, is served at "/". Pass nil during tests.
func Mount(svc *api.Service, sessions *SessionStore, hub *Hub, staticFS fs.FS, secure bool) chi.Router {
	r := chi.NewRouter()
	h := NewHandlers(svc, sessions, secure)
	gate := AuthGate(sessions, svc)

	r.Route("/api", func(api chi.Router) {
		// public
		api.Post("/login", h.Login)
		// gated
		api.Group(func(g chi.Router) {
			g.Use(gate)
			g.Post("/logout", h.Logout)

			g.Get("/torrents", h.ListTorrents)
			g.Post("/torrents/magnet", h.AddMagnet)
			g.Post("/torrents/file", h.AddTorrentFile)
			g.Post("/torrents/{id}/pause", h.Pause)
			g.Post("/torrents/{id}/resume", h.Resume)
			g.Delete("/torrents/{id}", h.Remove)
			g.Post("/torrents/category", h.SetTorrentCategory)
			g.Post("/torrents/file_priorities", h.SetFilePriorities)
			g.Post("/torrents/queue_position", h.SetQueuePosition)
			g.Post("/torrents/force_start", h.SetForceStart)

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
			g.Get("/settings/blocklist", h.GetBlocklist)
			g.Put("/settings/blocklist", h.SetBlocklist)
			g.Post("/settings/blocklist/refresh", h.RefreshBlocklist)
			g.Get("/settings/web", h.GetWebConfig)
			g.Put("/settings/web", h.SetWebConfig)
			g.Put("/settings/web/password", h.SetWebPassword)
			g.Post("/settings/web/api_key/rotate", h.RotateAPIKey)

			g.Get("/schedule_rules", h.ListScheduleRules)
			g.Post("/schedule_rules", h.CreateScheduleRule)
			g.Put("/schedule_rules", h.UpdateScheduleRule)
			g.Delete("/schedule_rules/{id}", h.DeleteScheduleRule)

			g.Get("/feeds", h.ListFeeds)
			g.Post("/feeds", h.CreateFeed)
			g.Put("/feeds", h.UpdateFeed)
			g.Delete("/feeds/{id}", h.DeleteFeed)
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
