package chat

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
)

const (
	// wsSendBuffer bounds how far a client may fall behind before its
	// connection is dropped. A chat stream must never grow an unbounded
	// per-connection queue; a slow client reconnects and catches up from
	// the REST history instead.
	wsSendBuffer = 64

	wsWriteTimeout = 10 * time.Second

	// wsPingInterval detects dead peers (laptop sleep, dead NAT entries)
	// instead of waiting for TCP timeouts.
	wsPingInterval = 30 * time.Second

	// Clients only send small control frames in this phase.
	wsReadLimit = 4 << 10
)

type wsClient struct {
	hub *Hub

	conn *websocket.Conn

	tenantID string
	userID   string

	send chan []byte

	cancel context.CancelFunc

	closeOnce sync.Once
}

// Stream upgrades GET /chat/stream to a WebSocket. Authentication has
// already run in the chi middleware, using the same headers as the REST
// API. Browser clients cannot set custom headers on a WebSocket handshake;
// short-lived stream tokens are the planned follow-up.
func (h *Handler) Stream(
	w http.ResponseWriter,
	r *http.Request,
) {
	identity, ok := h.identity(w, r)
	if !ok {
		return
	}

	// The HTTP server's ReadTimeout/WriteTimeout stay in place for normal
	// requests, but their absolute deadlines must not survive the upgrade.
	controller := http.NewResponseController(w)
	_ = controller.SetReadDeadline(time.Time{})
	_ = controller.SetWriteDeadline(time.Time{})

	conn, err := websocket.Accept(
		w,
		r,
		&websocket.AcceptOptions{
			OriginPatterns: h.allowedOrigins,
		},
	)
	if err != nil {
		log.Printf("chat stream: accept: %v", err)
		return
	}

	defer conn.CloseNow()

	conn.SetReadLimit(wsReadLimit)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	client := &wsClient{
		hub: h.hub,

		conn: conn,

		tenantID: identity.TenantID,
		userID:   identity.UserID,

		send: make(chan []byte, wsSendBuffer),

		cancel: cancel,
	}

	h.hub.Register(client)
	defer h.hub.Unregister(client)

	hello, err := json.Marshal(ClientEvent{
		Type: string(EventHello),

		UserID: identity.UserID,

		TenantID: identity.TenantID,

		ServerTime: time.Now().UTC(),
	})
	if err != nil {
		log.Printf("chat stream: encode hello: %v", err)
		return
	}

	client.enqueue(hello)

	go client.writePump(ctx)

	// Block until the peer disconnects or the connection is shut down.
	client.readPump(ctx)
}

func (c *wsClient) readPump(ctx context.Context) {
	for {
		// Reading is mandatory: control frames (ping/pong/close) are only
		// processed while a read is in flight.
		if _, _, err := c.conn.Read(ctx); err != nil {
			return
		}
	}
}

func (c *wsClient) writePump(ctx context.Context) {
	ticker := time.NewTicker(wsPingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case payload := <-c.send:
			writeCtx, cancel := context.WithTimeout(
				ctx,
				wsWriteTimeout,
			)

			err := c.conn.Write(
				writeCtx,
				websocket.MessageText,
				payload,
			)

			cancel()

			if err != nil {
				c.shutdown()
				return
			}

		case <-ticker.C:
			pingCtx, cancel := context.WithTimeout(
				ctx,
				wsWriteTimeout,
			)

			err := c.conn.Ping(pingCtx)

			cancel()

			if err != nil {
				c.shutdown()
				return
			}
		}
	}
}

func (c *wsClient) enqueue(payload []byte) {
	select {
	case c.send <- payload:
	default:
		log.Printf(
			"chat stream: slow consumer tenant=%s user=%s; closing",
			c.tenantID,
			c.userID,
		)

		c.shutdown()
	}
}

func (c *wsClient) shutdown() {
	c.closeOnce.Do(func() {
		c.cancel()

		_ = c.conn.CloseNow()
	})
}
