package chat

import (
	"errors"
	"testing"

	"github.com/mhdph/taskmanager/internal/auth"
)

func TestServiceNotificationPreferences(t *testing.T) {
	service, pool, ctx := integrationChatService(t)

	const tenant = "tenant-a"

	addTenantMembers(t, pool, ctx, tenant, "alice", "bob", "eve")

	alice := auth.Identity{TenantID: tenant, UserID: "alice"}
	bob := auth.Identity{TenantID: tenant, UserID: "bob"}
	eve := auth.Identity{TenantID: tenant, UserID: "eve"}

	detail, err := service.CreateGroup(ctx, CreateGroupInput{
		Identity:  alice,
		Title:     "preferences",
		MemberIDs: []string{"bob"},
	})
	if err != nil {
		t.Fatal(err)
	}

	// The default mode notifies for every message.
	mode, err := service.GetNotificationPreference(
		ctx,
		bob,
		detail.ID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if mode != NotificationModeAll {
		t.Fatalf("default mode = %q, want %q", mode, NotificationModeAll)
	}

	// A member can store a mode, and it is scoped to that conversation.
	mode, err = service.SetNotificationPreference(
		ctx,
		bob,
		detail.ID,
		NotificationModeMute,
	)
	if err != nil || mode != NotificationModeMute {
		t.Fatalf("set mode: mode=%q err=%v", mode, err)
	}

	mode, err = service.GetNotificationPreference(ctx, bob, detail.ID)
	if err != nil || mode != NotificationModeMute {
		t.Fatalf("read mode: mode=%q err=%v", mode, err)
	}

	other, err := service.CreateGroup(ctx, CreateGroupInput{
		Identity:  alice,
		Title:     "other preferences",
		MemberIDs: []string{"bob"},
	})
	if err != nil {
		t.Fatal(err)
	}

	mode, err = service.GetNotificationPreference(ctx, bob, other.ID)
	if err != nil || mode != NotificationModeAll {
		t.Fatalf("mode in other conversation = %q, want %q", mode, NotificationModeAll)
	}

	// Non-members cannot read or change preferences.
	if _, err := service.GetNotificationPreference(
		ctx,
		eve,
		detail.ID,
	); !errors.Is(err, ErrForbidden) {
		t.Fatalf("non-member read error = %v, want ErrForbidden", err)
	}

	if _, err := service.SetNotificationPreference(
		ctx,
		eve,
		detail.ID,
		NotificationModeMute,
	); !errors.Is(err, ErrForbidden) {
		t.Fatalf("non-member write error = %v, want ErrForbidden", err)
	}

	// An unknown mode is rejected.
	if _, err := service.SetNotificationPreference(
		ctx,
		bob,
		detail.ID,
		NotificationMode("shout"),
	); !errors.Is(err, ErrInvalidNotificationMode) {
		t.Fatalf("invalid mode error = %v, want ErrInvalidNotificationMode", err)
	}
}

func TestServiceChatNotificationsHonourPreferencesAndMentions(t *testing.T) {
	repository, pool, ctx := integrationChatRepository(t)

	const tenant = "tenant-a"

	addTenantMembers(t, pool, ctx, tenant, "alice", "bob", "carol")

	sender := &recordingNotificationSender{}

	service := NewService(
		repository,
		nil,
		nil,
		nil,
		nil,
		0,
		repository,
		sender,
	)

	alice := auth.Identity{TenantID: tenant, UserID: "alice"}
	bob := auth.Identity{TenantID: tenant, UserID: "bob"}
	carol := auth.Identity{TenantID: tenant, UserID: "carol"}

	detail, err := service.CreateGroup(ctx, CreateGroupInput{
		Identity:  alice,
		Title:     "mention routing",
		MemberIDs: []string{"bob", "carol"},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Mentions are resolved when the message is written and stored with it.
	message, _, err := service.SendMessage(ctx, SendMessageInput{
		Identity:       alice,
		ConversationID: detail.ID,
		Body:           "carol, can you look? @carol",
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(message.Mentions) != 1 || message.Mentions[0] != "carol" {
		t.Fatalf("message mentions = %v, want [carol]", message.Mentions)
	}

	wire := NewWireMessage(message)
	if len(wire.MentionedUserIDs) != 1 ||
		wire.MentionedUserIDs[0] != "carol" {
		t.Fatalf("wire mentions = %v, want [carol]", wire.MentionedUserIDs)
	}

	page, err := service.ListMessages(ctx, ListMessagesInput{
		Identity:       alice,
		ConversationID: detail.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 ||
		len(page.Items[0].Mentions) != 1 ||
		page.Items[0].Mentions[0] != "carol" {
		t.Fatalf("stored mentions = %+v, want [carol]", page.Items)
	}

	// Editing the body resolves the mentions again.
	edited, err := service.EditMessage(ctx, EditMessageInput{
		Identity:       alice,
		ConversationID: detail.ID,
		MessageID:      message.ID,
		Body:           "bob owns this now @bob",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(edited.Mentions) != 1 || edited.Mentions[0] != "bob" {
		t.Fatalf("edited mentions = %v, want [bob]", edited.Mentions)
	}

	// Bob mutes the conversation: no notifications for him at all.
	if _, err := service.SetNotificationPreference(
		ctx,
		bob,
		detail.ID,
		NotificationModeMute,
	); err != nil {
		t.Fatal(err)
	}

	// Carol only wants mentions.
	if _, err := service.SetNotificationPreference(
		ctx,
		carol,
		detail.ID,
		NotificationModeMentions,
	); err != nil {
		t.Fatal(err)
	}

	before := len(sender.snapshot())

	if _, _, err := service.SendMessage(ctx, SendMessageInput{
		Identity:       alice,
		ConversationID: detail.ID,
		Body:           "plain update for everyone",
	}); err != nil {
		t.Fatal(err)
	}

	notifications := sender.snapshot()[before:]
	if len(notifications) != 0 {
		t.Fatalf("muted and mention-only members were notified: %+v", notifications)
	}

	// Mentioning carol reaches her again.
	before = len(sender.snapshot())

	if _, _, err := service.SendMessage(ctx, SendMessageInput{
		Identity:       alice,
		ConversationID: detail.ID,
		Body:           "ping @carol",
	}); err != nil {
		t.Fatal(err)
	}

	notifications = sender.snapshot()[before:]
	if len(notifications) != 1 ||
		notifications[0].RecipientUserID != "carol" {
		t.Fatalf("mention notifications = %+v, want only carol", notifications)
	}

	// The author is never notified, even when mentioning themselves.
	before = len(sender.snapshot())

	if _, _, err := service.SendMessage(ctx, SendMessageInput{
		Identity:       alice,
		ConversationID: detail.ID,
		Body:           "note to self @alice",
	}); err != nil {
		t.Fatal(err)
	}

	if got := len(sender.snapshot()) - before; got != 0 {
		t.Fatalf("author received %d notifications", got)
	}
}
