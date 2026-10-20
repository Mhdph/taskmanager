package storagegc

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/mhdph/taskmanager/internal/storage"
)

type fakeStore struct {
	objects []storage.ObjectInfo

	deleted []string

	deleteErr error
}

func (f *fakeStore) Delete(
	_ context.Context,
	objectKey string,
) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}

	f.deleted = append(f.deleted, objectKey)

	return nil
}

func (f *fakeStore) List(
	_ context.Context,
	prefix string,
	fn func(storage.ObjectInfo) error,
) error {
	for _, object := range f.objects {
		if len(object.Key) < len(prefix) ||
			object.Key[:len(prefix)] != prefix {

			continue
		}

		if err := fn(object); err != nil {
			return err
		}
	}

	return nil
}

type fakeReferences struct {
	live map[string]struct{}

	err error
}

func (f *fakeReferences) LiveObjectKeys(
	_ context.Context,
	objectKeys []string,
) ([]string, error) {
	if f.err != nil {
		return nil, f.err
	}

	keys := make([]string, 0, len(objectKeys))

	for _, key := range objectKeys {
		if _, ok := f.live[key]; ok {
			keys = append(keys, key)
		}
	}

	return keys, nil
}

func TestReconcileRemovesOnlyOrphanObjects(t *testing.T) {
	liveKey := storage.ChatAttachmentObjectKey(
		"tenant-a",
		"conversation-1",
		"attachment-1",
	)
	deletedTaskKey := storage.TaskAttachmentObjectKey(
		"tenant-a",
		"project-1",
		"task-1",
		"attachment-2",
	)
	orphanKey := storage.ChatAttachmentObjectKey(
		"tenant-a",
		"conversation-1",
		"attachment-3",
	)
	recentKey := storage.ChatAttachmentObjectKey(
		"tenant-a",
		"conversation-1",
		"attachment-4",
	)
	otherKey := "tenants/tenant-a/projects/project-1/avatar"

	old := time.Now().Add(-2 * time.Hour)

	store := &fakeStore{
		objects: []storage.ObjectInfo{
			{Key: liveKey, LastModified: old},
			{Key: deletedTaskKey, LastModified: old},
			{Key: orphanKey, LastModified: old},
			{Key: recentKey, LastModified: time.Now()},
			{Key: otherKey, LastModified: old},
		},
	}

	references := &fakeReferences{
		live: map[string]struct{}{
			liveKey: {},
		},
	}

	reconciler := NewReconciler(store, references)

	stats, err := reconciler.Reconcile(
		context.Background(),
		Options{
			GracePeriod: 10 * time.Minute,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	sort.Strings(store.deleted)

	want := []string{orphanKey, deletedTaskKey}

	if len(store.deleted) != len(want) {
		t.Fatalf("deleted = %v, want %v", store.deleted, want)
	}

	for i, key := range want {
		if store.deleted[i] != key {
			t.Fatalf("deleted = %v, want %v", store.deleted, want)
		}
	}

	if stats.Deleted != 2 {
		t.Fatalf("stats = %+v, want 2 deletions", stats)
	}
	if stats.SkippedRecent != 1 {
		t.Fatalf("stats = %+v, want one skipped recent object", stats)
	}
}

func TestReconcileStopsAtDeleteLimit(t *testing.T) {
	keys := make([]string, 0, 5)
	objects := make([]storage.ObjectInfo, 0, 5)

	old := time.Now().Add(-2 * time.Hour)

	for i := 0; i < 5; i++ {
		key := storage.ChatAttachmentObjectKey(
			"tenant-a",
			"conversation-1",
			"attachment-"+string(rune('a'+i)),
		)

		keys = append(keys, key)
		objects = append(objects, storage.ObjectInfo{
			Key:          key,
			LastModified: old,
		})
	}

	store := &fakeStore{objects: objects}

	reconciler := NewReconciler(
		store,
		&fakeReferences{live: map[string]struct{}{}},
	)

	stats, err := reconciler.Reconcile(
		context.Background(),
		Options{
			MaxDeletes: 2,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if stats.Deleted != 2 {
		t.Fatalf("stats = %+v, want 2 deletions", stats)
	}
	if len(store.deleted) != 2 {
		t.Fatalf("deleted = %v, want 2 objects", store.deleted)
	}
}

func TestReconcileReportsDeleteFailures(t *testing.T) {
	key := storage.ChatAttachmentObjectKey(
		"tenant-a",
		"conversation-1",
		"attachment-1",
	)

	store := &fakeStore{
		objects: []storage.ObjectInfo{
			{
				Key:          key,
				LastModified: time.Now().Add(-2 * time.Hour),
			},
		},
		deleteErr: errors.New("storage unavailable"),
	}

	reconciler := NewReconciler(
		store,
		&fakeReferences{live: map[string]struct{}{}},
	)

	stats, err := reconciler.Reconcile(context.Background(), Options{})
	if err != nil {
		t.Fatal(err)
	}

	if stats.Failed != 1 || stats.Deleted != 0 {
		t.Fatalf("stats = %+v, want one failure", stats)
	}
}
