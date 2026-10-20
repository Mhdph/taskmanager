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

	// closed stops presence bookkeeping during shutdown, when every
	// connection is being torn down anyway.
	closed bool
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

func (h *Hub) Register(ctx context.Context, client *wsClient) {
	key := clientKey{
		tenantID: client.tenantID,
		userID:   client.userID,
	}

	h.mu.Lock()

	if h.closed {
		h.mu.Unlock()

		return
	}

	connections := h.clients[key]
	wentOnline := len(connections) == 0

	if connections == nil {
		connections = make(map[*wsClient]struct{})
		h.clients[key] = connections
	}

	connections[client] = struct{}{}

	h.mu.Unlock()

	if wentOnline {
		h.broadcastPresence(ctx, key, true)
	}
}

func (h *Hub) Unregister(ctx context.Context, client *wsClient) {
	key := clientKey{
		tenantID: client.tenantID,
		userID:   client.userID,
	}

	h.mu.Lock()

	connections := h.clients[key]
	if connections == nil {
		h.mu.Unlock()

		return
	}

	delete(connections, client)

	wentOffline := len(connections) == 0
	if wentOffline {
		delete(h.clients, key)
	}

	closed := h.closed

	h.mu.Unlock()

	if wentOffline && !closed {
		h.broadcastPresence(ctx, key, false)
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

	case EventTyping:
		h.handleTypingEvent(ctx, event)

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

func (h *Hub) handleTypingEvent(
	ctx context.Context,
	event Event,
) {
	members, err := h.repository.ListMembers(
		ctx,
		event.TenantID,
		event.ConversationID,
	)
	if err != nil {
		log.Printf("chat hub: list members: %v", err)
		return
	}

	typing := event.Typing

	payload, err := json.Marshal(ClientEvent{
		Type: string(EventTyping),

		ConversationID: event.ConversationID,

		UserID: event.ActorUserID,

		Typing: &typing,

		ServerTime: time.Now().UTC(),
	})
	if err != nil {
		log.Printf("chat hub: encode typing event: %v", err)
		return
	}

	userIDs := make([]string, 0, len(members))

	for _, member := range members {
		// Do not echo the indicator back to the typist.
		if member.UserID == event.ActorUserID {
			continue
		}

		userIDs = append(userIDs, member.UserID)
	}

	h.broadcast(event.TenantID, userIDs, payload)
}

// broadcastPresence tells every online user who shares a conversation with
// the actor that the actor came online or went offline.
//
// Presence is instance-local: two API instances do not know about each
// other's connections. Making it correct across instances needs a shared
// lease/heartbeat store (Redis or a presence table); until then the REST
// snapshot and these events describe connections handled by this process.
func (h *Hub) broadcastPresence(
	ctx context.Context,
	key clientKey,
	online bool,
) {
	conversationIDs, err := h.repository.ListConversationIDsForUser(
		ctx,
		key.tenantID,
		key.userID,
	)
	if err != nil {
		log.Printf("chat hub: list presence conversations: %v", err)
		return
	}

	recipients := make(map[string]struct{})

	for _, conversationID := range conversationIDs {
		members, err := h.repository.ListMembers(
			ctx,
			key.tenantID,
			conversationID,
		)
		if err != nil {
			log.Printf("chat hub: list presence members: %v", err)
			continue
		}

		for _, member := range members {
			if member.UserID != key.userID {
				recipients[member.UserID] = struct{}{}
			}
		}
	}

	if len(recipients) == 0 {
		return
	}

	kind := EventPresenceOnline
	if !online {
		kind = EventPresenceOffline
	}

	payload, err := json.Marshal(ClientEvent{
		Type: string(kind),

		UserID: key.userID,

		ServerTime: time.Now().UTC(),
	})
	if err != nil {
		log.Printf("chat hub: encode presence event: %v", err)
		return
	}

	userIDs := make([]string, 0, len(recipients))
	for userID := range recipients {
		userIDs = append(userIDs, userID)
	}

	h.broadcast(key.tenantID, userIDs, payload)
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

	h.closed = true

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
