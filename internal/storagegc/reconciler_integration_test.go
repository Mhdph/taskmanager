package storagegc

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/mhdph/taskmanager/internal/database/generated"
	"github.com/mhdph/taskmanager/internal/storage"
)

func newQueries(pool *pgxpool.Pool) *db.Queries {
	return db.New(pool)
}

// nullableTime returns the timestamp for a soft-deleted row, or NULL.
func nullableTime(deleted bool) any {
	if !deleted {
		return nil
	}

	return time.Now()
}

// integrationSchema creates an isolated schema with the application
// migrations applied, mirroring the other integration test suites.
func integrationSchema(t *testing.T) (*pgxpool.Pool, context.Context) {
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
		_, _ = admin.Exec(
			context.Background(),
			`DROP SCHEMA `+schema+` CASCADE`,
		)
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

	return pool, ctx
}

func TestSQLReferencesOnlyReportsLiveAttachments(t *testing.T) {
	pool, ctx := integrationSchema(t)

	const tenant = "tenant-a"

	projectID := uuid.NewString()
	statusID := uuid.NewString()
	taskID := uuid.NewString()
	conversationID := uuid.NewString()

	if _, err := pool.Exec(
		ctx,
		`INSERT INTO projects (id, tenant_id, name, created_by_user_id)
		 VALUES ($1, $2, 'project', 'alice')`,
		projectID,
		tenant,
	); err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(
		ctx,
		`INSERT INTO project_statuses (id, tenant_id, project_id, name, position, category)
		 VALUES ($1, $2, $3, 'todo', 0, 'todo')`,
		statusID,
		tenant,
		projectID,
	); err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(
		ctx,
		`INSERT INTO tasks (id, tenant_id, project_id, status_id, title, created_by_user_id)
		 VALUES ($1, $2, $3, $4, 'task', 'alice')`,
		taskID,
		tenant,
		projectID,
		statusID,
	); err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(
		ctx,
		`INSERT INTO conversations (id, tenant_id, type, title, created_by_user_id)
		 VALUES ($1, $2, 'group', 'chat', 'alice')`,
		conversationID,
		tenant,
	); err != nil {
		t.Fatal(err)
	}

	liveTaskKey := storage.TaskAttachmentObjectKey(
		tenant,
		projectID,
		taskID,
		"live-task",
	)
	deletedTaskKey := storage.TaskAttachmentObjectKey(
		tenant,
		projectID,
		taskID,
		"deleted-task",
	)
	liveChatKey := storage.ChatAttachmentObjectKey(
		tenant,
		conversationID,
		"live-chat",
	)
	deletedChatKey := storage.ChatAttachmentObjectKey(
		tenant,
		conversationID,
		"deleted-chat",
	)

	insertTaskAttachment := func(
		key string,
		deleted bool,
	) {
		t.Helper()

		if _, err := pool.Exec(
			ctx,
			`INSERT INTO task_attachments (
				id, tenant_id, project_id, task_id, uploaded_by_user_id,
				filename, content_type, size_bytes, object_key, deleted_at
			 ) VALUES ($1, $2, $3, $4, 'alice', 'file.txt', 'text/plain', 3, $5, $6)`,
			uuid.NewString(),
			tenant,
			projectID,
			taskID,
			key,
			nullableTime(deleted),
		); err != nil {
			t.Fatal(err)
		}
	}

	insertChatAttachment := func(
		key string,
		deleted bool,
	) {
		t.Helper()

		if _, err := pool.Exec(
			ctx,
			`INSERT INTO message_attachments (
				id, tenant_id, conversation_id, uploaded_by_user_id,
				filename, content_type, size_bytes, object_key, deleted_at
			 ) VALUES ($1, $2, $3, 'alice', 'file.txt', 'text/plain', 3, $4, $5)`,
			uuid.NewString(),
			tenant,
			conversationID,
			key,
			nullableTime(deleted),
		); err != nil {
			t.Fatal(err)
		}
	}

	insertTaskAttachment(liveTaskKey, false)
	insertTaskAttachment(deletedTaskKey, true)
	insertChatAttachment(liveChatKey, false)
	insertChatAttachment(deletedChatKey, true)

	references := NewSQLReferences(newQueries(pool))

	keys := []string{
		liveTaskKey,
		deletedTaskKey,
		liveChatKey,
		deletedChatKey,
	}

	live, err := references.LiveObjectKeys(ctx, keys)
	if err != nil {
		t.Fatal(err)
	}

	sort.Strings(live)

	want := []string{liveChatKey, liveTaskKey}

	if len(live) != len(want) {
		t.Fatalf("live keys = %v, want %v", live, want)
	}

	for i, key := range want {
		if live[i] != key {
			t.Fatalf("live keys = %v, want %v", live, want)
		}
	}
}

func TestReconcileRemovesObjectsOfCascadedRows(t *testing.T) {
	pool, ctx := integrationSchema(t)

	const tenant = "tenant-a"

	projectID := uuid.NewString()
	statusID := uuid.NewString()
	cascadedTaskID := uuid.NewString()
	liveTaskID := uuid.NewString()

	if _, err := pool.Exec(
		ctx,
		`INSERT INTO projects (id, tenant_id, name, created_by_user_id)
		 VALUES ($1, $2, 'project', 'alice')`,
		projectID,
		tenant,
	); err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(
		ctx,
		`INSERT INTO project_statuses (id, tenant_id, project_id, name, position, category)
		 VALUES ($1, $2, $3, 'todo', 0, 'todo')`,
		statusID,
		tenant,
		projectID,
	); err != nil {
		t.Fatal(err)
	}

	for _, taskID := range []string{cascadedTaskID, liveTaskID} {
		if _, err := pool.Exec(
			ctx,
			`INSERT INTO tasks (id, tenant_id, project_id, status_id, title, created_by_user_id)
			 VALUES ($1, $2, $3, $4, 'task', 'alice')`,
			taskID,
			tenant,
			projectID,
			statusID,
		); err != nil {
			t.Fatal(err)
		}
	}

	liveKey := storage.TaskAttachmentObjectKey(
		tenant,
		projectID,
		liveTaskID,
		"live",
	)
	cascadedKey := storage.TaskAttachmentObjectKey(
		tenant,
		projectID,
		cascadedTaskID,
		"cascaded",
	)

	for _, attachment := range []struct {
		taskID string
		key    string
	}{
		{taskID: liveTaskID, key: liveKey},
		{taskID: cascadedTaskID, key: cascadedKey},
	} {
		if _, err := pool.Exec(
			ctx,
			`INSERT INTO task_attachments (
				id, tenant_id, project_id, task_id, uploaded_by_user_id,
				filename, content_type, size_bytes, object_key
			 ) VALUES ($1, $2, $3, $4, 'alice', 'file.txt', 'text/plain', 3, $5)`,
			uuid.NewString(),
			tenant,
			projectID,
			attachment.taskID,
			attachment.key,
		); err != nil {
			t.Fatal(err)
		}
	}

	// Deleting the task cascades the attachment row away. The object stays
	// in the bucket: this is exactly the leak the reconciler repairs.
	if _, err := pool.Exec(
		ctx,
		`DELETE FROM tasks WHERE id = $1`,
		cascadedTaskID,
	); err != nil {
		t.Fatal(err)
	}

	store := &fakeStore{
		objects: []storage.ObjectInfo{
			{
				Key:          liveKey,
				LastModified: time.Now().Add(-2 * time.Hour),
			},
			{
				Key:          cascadedKey,
				LastModified: time.Now().Add(-2 * time.Hour),
			},
		},
	}

	reconciler := NewReconciler(
		store,
		NewSQLReferences(newQueries(pool)),
	)

	stats, err := reconciler.Reconcile(ctx, Options{})
	if err != nil {
		t.Fatal(err)
	}

	if stats.Deleted != 1 {
		t.Fatalf("stats = %+v, want one deletion", stats)
	}

	if len(store.deleted) != 1 || store.deleted[0] != cascadedKey {
		t.Fatalf("deleted = %v, want %v", store.deleted, cascadedKey)
	}
}
