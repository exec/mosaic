package remote

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
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
//
// Frames are serialized to JSON *once* at the Publish boundary and the resulting
// bytes are what travel through the bus and the per-client channels — every
// connection then writes the same pre-encoded []byte rather than each running
// its own json.Encode of an identical Envelope. With many connected clients
// (mosaicd, multi-tab) that turns an O(clients) marshal per tick into O(1).
type Hub struct {
	bus *events.Bus[[]byte]

	mu      sync.Mutex
	clients map[*hubClient]struct{}
}

// NewHub returns an empty hub. Call Run(ctx) to start fan-out.
func NewHub() *Hub {
	return &Hub{
		bus:     events.NewBus[[]byte](256),
		clients: make(map[*hubClient]struct{}),
	}
}

// Run consumes the internal bus and pushes pre-encoded frames to every
// connected client's send channel. Returns when ctx is done.
func (h *Hub) Run(ctx context.Context) {
	sub := h.bus.Subscribe()
	for {
		select {
		case <-ctx.Done():
			return
		case frame, ok := <-sub:
			if !ok {
				return
			}
			h.broadcast(frame)
		}
	}
}

func (h *Hub) broadcast(frame []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		select {
		case c.send <- frame:
		default: // slow client: drop
		}
	}
}

// encodeFrame marshals an Envelope to its wire JSON. A marshal failure here
// would mean a non-serializable payload (a programming error); we log and
// return nil so callers can skip the publish rather than panic.
func encodeFrame(typ string, payload any) []byte {
	b, err := json.Marshal(Envelope{Type: typ, Payload: payload})
	if err != nil {
		log.Error().Err(err).Str("type", typ).Msg("ws: encode frame")
		return nil
	}
	return b
}

// PublishTorrents emits a torrents:tick frame to all connected clients.
func (h *Hub) PublishTorrents(rows []api.TorrentDTO) {
	if frame := encodeFrame("torrents:tick", rows); frame != nil {
		h.bus.Publish(frame)
	}
}

// PublishStats emits a stats:tick frame.
func (h *Hub) PublishStats(s api.GlobalStats) {
	if frame := encodeFrame("stats:tick", s); frame != nil {
		h.bus.Publish(frame)
	}
}

// PublishInspector emits an inspector:tick frame.
func (h *Hub) PublishInspector(d api.DetailDTO) {
	if frame := encodeFrame("inspector:tick", d); frame != nil {
		h.bus.Publish(frame)
	}
}

// PublishUpdate emits an update:available frame to all connected clients.
func (h *Hub) PublishUpdate(info api.UpdateInfoDTO) {
	if frame := encodeFrame("update:available", info); frame != nil {
		h.bus.Publish(frame)
	}
}

// sendFrameToUser pushes a pre-encoded frame to every connected client owned by
// userID. Used by mosaicd's per-user tick fan-out so one user never receives
// another user's torrents/stats/inspector data.
func (h *Hub) sendFrameToUser(userID int, frame []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		if c.caller.UserID != userID {
			continue
		}
		select {
		case c.send <- frame:
		default: // slow client: drop
		}
	}
}

// PublishTorrentsTo emits a torrents:tick frame to one user's clients only.
func (h *Hub) PublishTorrentsTo(userID int, rows []api.TorrentDTO) {
	if frame := encodeFrame("torrents:tick", rows); frame != nil {
		h.sendFrameToUser(userID, frame)
	}
}

// PublishTorrentsRawTo sends an already-encoded torrents:tick frame to one
// user's clients. mosaicd's streamTicks encodes the shared admin frame once and
// fans the identical bytes out to every admin via this path.
func (h *Hub) PublishTorrentsRawTo(userID int, frame []byte) {
	if frame != nil {
		h.sendFrameToUser(userID, frame)
	}
}

// EncodeTorrentsFrame serializes a torrents:tick payload to its wire bytes so a
// caller can encode once and fan the result out via PublishTorrentsRawTo.
// Returns nil on a (programming-error) marshal failure.
func EncodeTorrentsFrame(rows []api.TorrentDTO) []byte {
	return encodeFrame("torrents:tick", rows)
}

// PublishStatsTo emits a stats:tick frame to one user's clients only.
func (h *Hub) PublishStatsTo(userID int, s api.GlobalStats) {
	if frame := encodeFrame("stats:tick", s); frame != nil {
		h.sendFrameToUser(userID, frame)
	}
}

// PublishInspectorTo emits an inspector:tick frame to one user's clients only.
func (h *Hub) PublishInspectorTo(userID int, d api.DetailDTO) {
	if frame := encodeFrame("inspector:tick", d); frame != nil {
		h.sendFrameToUser(userID, frame)
	}
}

// ConnectedUserIDs returns the distinct user ids with at least one live
// WebSocket client. mosaicd's streamTicks iterates these to compute and push
// each user's filtered view.
func (h *Hub) ConnectedUserIDs() []int {
	h.mu.Lock()
	defer h.mu.Unlock()
	seen := make(map[int]struct{}, len(h.clients))
	for c := range h.clients {
		seen[c.caller.UserID] = struct{}{}
	}
	out := make([]int, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	return out
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
	send   chan []byte
	caller api.Caller
	// conn is held so RevokeUser can synchronously close the underlying
	// socket with a policy-violation status code, signalling the SPA to
	// fall back to the login screen.
	conn *websocket.Conn
}

func (h *Hub) addClient(caller api.Caller, conn *websocket.Conn) *hubClient {
	c := &hubClient{send: make(chan []byte, 64), caller: caller, conn: conn}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.clients == nil {
		// Hub has been Closed; refuse to register so the caller's defer
		// removeClient becomes a no-op and we don't keep a doomed channel
		// alive. Caller treats nil as a closed-channel signal.
		close(c.send)
		return c
	}
	h.clients[c] = struct{}{}
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

// RevokeUser hangs up every live WebSocket belonging to userID. It is the
// counterpart to SessionStore.RevokeUser: when a user is deleted, disabled,
// renamed, or has their password changed, the session is gone but an already-
// upgraded WebSocket would otherwise keep streaming until it next tried to
// re-auth. The conn.Close races with the per-conn read loop noticing the
// closed socket; the periodic Valid() check in HandleUpgrade is the secondary
// safety net. Wired into api.Service.invalidateCaller via AttachWSRevoker
// from the binary's startup (cmd/mosaicd/main.go and main.go).
func (h *Hub) RevokeUser(userID int) {
	h.mu.Lock()
	var victims []*hubClient
	for c := range h.clients {
		if c.caller.UserID == userID {
			victims = append(victims, c)
		}
	}
	h.mu.Unlock()
	// Close outside the hub lock so the loop in HandleUpgrade can call
	// removeClient (which re-acquires it) without deadlocking us.
	for _, c := range victims {
		if c.conn != nil {
			_ = c.conn.Close(websocket.StatusPolicyViolation, "session revoked")
		}
	}
}

// wsSessionRecheckInterval is the cadence at which a live WebSocket
// re-validates its session token. Cheaper than checking on every frame and
// still fast enough that a logged-out user's stream stops within ~30s of
// their session disappearing.
const wsSessionRecheckInterval = 30 * time.Second

// HandleUpgrade returns an http.HandlerFunc that upgrades the request to a
// WebSocket and pumps frames from the per-client buffered channel until either
// side disconnects. Auth is checked inline (cookie OR bearer) so the upgrade
// response is correct.
func (h *Hub) HandleUpgrade(sessions *SessionStore, res CallerResolver) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		caller, ok := resolveCaller(r, sessions, res)
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		// Capture the session token (if any) so the loop below can re-validate
		// it periodically. Bearer-keyed callers have token == "" and skip the
		// periodic check — RevokeUser still hangs up their socket directly
		// when the underlying user is mutated.
		sessionToken := SessionTokenFromRequest(r)

		// Pin the Origin to the request Host to prevent Cross-Site WebSocket
		// Hijacking: if a logged-in user visits a malicious page, the browser
		// will happily attach our session cookie to a WS Upgrade — only the
		// Origin header tells us the request actually came from our own SPA.
		// Bearer-keyed callers (no cookie) aren't a CSWH risk but the same
		// allow-list still lets them through because nhooyr only enforces
		// OriginPatterns when the Origin header is set, which non-browser
		// clients typically do not.
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			OriginPatterns: []string{r.Host},
		})
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusInternalError, "closing")

		client := h.addClient(caller, conn)
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

		recheck := time.NewTicker(wsSessionRecheckInterval)
		defer recheck.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-peerGone:
				return
			case <-recheck.C:
				if sessionToken != "" {
					if _, ok := sessions.Valid(sessionToken); !ok {
						conn.Close(websocket.StatusPolicyViolation, "session revoked")
						return
					}
				}
			case frame, ok := <-client.send:
				if !ok {
					// Channel was closed by RevokeUser or hub shutdown — the
					// kicker also called conn.Close, but exit promptly so the
					// defer can run.
					return
				}
				writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				err := writeRawFrame(writeCtx, conn, frame)
				cancel()
				if err != nil {
					return
				}
			}
		}
	}
}

// writeRawFrame writes a pre-encoded JSON frame as a single text message. The
// bytes were marshaled once at the Publish boundary (see encodeFrame); writing
// them raw avoids a redundant per-connection json.Encode of identical content.
func writeRawFrame(ctx context.Context, conn *websocket.Conn, frame []byte) error {
	return conn.Write(ctx, websocket.MessageText, frame)
}
