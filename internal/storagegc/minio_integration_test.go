package storagegc

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mhdph/taskmanager/internal/storage"
)

// TestReconcileAgainstObjectStore exercises the reconciler against a real
// object store. It is skipped unless a MinIO endpoint is configured.
func TestReconcileAgainstObjectStore(t *testing.T) {
	endpoint := os.Getenv("TASKMANAGER_TEST_MINIO_ENDPOINT")
	if endpoint == "" {
		t.Skip("TASKMANAGER_TEST_MINIO_ENDPOINT is not set")
	}

	accessKey := envOrDefault(
		"TASKMANAGER_TEST_MINIO_ACCESS_KEY",
		"taskmanager",
	)
	secretKey := envOrDefault(
		"TASKMANAGER_TEST_MINIO_SECRET_KEY",
		"taskmanager-secret",
	)
	bucket := envOrDefault(
		"TASKMANAGER_TEST_MINIO_BUCKET",
		"taskmanager-reconcile-test",
	)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client, err := storage.NewClient(
		endpoint,
		accessKey,
		secretKey,
		bucket,
		false,
	)
	if err != nil {
		t.Fatal(err)
	}

	if err := client.EnsureBucket(ctx); err != nil {
		t.Fatal(err)
	}

	liveKey := storage.ChatAttachmentObjectKey(
		"tenant-a",
		"conversation-1",
		"live",
	)
	orphanKey := storage.ChatAttachmentObjectKey(
		"tenant-a",
		"conversation-1",
		"orphan",
	)
	foreignKey := "tenants/tenant-a/projects/project-1/avatar"

	for _, key := range []string{liveKey, orphanKey, foreignKey} {
		err := client.Upload(ctx, storage.UploadInput{
			ObjectKey:   key,
			Reader:      strings.NewReader("payload"),
			Size:        7,
			ContentType: "text/plain",
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(
			context.Background(),
			10*time.Second,
		)
		defer cleanupCancel()

		for _, key := range []string{liveKey, orphanKey, foreignKey} {
			_ = client.Delete(cleanupCtx, key)
		}
	})

	// The reconciler skips objects that were just written, so let the
	// objects age past the grace period.
	time.Sleep(2 * time.Second)

	store := &realStore{client: client}

	reconciler := NewReconciler(
		store,
		&fakeReferences{
			live: map[string]struct{}{
				liveKey: {},
			},
		},
	)

	stats, err := reconciler.Reconcile(ctx, Options{
		GracePeriod: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}

	if stats.Deleted != 1 {
		t.Fatalf("stats = %+v, want one deletion", stats)
	}

	if exists, err := store.exists(ctx, orphanKey); err != nil {
		t.Fatal(err)
	} else if exists {
		t.Fatal("orphan object was not deleted")
	}

	if exists, err := store.exists(ctx, liveKey); err != nil {
		t.Fatal(err)
	} else if !exists {
		t.Fatal("live object was deleted")
	}

	// Objects outside the attachment layout are never touched.
	if exists, err := store.exists(ctx, foreignKey); err != nil {
		t.Fatal(err)
	} else if !exists {
		t.Fatal("object outside the attachment layout was deleted")
	}
}

// realStore adapts the storage client to the reconciler interface and can
// probe for object existence.
type realStore struct {
	client *storage.Client
}

func (r *realStore) Delete(
	ctx context.Context,
	objectKey string,
) error {
	return r.client.Delete(ctx, objectKey)
}

func (r *realStore) List(
	ctx context.Context,
	prefix string,
	fn func(storage.ObjectInfo) error,
) error {
	return r.client.List(ctx, prefix, fn)
}

func (r *realStore) exists(
	ctx context.Context,
	objectKey string,
) (bool, error) {
	found := false

	err := r.client.List(
		ctx,
		objectKey,
		func(object storage.ObjectInfo) error {
			if object.Key == objectKey {
				found = true
			}

			return nil
		},
	)
	if err != nil {
		return false, err
	}

	return found, nil
}

func envOrDefault(
	name string,
	fallback string,
) string {
	if value := os.Getenv(name); value != "" {
		return value
	}

	return fallback
}
