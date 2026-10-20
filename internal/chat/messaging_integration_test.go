package chat

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mhdph/taskmanager/internal/auth"
	db "github.com/mhdph/taskmanager/internal/database/generated"
)

func TestServiceSendEditDeleteAndIdempotency(t *testing.T) {
	service, pool, ctx := integrationChatService(t)

	const tenant = "tenant-a"

	addTenantMembers(t, pool, ctx, tenant, "alice", "bob", "eve")

	alice := auth.Identity{TenantID: tenant, UserID: "alice"}
	bob := auth.Identity{TenantID: tenant, UserID: "bob"}
	eve := auth.Identity{TenantID: tenant, UserID: "eve"}

	detail, err := service.CreateGroup(ctx, CreateGroupInput{
		Identity:  alice,
		Title:     "decisions",
		MemberIDs: []string{"bob"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, _, err := service.SendMessage(ctx, SendMessageInput{
		Identity:       alice,
		ConversationID: detail.ID,
		Body:           "   ",
	}); !errors.Is(err, ErrInvalidBody) {
		t.Fatalf("empty body error = %v, want ErrInvalidBody", err)
	}

	if _, _, err := service.SendMessage(ctx, SendMessageInput{
		Identity:       alice,
		ConversationID: detail.ID,
		Body:           string(make([]byte, maxMessageBodyLength+1)),
	}); !errors.Is(err, ErrInvalidBody) {
		t.Fatalf("oversized body error = %v, want ErrInvalidBody", err)
	}

	clientMessageID := uuid.NewString()

	first, created, err := service.SendMessage(ctx, SendMessageInput{
		Identity:        alice,
		ConversationID:  detail.ID,
		Body:            "  should we ship friday?  ",
		ClientMessageID: &clientMessageID,
	})
	if err != nil || !created {
		t.Fatalf("send message: created=%v err=%v", created, err)
	}
	if first.Body != "should we ship friday?" {
		t.Fatalf("body = %q, want trimmed body", first.Body)
	}

	// A retry with the same client message id returns the original row.
	retry, created, err := service.SendMessage(ctx, SendMessageInput{
		Identity:        alice,
		ConversationID:  detail.ID,
		Body:            "different text",
		ClientMessageID: &clientMessageID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Fatal("retry created a second message")
	}
	if retry.ID != first.ID || retry.Body != first.Body || retry.Seq != first.Seq {
		t.Fatalf("retry = %+v, want original %+v", retry, first)
	}

	// Client message ids are scoped to a conversation: reusing one in a
	// different conversation must create a new message, never return the
	// other conversation's message.
	otherDetail, err := service.CreateGroup(ctx, CreateGroupInput{
		Identity:  alice,
		Title:     "other decisions",
		MemberIDs: []string{"bob"},
	})
	if err != nil {
		t.Fatal(err)
	}

	elsewhere, created, err := service.SendMessage(ctx, SendMessageInput{
		Identity:        alice,
		ConversationID:  otherDetail.ID,
		Body:            "same id, other conversation",
		ClientMessageID: &clientMessageID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("same client message id in another conversation was deduplicated")
	}
	if elsewhere.ID == first.ID || elsewhere.ConversationID != otherDetail.ID {
		t.Fatalf("message leaked across conversations: %+v", elsewhere)
	}

	// A workspace member who is not in the conversation cannot send.
	if _, _, err := service.SendMessage(ctx, SendMessageInput{
		Identity:       eve,
		ConversationID: detail.ID,
		Body:           "not allowed",
	}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("non-member send error = %v, want ErrForbidden", err)
	}

	bobMessage, _, err := service.SendMessage(ctx, SendMessageInput{
		Identity:       bob,
		ConversationID: detail.ID,
		Body:           "bob was here",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Only the author can edit.
	if _, err := service.EditMessage(ctx, EditMessageInput{
		Identity:       bob,
		ConversationID: detail.ID,
		MessageID:      first.ID,
		Body:           "hijacked",
	}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("non-author edit error = %v, want ErrForbidden", err)
	}

	edited, err := service.EditMessage(ctx, EditMessageInput{
		Identity:       alice,
		ConversationID: detail.ID,
		MessageID:      first.ID,
		Body:           "ship monday",
	})
	if err != nil {
		t.Fatal(err)
	}
	if edited.Body != "ship monday" || edited.EditedAt == nil {
		t.Fatalf("edited message = %+v, want new body and editedAt", edited)
	}
	if edited.Seq != first.Seq {
		t.Fatalf("edit changed seq: %d != %d", edited.Seq, first.Seq)
	}

	// A regular member cannot delete someone else's message...
	if _, err := service.DeleteMessage(ctx, DeleteMessageInput{
		Identity:       bob,
		ConversationID: detail.ID,
		MessageID:      first.ID,
	}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member moderation error = %v, want ErrForbidden", err)
	}

	// ...but the group owner can moderate any message.
	deleted, err := service.DeleteMessage(ctx, DeleteMessageInput{
		Identity:       alice,
		ConversationID: detail.ID,
		MessageID:      bobMessage.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if deleted.Body != "" || deleted.DeletedAt == nil {
		t.Fatalf("deleted message = %+v, want a tombstone", deleted)
	}

	// Authors can always delete their own messages.
	if _, err := service.DeleteMessage(ctx, DeleteMessageInput{
		Identity:       alice,
		ConversationID: detail.ID,
		MessageID:      first.ID,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresBrokerPublishesTransactionalEvents(t *testing.T) {
	repository, pool, ctx := integrationChatRepository(t)

	const tenant = "tenant-a"

	addTenantMembers(t, pool, ctx, tenant, "alice", "bob")

	broker := NewPostgresBroker(pool)

	events := make(chan Event, 16)

	if err := broker.Start(ctx, func(_ context.Context, event Event) {
		events <- event
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = broker.Close()
	})

	// Writes through this repository publish inside the write transaction.
	publisherRepository := NewRepository(pool, db.New(pool), broker)

	detail, err := publisherRepository.CreateGroupConversation(
		ctx,
		CreateGroupConversationParams{
			ID:              uuid.NewString(),
			TenantID:        tenant,
			Title:           "broker",
			CreatedByUserID: "alice",
			MemberIDs:       []string{"bob"},
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	alice := "alice"

	message, _, err := publisherRepository.CreateMessage(ctx, CreateMessageParams{
		ID:             uuid.NewString(),
		TenantID:       tenant,
		ConversationID: detail.ID,
		Kind:           MessageKindUser,
		AuthorUserID:   &alice,
		Body:           "transactional hello",
	})
	if err != nil {
		t.Fatal(err)
	}

	createdEvent := waitForEvent(t, events, EventMessageCreated)
	if createdEvent.ConversationID != detail.ID || createdEvent.MessageID != message.ID {
		t.Fatalf("created event = %+v, want message %s", createdEvent, message.ID)
	}

	// The event is a reference: the listener hydrates committed state.
	hydrated, err := repository.GetMessage(ctx, tenant, detail.ID, message.ID)
	if err != nil {
		t.Fatal(err)
	}
	if hydrated.Body != "transactional hello" || hydrated.Seq != message.Seq {
		t.Fatalf("hydrated = %+v, want the committed message", hydrated)
	}

	if _, err := publisherRepository.EditMessage(
		ctx,
		tenant,
		detail.ID,
		message.ID,
		"edited",
	); err != nil {
		t.Fatal(err)
	}

	editEvent := waitForEvent(t, events, EventMessageEdited)
	if editEvent.MessageID != message.ID || editEvent.Seq != message.Seq {
		t.Fatalf("edit event = %+v", editEvent)
	}

	if _, _, err := publisherRepository.DeleteMessage(
		ctx,
		tenant,
		detail.ID,
		message.ID,
	); err != nil {
		t.Fatal(err)
	}

	deleteEvent := waitForEvent(t, events, EventMessageDeleted)
	if deleteEvent.MessageID != message.ID {
		t.Fatalf("delete event = %+v", deleteEvent)
	}

	if err := broker.Publish(ctx, Event{
		Kind:           EventConversationRead,
		TenantID:       tenant,
		ConversationID: detail.ID,
		ActorUserID:    "bob",
		LastReadSeq:    message.Seq,
	}); err != nil {
		t.Fatal(err)
	}

	readEvent := waitForEvent(t, events, EventConversationRead)
	if readEvent.ActorUserID != "bob" || readEvent.LastReadSeq != message.Seq {
		t.Fatalf("read event = %+v", readEvent)
	}
}

func TestHubHydratesAndRoutesEvents(t *testing.T) {
	repository, pool, ctx := integrationChatRepository(t)

	const tenant = "tenant-a"

	addTenantMembers(t, pool, ctx, tenant, "alice", "bob", "eve")

	detail, err := repository.CreateGroupConversation(
		ctx,
		CreateGroupConversationParams{
			ID:              uuid.NewString(),
			TenantID:        tenant,
			Title:           "hub",
			CreatedByUserID: "alice",
			MemberIDs:       []string{"bob"},
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	alice := "alice"

	attachment, err := repository.CreateAttachment(ctx, CreateAttachmentParams{
		ID:               uuid.NewString(),
		TenantID:         tenant,
		ConversationID:   detail.ID,
		UploadedByUserID: "alice",
		Filename:         "fanout.txt",
		ContentType:      "text/plain",
		SizeBytes:        7,
		ObjectKey:        "tests/" + uuid.NewString(),
	})
	if err != nil {
		t.Fatal(err)
	}

	message, _, err := repository.CreateMessage(ctx, CreateMessageParams{
		ID:             uuid.NewString(),
		TenantID:       tenant,
		ConversationID: detail.ID,
		Kind:           MessageKindUser,
		AuthorUserID:   &alice,
		Body:           "fan out",
		AttachmentIDs:  []string{attachment.ID},
	})
	if err != nil {
		t.Fatal(err)
	}

	hub := NewHub(repository)

	bobClient := &wsClient{
		tenantID: tenant,
		userID:   "bob",
		send:     make(chan []byte, 4),
	}

	eveClient := &wsClient{
		tenantID: tenant,
		userID:   "eve",
		send:     make(chan []byte, 4),
	}

	hub.Register(ctx, bobClient)
	hub.Register(ctx, eveClient)
	t.Cleanup(func() {
		hub.Unregister(ctx, bobClient)
		hub.Unregister(ctx, eveClient)
	})

	hub.HandleEvent(ctx, Event{
		Kind:           EventMessageCreated,
		TenantID:       tenant,
		ConversationID: detail.ID,
		MessageID:      message.ID,
		Seq:            message.Seq,
	})

	payload := <-bobClient.send

	var event ClientEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		t.Fatal(err)
	}

	if event.Type != string(EventMessageCreated) {
		t.Fatalf("event type = %q", event.Type)
	}
	if event.Message == nil || event.Message.ID != message.ID || event.Message.Body != "fan out" {
		t.Fatalf("event message = %+v", event.Message)
	}
	if len(event.Message.Attachments) != 1 ||
		event.Message.Attachments[0].Filename != "fanout.txt" {
		t.Fatalf(
			"event attachments = %+v, want the hydrated attachment",
			event.Message.Attachments,
		)
	}
	if event.Seq != message.Seq || event.ConversationID != detail.ID {
		t.Fatalf("event routing = %+v", event)
	}

	// Eve is not a conversation member: nothing is pushed to her.
	select {
	case payload := <-eveClient.send:
		t.Fatalf("non-member received event: %s", payload)
	default:
	}

	hub.HandleEvent(ctx, Event{
		Kind:           EventConversationRead,
		TenantID:       tenant,
		ConversationID: detail.ID,
		ActorUserID:    "alice",
		LastReadSeq:    message.Seq,
	})

	payload = <-bobClient.send

	if err := json.Unmarshal(payload, &event); err != nil {
		t.Fatal(err)
	}
	if event.Type != string(EventConversationRead) ||
		event.UserID != "alice" ||
		event.LastReadSeq != message.Seq {
		t.Fatalf("read event = %+v", event)
	}
}

func waitForEvent(t *testing.T, events <-chan Event, kind EventKind) Event {
	t.Helper()

	timeout := time.After(5 * time.Second)

	for {
		select {
		case event := <-events:
			if event.Kind == kind {
				return event
			}

		case <-timeout:
			t.Fatalf("timed out waiting for %s", kind)
		}
	}
}

func TestServiceTypingAuthorizationAndPublish(t *testing.T) {
	repository, pool, ctx := integrationChatRepository(t)

	const tenant = "tenant-a"

	addTenantMembers(t, pool, ctx, tenant, "alice", "bob", "eve")

	recorder := &recordingPublisher{}

	service := NewService(repository, nil, nil, recorder, nil, 0)

	alice := auth.Identity{TenantID: tenant, UserID: "alice"}
	eve := auth.Identity{TenantID: tenant, UserID: "eve"}

	detail, err := service.CreateGroup(ctx, CreateGroupInput{
		Identity:  alice,
		Title:     "typing",
		MemberIDs: []string{"bob"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := service.SetTyping(ctx, TypingInput{
		Identity:       alice,
		ConversationID: detail.ID,
		Typing:         true,
	}); err != nil {
		t.Fatal(err)
	}

	if len(recorder.events) != 1 {
		t.Fatalf("published %d events, want 1", len(recorder.events))
	}

	event := recorder.events[0]

	if event.Kind != EventTyping ||
		event.TenantID != tenant ||
		event.ConversationID != detail.ID ||
		event.ActorUserID != "alice" ||
		!event.Typing {
		t.Fatalf("typing event = %+v", event)
	}

	// A workspace member outside the conversation cannot emit indicators.
	if err := service.SetTyping(ctx, TypingInput{
		Identity:       eve,
		ConversationID: detail.ID,
		Typing:         true,
	}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("non-member typing error = %v, want ErrForbidden", err)
	}

	if len(recorder.events) != 1 {
		t.Fatalf("non-member typing was published: %+v", recorder.events)
	}
}

func TestHubTypingAndPresence(t *testing.T) {
	repository, pool, ctx := integrationChatRepository(t)

	const tenant = "tenant-a"

	addTenantMembers(t, pool, ctx, tenant, "alice", "bob")

	detail, err := repository.CreateGroupConversation(
		ctx,
		CreateGroupConversationParams{
			ID:              uuid.NewString(),
			TenantID:        tenant,
			Title:           "presence",
			CreatedByUserID: "alice",
			MemberIDs:       []string{"bob"},
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	hub := NewHub(repository)

	bobClient := &wsClient{
		tenantID: tenant,
		userID:   "bob",
		send:     make(chan []byte, 8),
	}

	aliceClient := &wsClient{
		tenantID: tenant,
		userID:   "alice",
		send:     make(chan []byte, 8),
	}

	hub.Register(ctx, bobClient)
	t.Cleanup(func() {
		hub.Unregister(ctx, bobClient)
	})

	// Bob was already online, so Alice's first connection produces his
	// presence.online frame.
	hub.Register(ctx, aliceClient)

	online := readClientEvent(t, bobClient.send)
	if online.Type != string(EventPresenceOnline) || online.UserID != "alice" {
		t.Fatalf("online event = %+v", online)
	}

	hub.HandleEvent(ctx, Event{
		Kind:           EventTyping,
		TenantID:       tenant,
		ConversationID: detail.ID,
		ActorUserID:    "alice",
		Typing:         true,
	})

	started := readClientEvent(t, bobClient.send)
	if started.Type != string(EventTyping) ||
		started.UserID != "alice" ||
		started.Typing == nil ||
		!*started.Typing {
		t.Fatalf("typing start = %+v", started)
	}

	hub.HandleEvent(ctx, Event{
		Kind:           EventTyping,
		TenantID:       tenant,
		ConversationID: detail.ID,
		ActorUserID:    "alice",
		Typing:         false,
	})

	stopped := readClientEvent(t, bobClient.send)
	if stopped.Type != string(EventTyping) ||
		stopped.Typing == nil ||
		*stopped.Typing {
		t.Fatalf("typing stop = %+v", stopped)
	}

	// The typist never receives their own indicator.
	assertNoFrame(t, aliceClient.send)

	hub.Unregister(ctx, aliceClient)

	offline := readClientEvent(t, bobClient.send)
	if offline.Type != string(EventPresenceOffline) || offline.UserID != "alice" {
		t.Fatalf("offline event = %+v", offline)
	}
}

type recordingPublisher struct {
	events []Event
}

func (p *recordingPublisher) Publish(
	_ context.Context,
	event Event,
) error {
	p.events = append(p.events, event)

	return nil
}

func readClientEvent(t *testing.T, frames <-chan []byte) ClientEvent {
	t.Helper()

	select {
	case payload := <-frames:
		var event ClientEvent

		if err := json.Unmarshal(payload, &event); err != nil {
			t.Fatal(err)
		}

		return event

	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for a client event")
	}

	return ClientEvent{}
}

func assertNoFrame(t *testing.T, frames <-chan []byte) {
	t.Helper()

	select {
	case payload := <-frames:
		t.Fatalf("unexpected frame: %s", payload)
	default:
	}
}
