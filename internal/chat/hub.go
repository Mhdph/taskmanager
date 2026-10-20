package chat

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
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

	// publisher fans presence transitions out to every API instance.
	publisher Publisher

	// instanceID identifies this process in presence lease rows. It is
	// operational metadata; correctness comes from the lease itself.
	instanceID string

	// closed stops new connections and tells shutdown to release leases
	// instead of waiting for them to expire.
	closed bool
}

type clientKey struct {
	tenantID string
	userID   string
}

func NewHub(repository *Repository, publisher Publisher) *Hub {
	return &Hub{
		clients: make(
			map[clientKey]map[*wsClient]struct{},
		),

		repository: repository,

		publisher: publisher,

		instanceID: uuid.NewString(),
	}
}

func (h *Hub) isClosed() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	return h.closed
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

	if client.connectionID == "" {
		client.connectionID = uuid.NewString()
	}

	connections := h.clients[key]
	if connections == nil {
		connections = make(map[*wsClient]struct{})
		h.clients[key] = connections
	}

	connections[client] = struct{}{}

	h.mu.Unlock()

	h.touchPresence(ctx, client)
}

func (h *Hub) Unregister(ctx context.Context, client *wsClient) {
	key := clientKey{
		tenantID: client.tenantID,
		userID:   client.userID,
	}

	h.mu.Lock()

	connections := h.clients[key]
	if connections != nil {
		delete(connections, client)

		if len(connections) == 0 {
			delete(h.clients, key)
		}
	}

	h.mu.Unlock()

	h.releasePresence(ctx, client)
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

	case EventPresenceOnline, EventPresenceOffline:
		h.handlePresenceEvent(ctx, event)

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

// touchPresence creates or extends this connection's shared lease. Register
// calls it once; the WebSocket ping loop calls it as a heartbeat.
func (h *Hub) touchPresence(ctx context.Context, client *wsClient) {
	client.presenceMu.Lock()
	defer client.presenceMu.Unlock()

	if client.presenceDone || h.isClosed() {
		return
	}

	if client.presenceHeld {
		found, err := h.repository.RefreshPresence(
			ctx,
			client.connectionID,
			h.instanceID,
			presenceLeaseTTL,
		)
		if err != nil {
			log.Printf(
				"chat hub: refresh presence tenant=%s user=%s: %v",
				client.tenantID,
				client.userID,
				err,
			)
			return
		}

		if found {
			return
		}
	}

	changed, err := h.repository.AcquirePresence(
		ctx,
		PresenceLease{
			ConnectionID: client.connectionID,
			TenantID:     client.tenantID,
			UserID:       client.userID,
			InstanceID:   h.instanceID,
			TTL:          presenceLeaseTTL,
		},
	)
	if err != nil {
		log.Printf(
			"chat hub: acquire presence tenant=%s user=%s: %v",
			client.tenantID,
			client.userID,
			err,
		)
		return
	}

	client.presenceHeld = true

	if changed {
		h.publishPresence(client.tenantID, client.userID, true)
	}
}

// releasePresence removes this connection's lease and publishes the global
// offline transition when it was the user's last connection.
func (h *Hub) releasePresence(ctx context.Context, client *wsClient) {
	client.presenceMu.Lock()
	defer client.presenceMu.Unlock()

	if client.presenceDone {
		return
	}

	client.presenceDone = true

	if !client.presenceHeld {
		return
	}

	client.presenceHeld = false

	changed, err := h.repository.ReleasePresence(
		ctx,
		client.connectionID,
		client.tenantID,
		client.userID,
	)
	if err != nil {
		log.Printf(
			"chat hub: release presence tenant=%s user=%s: %v",
			client.tenantID,
			client.userID,
			err,
		)
		return
	}

	if changed {
		h.publishPresence(client.tenantID, client.userID, false)
	}
}

// publishPresence emits a transition through the broker. Every instance,
// including this one, receives it and fans it out to local recipients.
func (h *Hub) publishPresence(
	tenantID string,
	userID string,
	online bool,
) {
	if h.publisher == nil {
		return
	}

	kind := EventPresenceOnline
	if !online {
		kind = EventPresenceOffline
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		5*time.Second,
	)
	defer cancel()

	if err := h.publisher.Publish(ctx, Event{
		Kind:        kind,
		TenantID:    tenantID,
		ActorUserID: userID,
	}); err != nil {
		log.Printf(
			"chat hub: publish %s tenant=%s user=%s: %v",
			kind,
			tenantID,
			userID,
			err,
		)
	}
}

// RunPresenceCleanup sweeps expired leases at startup and then periodically.
// It must run on every instance: a crashed instance leaves leases behind,
// and any survivor can reclaim the offline transition once they expire.
func (h *Hub) RunPresenceCleanup(ctx context.Context) {
	h.sweepPresence(ctx)

	ticker := time.NewTicker(presenceSweepInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.sweepPresence(ctx)
		}
	}
}

func (h *Hub) sweepPresence(ctx context.Context) {
	if h.isClosed() {
		return
	}

	updates, err := h.repository.SweepExpiredPresence(
		ctx,
		presenceSweepBatch,
	)
	if err != nil {
		log.Printf("chat hub: sweep presence: %v", err)
		return
	}

	for _, update := range updates {
		h.publishPresence(
			update.TenantID,
			update.UserID,
			update.Online,
		)
	}
}

// handlePresenceEvent re-checks the shared store before broadcasting. Events
// can arrive out of order after a rapid reconnect; the current lease state
// decides which transition is still true.
func (h *Hub) handlePresenceEvent(
	ctx context.Context,
	event Event,
) {
	if event.ActorUserID == "" {
		return
	}

	online, err := h.repository.IsUserOnline(
		ctx,
		event.TenantID,
		event.ActorUserID,
	)
	if err != nil {
		log.Printf("chat hub: check presence: %v", err)
		return
	}

	if online != (event.Kind == EventPresenceOnline) {
		return
	}

	h.broadcastPresence(
		ctx,
		event.TenantID,
		event.ActorUserID,
		online,
	)
}

// broadcastPresence tells every online user who shares a conversation with
// the actor that the actor came online or went offline.
func (h *Hub) broadcastPresence(
	ctx context.Context,
	tenantID string,
	userID string,
	online bool,
) {
	conversationIDs, err := h.repository.ListConversationIDsForUser(
		ctx,
		tenantID,
		userID,
	)
	if err != nil {
		log.Printf("chat hub: list presence conversations: %v", err)
		return
	}

	recipients := make(map[string]struct{})

	for _, conversationID := range conversationIDs {
		members, err := h.repository.ListMembers(
			ctx,
			tenantID,
			conversationID,
		)
		if err != nil {
			log.Printf("chat hub: list presence members: %v", err)
			continue
		}

		for _, member := range members {
			if member.UserID != userID {
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

		UserID: userID,

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

	h.broadcast(tenantID, userIDs, payload)
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

// Close terminates every WebSocket connection and releases its presence
// lease. http.Server.Shutdown does not close hijacked connections, so this
// must run explicitly. Releasing synchronously lets other instances see the
// offline transition immediately instead of waiting for the lease TTL.
func (h *Hub) Close(ctx context.Context) {
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

	for _, client := range clients {
		h.releasePresence(ctx, client)
	}
}
