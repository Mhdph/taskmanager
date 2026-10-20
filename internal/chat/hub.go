package chat

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"
)

// Hub is the in-process fan-out layer: it owns the WebSocket connections
// for this instance and turns broker events into client frames.
//
// It is intentionally not the source of truth. Broker events reference
// database rows; the hub hydrates them before pushing, so clients always
// receive committed state.
type Hub struct {
	mu sync.RWMutex

	clients map[clientKey]map[*wsClient]struct{}

	repository *Repository
}

type clientKey struct {
	tenantID string
	userID   string
}

func NewHub(repository *Repository) *Hub {
	return &Hub{
		clients: make(
			map[clientKey]map[*wsClient]struct{},
		),

		repository: repository,
	}
}

func (h *Hub) Register(client *wsClient) {
	h.mu.Lock()
	defer h.mu.Unlock()

	key := clientKey{
		tenantID: client.tenantID,
		userID:   client.userID,
	}

	connections := h.clients[key]
	if connections == nil {
		connections = make(map[*wsClient]struct{})
		h.clients[key] = connections
	}

	connections[client] = struct{}{}
}

func (h *Hub) Unregister(client *wsClient) {
	h.mu.Lock()
	defer h.mu.Unlock()

	key := clientKey{
		tenantID: client.tenantID,
		userID:   client.userID,
	}

	connections := h.clients[key]
	if connections == nil {
		return
	}

	delete(connections, client)

	if len(connections) == 0 {
		delete(h.clients, key)
	}
}

func (h *Hub) IsOnline(
	tenantID string,
	userID string,
) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	return len(h.clients[clientKey{tenantID: tenantID, userID: userID}]) > 0
}

// HandleEvent hydrates a broker event and pushes it to every online member
// of the conversation. Membership is read from the database for each event,
// so joining or leaving a conversation takes effect immediately, even for
// connections opened before the change.
func (h *Hub) HandleEvent(
	ctx context.Context,
	event Event,
) {
	switch event.Kind {
	case EventMessageCreated,
		EventMessageEdited,
		EventMessageDeleted:

		h.handleMessageEvent(ctx, event)

	case EventConversationRead:
		h.handleReadEvent(ctx, event)

	default:
		log.Printf(
			"chat hub: ignoring unknown event kind %q",
			event.Kind,
		)
	}
}

func (h *Hub) handleMessageEvent(
	ctx context.Context,
	event Event,
) {
	message, err := h.repository.GetMessage(
		ctx,
		event.TenantID,
		event.ConversationID,
		event.MessageID,
	)
	if err != nil {
		// The conversation may have been deleted between commit and
		// hydration; there is nobody left to notify.
		log.Printf(
			"chat hub: hydrate %s: %v",
			event.Kind,
			err,
		)
		return
	}

	wire := NewWireMessage(message)

	payload, err := json.Marshal(ClientEvent{
		Type: string(event.Kind),

		ConversationID: event.ConversationID,

		Seq: message.Seq,

		Message: &wire,

		ServerTime: time.Now().UTC(),
	})
	if err != nil {
		log.Printf("chat hub: encode message event: %v", err)
		return
	}

	members, err := h.repository.ListMembers(
		ctx,
		event.TenantID,
		event.ConversationID,
	)
	if err != nil {
		log.Printf("chat hub: list members: %v", err)
		return
	}

	userIDs := make([]string, 0, len(members))
	for _, member := range members {
		userIDs = append(userIDs, member.UserID)
	}

	h.broadcast(event.TenantID, userIDs, payload)
}

func (h *Hub) handleReadEvent(
	ctx context.Context,
	event Event,
) {
	payload, err := json.Marshal(ClientEvent{
		Type: string(EventConversationRead),

		ConversationID: event.ConversationID,

		UserID: event.ActorUserID,

		LastReadSeq: event.LastReadSeq,

		ServerTime: time.Now().UTC(),
	})
	if err != nil {
		log.Printf("chat hub: encode read event: %v", err)
		return
	}

	members, err := h.repository.ListMembers(
		ctx,
		event.TenantID,
		event.ConversationID,
	)
	if err != nil {
		log.Printf("chat hub: list members: %v", err)
		return
	}

	userIDs := make([]string, 0, len(members))
	for _, member := range members {
		userIDs = append(userIDs, member.UserID)
	}

	h.broadcast(event.TenantID, userIDs, payload)
}

func (h *Hub) broadcast(
	tenantID string,
	userIDs []string,
	payload []byte,
) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, userID := range userIDs {
		connections := h.clients[clientKey{
			tenantID: tenantID,
			userID:   userID,
		}]

		for client := range connections {
			client.enqueue(payload)
		}
	}
}

// Close terminates every WebSocket connection. http.Server.Shutdown does
// not close hijacked connections, so this must run explicitly.
func (h *Hub) Close() {
	h.mu.Lock()

	clients := make([]*wsClient, 0, len(h.clients))

	for _, connections := range h.clients {
		for client := range connections {
			clients = append(clients, client)
		}
	}

	h.clients = make(
		map[clientKey]map[*wsClient]struct{},
	)

	h.mu.Unlock()

	for _, client := range clients {
		client.shutdown()
	}
}
