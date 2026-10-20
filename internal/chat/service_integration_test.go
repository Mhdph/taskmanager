package chat

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mhdph/taskmanager/internal/auth"
	db "github.com/mhdph/taskmanager/internal/database/generated"
	"github.com/mhdph/taskmanager/internal/project"
)

func integrationChatService(t *testing.T) (*Service, *pgxpool.Pool, context.Context) {
	t.Helper()

	repository, pool, ctx := integrationChatRepository(t)

	projects := project.NewRepository(pool, db.New(pool))

	return NewService(
		repository,
		projects,
		project.NewAuthorizer(projects),
		nil,
		nil,
		0,
		nil,
		nil,
	), pool, ctx
}

func TestServiceGroupLifecycleAndPermissions(t *testing.T) {
	service, pool, ctx := integrationChatService(t)

	const tenant = "tenant-a"

	addTenantMembers(t, pool, ctx, tenant, "alice", "bob", "eve")

	alice := auth.Identity{TenantID: tenant, UserID: "alice"}
	bob := auth.Identity{TenantID: tenant, UserID: "bob"}

	if _, err := service.CreateGroup(ctx, CreateGroupInput{
		Identity: alice,
		Title:    " ",
	}); !errors.Is(err, ErrInvalidTitle) {
		t.Fatalf("empty title error = %v, want ErrInvalidTitle", err)
	}

	if _, err := service.CreateGroup(ctx, CreateGroupInput{
		Identity:  alice,
		Title:     "decisions",
		MemberIDs: []string{"bob", "not-a-member"},
	}); !errors.Is(err, ErrInvalidMembers) {
		t.Fatalf("non-workspace member error = %v, want ErrInvalidMembers", err)
	}

	detail, err := service.CreateGroup(ctx, CreateGroupInput{
		Identity:  alice,
		Title:     "decisions",
		MemberIDs: []string{"bob", "bob"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if detail.Type != ConversationTypeGroup {
		t.Fatalf("type = %s, want group", detail.Type)
	}
	if len(detail.Members) != 2 {
		t.Fatalf("members = %+v, want alice and bob", detail.Members)
	}
	if detail.Members[0].UserID != "alice" || detail.Members[0].Role != MemberRoleOwner {
		t.Fatalf("owner membership = %+v", detail.Members[0])
	}

	// Non-members of the conversation cannot read it even when they are on
	// the workspace.
	eve := auth.Identity{TenantID: tenant, UserID: "eve"}
	if _, err := service.Get(ctx, eve, detail.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("eve get error = %v, want ErrForbidden", err)
	}

	if err := service.AddMember(ctx, bob, detail.ID, "eve"); !errors.Is(err, ErrNotConversationOwner) {
		t.Fatalf("non-owner add error = %v, want ErrNotConversationOwner", err)
	}

	if err := service.AddMember(ctx, alice, detail.ID, "eve"); err != nil {
		t.Fatal(err)
	}

	if err := service.RemoveMember(ctx, alice, detail.ID, "alice"); !errors.Is(err, ErrCannotRemoveSelf) {
		t.Fatalf("self removal error = %v, want ErrCannotRemoveSelf", err)
	}

	if err := service.RemoveMember(ctx, alice, detail.ID, "eve"); err != nil {
		t.Fatal(err)
	}

	after, err := service.Get(ctx, alice, detail.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Members) != 2 {
		t.Fatalf("members after removal = %+v, want 2", after.Members)
	}

	// Chat history is returned oldest first, with a cursor for older pages.
	for i := 0; i < 3; i++ {
		if _, _, err := service.repository.CreateMessage(ctx, CreateMessageParams{
			ID:             uuid.NewString(),
			TenantID:       tenant,
			ConversationID: detail.ID,
			Kind:           MessageKindUser,
			AuthorUserID:   stringPointer("alice"),
			Body:           "message",
		}); err != nil {
			t.Fatal(err)
		}
	}

	firstPage, err := service.ListMessages(ctx, ListMessagesInput{
		Identity:       alice,
		ConversationID: detail.ID,
		Limit:          2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(firstPage.Items) != 2 || firstPage.Items[0].Seq != 2 || firstPage.Items[1].Seq != 3 {
		t.Fatalf("first page seqs = %v, want [2 3]", seqs(firstPage.Items))
	}
	if firstPage.NextCursor == nil {
		t.Fatal("expected a next cursor for older messages")
	}

	secondPage, err := service.ListMessages(ctx, ListMessagesInput{
		Identity:       alice,
		ConversationID: detail.ID,
		Before:         *firstPage.NextCursor,
		Limit:          2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(secondPage.Items) != 1 || secondPage.Items[0].Seq != 1 {
		t.Fatalf("second page seqs = %v, want [1]", seqs(secondPage.Items))
	}
	if secondPage.NextCursor != nil {
		t.Fatal("unexpected next cursor on the last page")
	}

	if err := service.MarkRead(ctx, alice, detail.ID, 3); err != nil {
		t.Fatal(err)
	}

	member, err := service.repository.GetMember(ctx, tenant, detail.ID, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if member.LastReadSeq != 3 {
		t.Fatalf("last read seq = %d, want 3", member.LastReadSeq)
	}
}

func TestServiceDMValidation(t *testing.T) {
	service, pool, ctx := integrationChatService(t)

	const tenant = "tenant-a"

	addTenantMembers(t, pool, ctx, tenant, "alice", "bob", "carol")

	alice := auth.Identity{TenantID: tenant, UserID: "alice"}
	bob := auth.Identity{TenantID: tenant, UserID: "bob"}
	carol := auth.Identity{TenantID: tenant, UserID: "carol"}

	if _, err := service.CreateDM(ctx, CreateDMInput{
		Identity:    alice,
		OtherUserID: "alice",
	}); !errors.Is(err, ErrInvalidMembers) {
		t.Fatalf("self dm error = %v, want ErrInvalidMembers", err)
	}

	if _, err := service.CreateDM(ctx, CreateDMInput{
		Identity:    alice,
		OtherUserID: "not-a-member",
	}); !errors.Is(err, ErrInvalidMembers) {
		t.Fatalf("non-workspace dm error = %v, want ErrInvalidMembers", err)
	}

	detail, err := service.CreateDM(ctx, CreateDMInput{
		Identity:    alice,
		OtherUserID: "bob",
	})
	if err != nil {
		t.Fatal(err)
	}

	if detail.Type != ConversationTypeDM {
		t.Fatalf("type = %s, want dm", detail.Type)
	}
	if len(detail.Members) != 2 {
		t.Fatalf("dm members = %+v, want 2", detail.Members)
	}

	// Starting the same DM from the other side returns the same thread.
	again, err := service.CreateDM(ctx, CreateDMInput{
		Identity:    bob,
		OtherUserID: "alice",
	})
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != detail.ID {
		t.Fatalf("dm id changed: %s != %s", again.ID, detail.ID)
	}

	for _, identity := range []auth.Identity{alice, bob} {
		conversations, err := service.List(ctx, identity)
		if err != nil {
			t.Fatal(err)
		}
		if len(conversations) != 1 || conversations[0].ID != detail.ID {
			t.Fatalf("%s conversations = %+v, want the dm", identity.UserID, conversations)
		}
	}

	// Another workspace member cannot see or open the DM.
	conversations, err := service.List(ctx, carol)
	if err != nil {
		t.Fatal(err)
	}
	if len(conversations) != 0 {
		t.Fatalf("carol conversations = %+v, want none", conversations)
	}

	if _, err := service.Get(ctx, carol, detail.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("carol get error = %v, want ErrForbidden", err)
	}
}

func TestServiceProjectChatVisibilityAndSync(t *testing.T) {
	service, pool, ctx := integrationChatService(t)

	const tenant = "tenant-a"

	projectID := uuid.NewString()

	insertProject(t, pool, ctx, tenant, projectID, map[string]string{
		"alice": "manager",
		"bob":   "member",
	})

	addTenantMembers(t, pool, ctx, tenant, "alice", "bob", "admin", "stranger")

	alice := auth.Identity{TenantID: tenant, UserID: "alice"}
	bob := auth.Identity{TenantID: tenant, UserID: "bob"}
	admin := auth.Identity{TenantID: tenant, UserID: "admin", Role: auth.RoleAdmin}
	stranger := auth.Identity{TenantID: tenant, UserID: "stranger"}

	if _, err := service.ProjectChat(ctx, stranger, projectID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("stranger project chat error = %v, want ErrForbidden", err)
	}

	if _, err := service.ProjectChat(ctx, alice, uuid.NewString()); !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("unknown project error = %v, want ErrProjectNotFound", err)
	}

	managerView, err := service.ProjectChat(ctx, alice, projectID)
	if err != nil {
		t.Fatal(err)
	}
	if managerView.Type != ConversationTypeProject || managerView.ProjectID == nil || *managerView.ProjectID != projectID {
		t.Fatalf("project conversation = %+v", managerView.Conversation)
	}
	if len(managerView.Members) != 2 {
		t.Fatalf("project members = %+v, want alice and bob", managerView.Members)
	}

	memberView, err := service.ProjectChat(ctx, bob, projectID)
	if err != nil {
		t.Fatal(err)
	}
	if memberView.ID != managerView.ID {
		t.Fatalf("project conversation id changed: %s != %s", memberView.ID, managerView.ID)
	}

	// Admins can open a project chat even without project membership; they
	// get an explicit membership row so reads and unread counts work.
	adminView, err := service.ProjectChat(ctx, admin, projectID)
	if err != nil {
		t.Fatal(err)
	}
	if len(adminView.Members) != 3 {
		t.Fatalf("admin view members = %+v, want 3", adminView.Members)
	}

	// Leaving the project removes the project-sourced membership, while the
	// admin's manual membership survives the sync.
	if _, err := pool.Exec(
		ctx,
		`DELETE FROM project_members WHERE tenant_id = $1 AND project_id = $2 AND user_id = 'bob'`,
		tenant,
		projectID,
	); err != nil {
		t.Fatal(err)
	}

	afterLeave, err := service.ProjectChat(ctx, alice, projectID)
	if err != nil {
		t.Fatal(err)
	}
	if len(afterLeave.Members) != 2 {
		t.Fatalf("members after bob left = %+v, want alice and admin", afterLeave.Members)
	}
	for _, member := range afterLeave.Members {
		if member.UserID == "bob" {
			t.Fatal("bob is still a member after leaving the project")
		}
	}

	// Bob keeps history access only while he is a project member.
	if _, err := service.Get(ctx, bob, managerView.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("bob chat access after leaving = %v, want ErrForbidden", err)
	}
}
