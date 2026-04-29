package remote

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"nhooyr.io/websocket"

	"mosaic/backend/api"
	"mosaic/backend/events"
)

// Envelope is the WS frame shape. Clients receive `{type, payload}` JSON.
type Envelope struct {
	Type    string `json:"type"`
	Payload any    `json:"payload"`
}

// Hub fans out backend tick events to all connected WebSocket clients. Producers
// (typically the main.go ticker goroutine) call Publish*; consumers connect via
// HandleUpgrade.
type Hub struct {
	bus *events.Bus[Envelope]

	mu      sync.Mutex
	clients map[*hubClient]struct{}
}

// NewHub returns an empty hub. Call Run(ctx) to start fan-out.
func NewHub() *Hub {
	return &Hub{
		bus:     events.NewBus[Envelope](256),
		clients: make(map[*hubClient]struct{}),
	}
}

// Run consumes the internal bus and pushes envelopes to every connected
// client's send channel. Returns when ctx is done.
func (h *Hub) Run(ctx context.Context) {
	sub := h.bus.Subscribe()
	for {
		select {
		case <-ctx.Done():
			return
		case env, ok := <-sub:
			if !ok {
				return
			}
			h.broadcast(env)
		}
	}
}

func (h *Hub) broadcast(env Envelope) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		select {
		case c.send <- env:
		default: // slow client: drop
		}
	}
}

// PublishTorrents emits a torrents:tick frame to all connected clients.
func (h *Hub) PublishTorrents(rows []api.TorrentDTO) {
	h.bus.Publish(Envelope{Type: "torrents:tick", Payload: rows})
}

// PublishStats emits a stats:tick frame.
func (h *Hub) PublishStats(s api.GlobalStats) {
	h.bus.Publish(Envelope{Type: "stats:tick", Payload: s})
}

// PublishInspector emits an inspector:tick frame.
func (h *Hub) PublishInspector(d api.DetailDTO) {
	h.bus.Publish(Envelope{Type: "inspector:tick", Payload: d})
}

// Close detaches all clients and shuts down the internal bus.
func (h *Hub) Close() {
	h.bus.Close()
	h.mu.Lock()
	for c := range h.clients {
		close(c.send)
	}
	h.clients = nil
	h.mu.Unlock()
}

// ClientCount is exposed for tests + status reporting.
func (h *Hub) ClientCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}

type hubClient struct {
	send chan Envelope
}

func (h *Hub) addClient() *hubClient {
	c := &hubClient{send: make(chan Envelope, 64)}
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
	return c
}

func (h *Hub) removeClient(c *hubClient) {
	h.mu.Lock()
	if _, ok := h.clients[c]; ok {
		delete(h.clients, c)
		close(c.send)
	}
	h.mu.Unlock()
}

// HandleUpgrade returns an http.HandlerFunc that upgrades the request to a
// WebSocket and pumps frames from the per-client buffered channel until either
// side disconnects. Auth is checked inline (cookie OR bearer) so the upgrade
// response is correct.
func (h *Hub) HandleUpgrade(sessions *SessionStore, creds CredentialChecker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !sessions.Valid(SessionTokenFromRequest(r)) {
			key := BearerTokenFromRequest(r)
			if key == "" || !creds.VerifyAPIKey(r.Context(), key) {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
		}

		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			InsecureSkipVerify: true, // we don't enforce Origin; auth gate already ran
		})
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusInternalError, "closing")

		client := h.addClient()
		defer h.removeClient(client)

		ctx := r.Context()
		peerGone := make(chan struct{})
		go func() {
			defer close(peerGone)
			// Drain reads (and detect peer close) but ignore content.
			for {
				if _, _, err := conn.Read(ctx); err != nil {
					return
				}
			}
		}()

		for {
			select {
			case <-ctx.Done():
				return
			case <-peerGone:
				return
			case env, ok := <-client.send:
				if !ok {
					return
				}
				writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				err := writeJSONFrame(writeCtx, conn, env)
				cancel()
				if err != nil {
					return
				}
			}
		}
	}
}

func writeJSONFrame(ctx context.Context, conn *websocket.Conn, v any) error {
	w, err := conn.Writer(ctx, websocket.MessageText)
	if err != nil {
		return err
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		_ = w.Close()
		return err
	}
	return w.Close()
}
