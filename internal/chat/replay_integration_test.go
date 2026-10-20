package chat

import (
	"errors"
	"fmt"
	"testing"

	"github.com/mhdph/taskmanager/internal/auth"
)

func TestServiceListMessagesAfterReplay(t *testing.T) {
	service, pool, ctx := integrationChatService(t)

	const tenant = "tenant-a"

	addTenantMembers(t, pool, ctx, tenant, "alice", "bob")

	alice := auth.Identity{TenantID: tenant, UserID: "alice"}

	detail, err := service.CreateGroup(ctx, CreateGroupInput{
		Identity:  alice,
		Title:     "replay",
		MemberIDs: []string{"bob"},
	})
	if err != nil {
		t.Fatal(err)
	}

	messages := make([]Message, 0, 5)

	for i := 1; i <= 5; i++ {
		message, _, err := service.SendMessage(ctx, SendMessageInput{
			Identity:       alice,
			ConversationID: detail.ID,
			Body:           fmt.Sprintf("message %d", i),
		})
		if err != nil {
			t.Fatal(err)
		}

		messages = append(messages, message)
	}

	page, err := service.ListMessages(ctx, ListMessagesInput{
		Identity:       alice,
		ConversationID: detail.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 5 || page.NextAfter != nil || page.NextCursor != nil {
		t.Fatalf("default page = %+v", page)
	}
	for i := 1; i < len(page.Items); i++ {
		if page.Items[i-1].Seq >= page.Items[i].Seq {
			t.Fatalf("default page is not ascending: %+v", page.Items)
		}
	}

	// Replay everything after the second message.
	page, err = service.ListMessages(ctx, ListMessagesInput{
		Identity:       alice,
		ConversationID: detail.ID,
		After:          fmt.Sprint(messages[1].Seq),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 3 ||
		page.Items[0].ID != messages[2].ID ||
		page.Items[2].ID != messages[4].ID {
		t.Fatalf("replay items = %+v", page.Items)
	}
	if page.NextAfter != nil || page.NextCursor != nil {
		t.Fatalf("complete replay cursors = %+v", page)
	}

	// Forward pagination uses nextAfter, not nextCursor, and never reverses
	// the page.
	page, err = service.ListMessages(ctx, ListMessagesInput{
		Identity:       alice,
		ConversationID: detail.ID,
		After:          fmt.Sprint(messages[1].Seq),
		Limit:          2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 ||
		page.Items[0].ID != messages[2].ID ||
		page.Items[1].ID != messages[3].ID {
		t.Fatalf("first replay page = %+v", page.Items)
	}
	if page.NextAfter == nil || *page.NextAfter != messages[3].Seq {
		t.Fatalf("nextAfter = %v, want %d", page.NextAfter, messages[3].Seq)
	}
	if page.NextCursor != nil {
		t.Fatalf("nextCursor = %v on replay page", page.NextCursor)
	}

	page, err = service.ListMessages(ctx, ListMessagesInput{
		Identity:       alice,
		ConversationID: detail.ID,
		After:          fmt.Sprint(*page.NextAfter),
		Limit:          2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != messages[4].ID {
		t.Fatalf("second replay page = %+v", page.Items)
	}
	if page.NextAfter != nil {
		t.Fatalf("nextAfter = %v at tail", page.NextAfter)
	}

	// A page that exactly fills the limit must not claim another page.
	page, err = service.ListMessages(ctx, ListMessagesInput{
		Identity:       alice,
		ConversationID: detail.ID,
		After:          fmt.Sprint(messages[1].Seq),
		Limit:          3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 3 || page.NextAfter != nil {
		t.Fatalf("exact replay page = %+v", page)
	}

	// Replay from the beginning and from the current tail.
	page, err = service.ListMessages(ctx, ListMessagesInput{
		Identity:       alice,
		ConversationID: detail.ID,
		After:          "0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 5 || page.Items[0].ID != messages[0].ID {
		t.Fatalf("replay from zero = %+v", page.Items)
	}

	page, err = service.ListMessages(ctx, ListMessagesInput{
		Identity:       alice,
		ConversationID: detail.ID,
		After:          fmt.Sprint(messages[4].Seq),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 0 || page.NextAfter != nil {
		t.Fatalf("replay at tail = %+v", page)
	}

	// Invalid or conflicting pagination is rejected.
	for _, input := range []ListMessagesInput{
		{
			Identity:       alice,
			ConversationID: detail.ID,
			After:          "not-a-number",
		},
		{
			Identity:       alice,
			ConversationID: detail.ID,
			After:          "-1",
		},
		{
			Identity:       alice,
			ConversationID: detail.ID,
			Before:         EncodeMessageCursor(messages[3].Seq),
			After:          fmt.Sprint(messages[1].Seq),
		},
	} {
		if _, err := service.ListMessages(
			ctx,
			input,
		); !errors.Is(err, ErrInvalidCursor) {
			t.Fatalf("input %+v error = %v, want ErrInvalidCursor", input, err)
		}
	}

	// The older-history pagination still works exactly as before.
	page, err = service.ListMessages(ctx, ListMessagesInput{
		Identity:       alice,
		ConversationID: detail.ID,
		Before:         EncodeMessageCursor(messages[4].Seq),
		Limit:          2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 ||
		page.Items[0].ID != messages[2].ID ||
		page.Items[1].ID != messages[3].ID {
		t.Fatalf("history page = %+v", page.Items)
	}
	if page.NextCursor == nil {
		t.Fatal("history page has no nextCursor")
	}
	if page.NextAfter != nil {
		t.Fatalf("nextAfter = %v on history page", page.NextAfter)
	}
}
