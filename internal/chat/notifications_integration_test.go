package chat

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/mhdph/taskmanager/internal/auth"
	database "github.com/mhdph/taskmanager/internal/database/generated"
	"github.com/mhdph/taskmanager/internal/notification"
	"github.com/mhdph/taskmanager/internal/project"
)

type recordingNotificationSender struct {
	mu sync.Mutex

	inputs []notification.SendInput

	err error
}

func (r *recordingNotificationSender) Send(
	_ context.Context,
	input notification.SendInput,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.inputs = append(r.inputs, input)

	return r.err
}

func (r *recordingNotificationSender) snapshot() []notification.SendInput {
	r.mu.Lock()
	defer r.mu.Unlock()

	items := make([]notification.SendInput, len(r.inputs))
	copy(items, r.inputs)

	return items
}

type fakePresence struct {
	mu sync.Mutex

	online map[string]bool
}

func newFakePresence(userIDs ...string) *fakePresence {
	presence := &fakePresence{online: make(map[string]bool)}

	for _, userID := range userIDs {
		presence.set(userID, true)
	}

	return presence
}

func (p *fakePresence) set(userID string, online bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.online[userID] = online
}

func (p *fakePresence) IsOnline(_ string, userID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.online[userID]
}

func TestServiceChatNotificationsTargetOfflineMembers(t *testing.T) {
	repository, pool, ctx := integrationChatRepository(t)

	const tenant = "tenant-a"

	addTenantMembers(t, pool, ctx, tenant, "alice", "bob", "carol")

	sender := &recordingNotificationSender{}
	presence := newFakePresence("bob")

	service := NewService(
		repository,
		nil,
		nil,
		nil,
		nil,
		0,
		presence,
		sender,
	)

	alice := auth.Identity{TenantID: tenant, UserID: "alice"}

	detail, err := service.CreateGroup(ctx, CreateGroupInput{
		Identity:  alice,
		Title:     "notification group",
		MemberIDs: []string{"bob", "carol"},
	})
	if err != nil {
		t.Fatal(err)
	}

	clientMessageID := "client-message-1"

	message, created, err := service.SendMessage(ctx, SendMessageInput{
		Identity:        alice,
		ConversationID:  detail.ID,
		Body:            "hello\n\n  team",
		ClientMessageID: &clientMessageID,
	})
	if err != nil || !created {
		t.Fatalf("send message: created=%v err=%v", created, err)
	}

	notifications := sender.snapshot()
	if len(notifications) != 1 {
		t.Fatalf("notifications = %+v, want only the offline member", notifications)
	}

	item := notifications[0]
	if item.TenantID != tenant || item.RecipientUserID != "carol" {
		t.Fatalf("notification target = %+v", item)
	}
	if item.Type != notification.EventChatMessage {
		t.Fatalf("notification type = %q", item.Type)
	}
	if item.ActorUserID == nil || *item.ActorUserID != "alice" {
		t.Fatalf("notification actor = %v", item.ActorUserID)
	}
	if item.ConversationID == nil || *item.ConversationID != detail.ID {
		t.Fatalf("notification conversation = %v", item.ConversationID)
	}
	if item.MessageID == nil || *item.MessageID != message.ID {
		t.Fatalf("notification message = %v", item.MessageID)
	}
	if item.Title != "New message in notification group" {
		t.Fatalf("notification title = %q", item.Title)
	}
	if item.Body == nil || *item.Body != "hello team" {
		t.Fatalf("notification body = %v", item.Body)
	}
	if item.Data["conversationType"] != "group" ||
		item.Data["messageId"] != message.ID ||
		item.Data["attachmentCount"] != 0 {
		t.Fatalf("notification data = %+v", item.Data)
	}

	// Retrying an idempotent send must not notify again.
	if _, retried, err := service.SendMessage(ctx, SendMessageInput{
		Identity:        alice,
		ConversationID:  detail.ID,
		Body:            "hello\n\n  team",
		ClientMessageID: &clientMessageID,
	}); err != nil || retried {
		t.Fatalf("idempotent retry: created=%v err=%v", retried, err)
	}

	if got := len(sender.snapshot()); got != 1 {
		t.Fatalf("notifications after retry = %d, want 1", got)
	}

	// When the other member goes offline too, both recipients are notified
	// and the author never is.
	presence.set("bob", false)

	if _, _, err := service.SendMessage(ctx, SendMessageInput{
		Identity:       alice,
		ConversationID: detail.ID,
		Body:           "second message",
	}); err != nil {
		t.Fatal(err)
	}

	notifications = sender.snapshot()
	if len(notifications) != 3 {
		t.Fatalf("notifications = %d, want 3", len(notifications))
	}

	recipients := map[string]bool{}
	for _, notification := range notifications {
		recipients[notification.RecipientUserID] = true
	}

	if recipients["alice"] {
		t.Fatal("author received their own chat notification")
	}
	if !recipients["bob"] || !recipients["carol"] {
		t.Fatalf("recipients = %v, want bob and carol", recipients)
	}

	// Attachment-only messages get a readable preview.
	attachment, err := repository.CreateAttachment(ctx, CreateAttachmentParams{
		ID:               uuid.NewString(),
		TenantID:         tenant,
		ConversationID:   detail.ID,
		UploadedByUserID: "alice",
		Filename:         "report.pdf",
		ContentType:      "application/pdf",
		SizeBytes:        10,
		ObjectKey:        "tests/" + uuid.NewString(),
	})
	if err != nil {
		t.Fatal(err)
	}

	before := len(sender.snapshot())

	attachmentMessage, _, err := service.SendMessage(ctx, SendMessageInput{
		Identity:       alice,
		ConversationID: detail.ID,
		AttachmentIDs:  []string{attachment.ID},
	})
	if err != nil {
		t.Fatal(err)
	}

	notifications = sender.snapshot()[before:]
	if len(notifications) != 2 {
		t.Fatalf("attachment notifications = %d, want 2", len(notifications))
	}

	for _, item := range notifications {
		if item.Body == nil || *item.Body != "Sent an attachment" {
			t.Fatalf("attachment notification body = %v", item.Body)
		}

		if item.Data["attachmentCount"] != 1 {
			t.Fatalf("attachment notification data = %+v", item.Data)
		}
	}

	if notifications[0].MessageID == nil ||
		*notifications[0].MessageID != attachmentMessage.ID {
		t.Fatalf("attachment notification message = %v", notifications[0].MessageID)
	}

	// Direct messages get their own title.
	dm, err := service.CreateDM(ctx, CreateDMInput{
		Identity:    alice,
		OtherUserID: "bob",
	})
	if err != nil {
		t.Fatal(err)
	}

	presence.set("bob", false)

	before = len(sender.snapshot())

	if _, _, err := service.SendMessage(ctx, SendMessageInput{
		Identity:       alice,
		ConversationID: dm.ID,
		Body:           "direct hello",
	}); err != nil {
		t.Fatal(err)
	}

	notifications = sender.snapshot()[before:]
	if len(notifications) != 1 || notifications[0].RecipientUserID != "bob" {
		t.Fatalf("dm notifications = %+v", notifications)
	}
	if notifications[0].Title != "New direct message" {
		t.Fatalf("dm title = %q", notifications[0].Title)
	}
}

func TestServiceChatNotificationFailureDoesNotFailSend(t *testing.T) {
	repository, pool, ctx := integrationChatRepository(t)

	const tenant = "tenant-a"

	addTenantMembers(t, pool, ctx, tenant, "alice", "bob")

	sender := &recordingNotificationSender{
		err: notification.ErrQueueFull,
	}

	service := NewService(
		repository,
		nil,
		nil,
		nil,
		nil,
		0,
		nil,
		sender,
	)

	alice := auth.Identity{TenantID: tenant, UserID: "alice"}

	detail, err := service.CreateGroup(ctx, CreateGroupInput{
		Identity:  alice,
		Title:     "best effort",
		MemberIDs: []string{"bob"},
	})
	if err != nil {
		t.Fatal(err)
	}

	message, created, err := service.SendMessage(ctx, SendMessageInput{
		Identity:       alice,
		ConversationID: detail.ID,
		Body:           "still delivered",
	})
	if err != nil || !created || message.ID == "" {
		t.Fatalf(
			"send with failing notification: created=%v err=%v message=%+v",
			created,
			err,
			message,
		)
	}

	if got := len(sender.snapshot()); got != 1 {
		t.Fatalf("notification attempts = %d, want 1 before queue-full stop", got)
	}

	page, err := service.ListMessages(ctx, ListMessagesInput{
		Identity:       alice,
		ConversationID: detail.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 ||
		!strings.Contains(page.Items[0].Body, "still delivered") {
		t.Fatalf("history = %+v", page.Items)
	}
}

func TestServiceChatNotificationProjectTitle(t *testing.T) {
	repository, pool, ctx := integrationChatRepository(t)

	const tenant = "tenant-a"

	addTenantMembers(t, pool, ctx, tenant, "alice", "bob")

	projectID := uuid.NewString()

	insertProject(t, pool, ctx, tenant, projectID, map[string]string{
		"alice": "manager",
		"bob":   "member",
	})

	projectRepository := project.NewRepository(
		pool,
		database.New(pool),
	)

	sender := &recordingNotificationSender{}

	service := NewService(
		repository,
		projectRepository,
		project.NewAuthorizer(projectRepository),
		nil,
		nil,
		0,
		nil,
		sender,
	)

	alice := auth.Identity{TenantID: tenant, UserID: "alice"}

	detail, err := service.ProjectChat(ctx, alice, projectID)
	if err != nil {
		t.Fatal(err)
	}

	if _, _, err := service.SendMessage(ctx, SendMessageInput{
		Identity:       alice,
		ConversationID: detail.ID,
		Body:           "project hello",
	}); err != nil {
		t.Fatal(err)
	}

	notifications := sender.snapshot()
	if len(notifications) != 1 {
		t.Fatalf("notifications = %+v, want one for bob", notifications)
	}

	item := notifications[0]
	if item.RecipientUserID != "bob" {
		t.Fatalf("recipient = %q", item.RecipientUserID)
	}
	if item.Title != "New message in chat project" {
		t.Fatalf("title = %q", item.Title)
	}
	if item.ProjectID == nil || *item.ProjectID != projectID {
		t.Fatalf("project id = %v", item.ProjectID)
	}
	if item.Data["conversationType"] != "project" {
		t.Fatalf("data = %+v", item.Data)
	}
}
