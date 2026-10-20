package chat

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	db "github.com/mhdph/taskmanager/internal/database/generated"
)

func integrationChatRepository(t *testing.T) (*Repository, *pgxpool.Pool, context.Context) {
	t.Helper()

	url := os.Getenv("TASKMANAGER_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TASKMANAGER_TEST_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	t.Cleanup(cancel)

	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}

	schema := "test_" + uuid.NewString()[0:8]

	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)

	if _, err := admin.Exec(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), `DROP SCHEMA `+schema+` CASCADE`)
	})

	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	files, err := filepath.Glob("../../db/migrations/*.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)

	for _, file := range files {
		sql, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, string(sql)); err != nil {
			t.Fatalf("apply %s: %v", file, err)
		}
	}

	return NewRepository(pool, db.New(pool)), pool, ctx
}

func addTenantMembers(t *testing.T, pool *pgxpool.Pool, ctx context.Context, tenantID string, userIDs ...string) {
	t.Helper()

	for _, userID := range userIDs {
		if _, err := pool.Exec(
			ctx,
			`INSERT INTO tenant_members (tenant_id, user_id) VALUES ($1, $2)`,
			tenantID,
			userID,
		); err != nil {
			t.Fatal(err)
		}
	}
}

func insertProject(t *testing.T, pool *pgxpool.Pool, ctx context.Context, tenantID string, projectID string, members map[string]string) {
	t.Helper()

	if _, err := pool.Exec(
		ctx,
		`INSERT INTO projects (id, tenant_id, name, created_by_user_id)
		 VALUES ($1, $2, 'chat project', 'creator')`,
		projectID,
		tenantID,
	); err != nil {
		t.Fatal(err)
	}

	for userID, role := range members {
		if _, err := pool.Exec(
			ctx,
			`INSERT INTO project_members (project_id, tenant_id, user_id, role)
			 VALUES ($1, $2, $3, $4)`,
			projectID,
			tenantID,
			userID,
			role,
		); err != nil {
			t.Fatal(err)
		}
	}
}

func TestChatMessageOrderingIdempotencyAndUnread(t *testing.T) {
	repository, pool, ctx := integrationChatRepository(t)

	const tenant = "tenant-a"
	const alice = "alice"
	const bob = "bob"

	addTenantMembers(t, pool, ctx, tenant, alice, bob)

	conversation, err := repository.CreateGroupConversation(ctx, CreateGroupConversationParams{
		ID:              uuid.NewString(),
		TenantID:        tenant,
		Title:           "decisions",
		CreatedByUserID: alice,
		MemberIDs:       []string{bob},
	})
	if err != nil {
		t.Fatal(err)
	}

	firstClientID := uuid.NewString()
	first, created, err := repository.CreateMessage(ctx, CreateMessageParams{
		ID:              uuid.NewString(),
		TenantID:        tenant,
		ConversationID:  conversation.ID,
		Kind:            MessageKindUser,
		AuthorUserID:    stringPointer(alice),
		Body:            "first",
		ClientMessageID: &firstClientID,
	})
	if err != nil || !created {
		t.Fatalf("create first message: created=%v err=%v", created, err)
	}

	// Retrying with the same client message id returns the original row.
	duplicate, created, err := repository.CreateMessage(ctx, CreateMessageParams{
		ID:              uuid.NewString(),
		TenantID:        tenant,
		ConversationID:  conversation.ID,
		Kind:            MessageKindUser,
		AuthorUserID:    stringPointer(alice),
		Body:            "first (retry)",
		ClientMessageID: &firstClientID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Fatal("duplicate client message id created a second row")
	}
	if duplicate.ID != first.ID || duplicate.Seq != first.Seq || duplicate.Body != "first" {
		t.Fatalf("duplicate = %+v, want original %s/%d", duplicate, first.ID, first.Seq)
	}

	second, created, err := repository.CreateMessage(ctx, CreateMessageParams{
		ID:             uuid.NewString(),
		TenantID:       tenant,
		ConversationID: conversation.ID,
		Kind:           MessageKindUser,
		AuthorUserID:   stringPointer(bob),
		Body:           "second",
	})
	if err != nil || !created {
		t.Fatalf("create second message: created=%v err=%v", created, err)
	}
	if second.Seq <= first.Seq {
		t.Fatalf("sequence did not advance: first=%d second=%d", first.Seq, second.Seq)
	}

	// Alice authored the first message, so only Bob's message is unread.
	summaries, err := repository.ListForUser(ctx, tenant, alice, false, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 {
		t.Fatalf("alice conversations = %d, want 1", len(summaries))
	}
	if summaries[0].UnreadCount != 1 {
		t.Fatalf("alice unread = %d, want 1", summaries[0].UnreadCount)
	}
	if summaries[0].LastMessage == nil || summaries[0].LastMessage.Body != "second" {
		t.Fatalf("conversation tail = %+v, want second message", summaries[0].LastMessage)
	}
	if len(summaries[0].MemberIDs) != 2 {
		t.Fatalf("member ids = %v, want alice and bob", summaries[0].MemberIDs)
	}

	updated, err := repository.UpdateReadCursor(ctx, tenant, conversation.ID, alice, second.Seq)
	if err != nil || !updated {
		t.Fatalf("update read cursor: updated=%v err=%v", updated, err)
	}

	summaries, err = repository.ListForUser(ctx, tenant, alice, false, 20)
	if err != nil {
		t.Fatal(err)
	}
	if summaries[0].UnreadCount != 0 {
		t.Fatalf("alice unread after read = %d, want 0", summaries[0].UnreadCount)
	}

	// A read cursor never moves backwards.
	if _, err := repository.UpdateReadCursor(ctx, tenant, conversation.ID, alice, first.Seq); err != nil {
		t.Fatal(err)
	}

	member, err := repository.GetMember(ctx, tenant, conversation.ID, alice)
	if err != nil {
		t.Fatal(err)
	}
	if member.LastReadSeq != second.Seq {
		t.Fatalf("read cursor = %d, want %d", member.LastReadSeq, second.Seq)
	}

	// Soft delete keeps the stream position but drops the content.
	deleted, err := repository.DeleteMessage(ctx, tenant, conversation.ID, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if deleted.Body != "" || deleted.DeletedAt == nil {
		t.Fatalf("deleted message = %+v, want empty body and tombstone", deleted)
	}

	_, err = repository.DeleteMessage(ctx, tenant, conversation.ID, second.ID)
	if !errors.Is(err, ErrMessageNotFound) {
		t.Fatalf("second delete error = %v, want ErrMessageNotFound", err)
	}
}

func TestChatMessagePagination(t *testing.T) {
	repository, pool, ctx := integrationChatRepository(t)

	const tenant = "tenant-a"
	const alice = "alice"

	addTenantMembers(t, pool, ctx, tenant, alice)

	conversation, err := repository.CreateGroupConversation(ctx, CreateGroupConversationParams{
		ID:              uuid.NewString(),
		TenantID:        tenant,
		Title:           "paging",
		CreatedByUserID: alice,
	})
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 5; i++ {
		if _, _, err := repository.CreateMessage(ctx, CreateMessageParams{
			ID:             uuid.NewString(),
			TenantID:       tenant,
			ConversationID: conversation.ID,
			Kind:           MessageKindUser,
			AuthorUserID:   stringPointer(alice),
			Body:           "message",
		}); err != nil {
			t.Fatal(err)
		}
	}

	firstPage, err := repository.ListMessages(ctx, tenant, conversation.ID, nil, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(firstPage) != 2 || firstPage[0].Seq != 5 || firstPage[1].Seq != 4 {
		t.Fatalf("first page seqs = %v, want [5 4]", seqs(firstPage))
	}

	before := firstPage[1].Seq
	secondPage, err := repository.ListMessages(ctx, tenant, conversation.ID, &before, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(secondPage) != 2 || secondPage[0].Seq != 3 || secondPage[1].Seq != 2 {
		t.Fatalf("second page seqs = %v, want [3 2]", seqs(secondPage))
	}
}

func TestDMConversationIsUniquePerPair(t *testing.T) {
	repository, pool, ctx := integrationChatRepository(t)

	const tenant = "tenant-a"
	const alice = "alice"
	const bob = "bob"

	addTenantMembers(t, pool, ctx, tenant, alice, bob)

	first, err := repository.CreateDMConversation(ctx, tenant, alice, bob, alice, uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}

	second, err := repository.CreateDMConversation(ctx, tenant, bob, alice, bob, uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}

	if first.ID != second.ID {
		t.Fatalf("dm ids differ: %s != %s", first.ID, second.ID)
	}
	if first.Type != ConversationTypeDM {
		t.Fatalf("type = %s, want dm", first.Type)
	}

	members, err := repository.ListMembers(ctx, tenant, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 2 || members[0].UserID != alice || members[1].UserID != bob {
		t.Fatalf("dm members = %+v, want [alice bob]", members)
	}

	// Both participants see the same conversation.
	for _, userID := range []string{alice, bob} {
		summaries, err := repository.ListForUser(ctx, tenant, userID, false, 20)
		if err != nil {
			t.Fatal(err)
		}
		if len(summaries) != 1 || summaries[0].ID != first.ID {
			t.Fatalf("%s conversations = %+v, want the dm", userID, summaries)
		}
	}

	// A workspace without the other member cannot resolve the pair through
	// the repository; the service validates membership before calling it.
	existing, err := repository.ExistingTenantMembers(ctx, tenant, []string{alice, bob, "carol"})
	if err != nil {
		t.Fatal(err)
	}
	if len(existing) != 2 {
		t.Fatalf("existing members = %v, want alice and bob", existing)
	}
}

func TestProjectConversationSyncAndPrune(t *testing.T) {
	repository, pool, ctx := integrationChatRepository(t)

	const tenant = "tenant-a"
	const manager = "manager"
	const member = "member"
	const admin = "admin"

	projectID := uuid.NewString()
	insertProject(t, pool, ctx, tenant, projectID, map[string]string{
		manager: "manager",
		member:  "member",
	})

	conversation, err := repository.CreateProjectConversation(ctx, CreateProjectConversationParams{
		ID:              uuid.NewString(),
		TenantID:        tenant,
		ProjectID:       projectID,
		CreatedByUserID: manager,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Starting the conversation twice returns the same row.
	again, err := repository.CreateProjectConversation(ctx, CreateProjectConversationParams{
		ID:              uuid.NewString(),
		TenantID:        tenant,
		ProjectID:       projectID,
		CreatedByUserID: manager,
	})
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != conversation.ID {
		t.Fatalf("project conversation id changed: %s != %s", again.ID, conversation.ID)
	}

	if err := repository.SyncProjectMembers(ctx, tenant, conversation.ID, projectID); err != nil {
		t.Fatal(err)
	}

	members, err := repository.ListMembers(ctx, tenant, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 2 {
		t.Fatalf("synced members = %+v, want manager and member", members)
	}
	if members[0].UserID != manager || members[0].Role != MemberRoleOwner || members[0].Source != MemberSourceProject {
		t.Fatalf("manager membership = %+v", members[0])
	}

	// An admin who opened the project chat stays even though they are not
	// a project member.
	if err := repository.AddMember(ctx, tenant, conversation.ID, admin, MemberRoleMember); err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(
		ctx,
		`DELETE FROM project_members WHERE tenant_id = $1 AND project_id = $2 AND user_id = $3`,
		tenant,
		projectID,
		member,
	); err != nil {
		t.Fatal(err)
	}

	if err := repository.SyncProjectMembers(ctx, tenant, conversation.ID, projectID); err != nil {
		t.Fatal(err)
	}

	members, err = repository.ListMembers(ctx, tenant, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}

	byUser := make(map[string]Member, len(members))
	for _, item := range members {
		byUser[item.UserID] = item
	}

	if _, ok := byUser[member]; ok {
		t.Fatal("removed project member was not pruned from the conversation")
	}
	if _, ok := byUser[admin]; !ok {
		t.Fatal("manually added admin was pruned from the conversation")
	}
}

func stringPointer(value string) *string {
	return &value
}

func seqs(messages []Message) []int64 {
	result := make([]int64, 0, len(messages))
	for _, message := range messages {
		result = append(result, message.Seq)
	}
	return result
}
