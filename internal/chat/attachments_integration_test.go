package chat

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mhdph/taskmanager/internal/auth"
	"github.com/mhdph/taskmanager/internal/storage"
)

type fakeFileStorage struct {
	mu sync.Mutex

	objects map[string][]byte

	deleted []string
}

func newFakeFileStorage() *fakeFileStorage {
	return &fakeFileStorage{
		objects: make(map[string][]byte),
	}
}

func (f *fakeFileStorage) Upload(
	_ context.Context,
	input storage.UploadInput,
) error {
	data, err := io.ReadAll(input.Reader)
	if err != nil {
		return err
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	f.objects[input.ObjectKey] = data

	return nil
}

func (f *fakeFileStorage) Delete(
	_ context.Context,
	objectKey string,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	delete(f.objects, objectKey)
	f.deleted = append(f.deleted, objectKey)

	return nil
}

func (f *fakeFileStorage) PresignedDownloadURL(
	_ context.Context,
	objectKey string,
	filename string,
	_ time.Duration,
) (string, error) {
	return "https://storage.test/" + objectKey + "/" + filename, nil
}

func (f *fakeFileStorage) objectCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return len(f.objects)
}

func (f *fakeFileStorage) wasDeleted(objectKey string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, deleted := range f.deleted {
		if deleted == objectKey {
			return true
		}
	}

	return false
}

func TestServiceAttachmentUploadSendAndAccess(t *testing.T) {
	repository, pool, ctx := integrationChatRepository(t)

	const tenant = "tenant-a"

	addTenantMembers(t, pool, ctx, tenant, "alice", "bob", "eve")

	fileStorage := newFakeFileStorage()

	service := NewService(
		repository,
		nil,
		nil,
		nil,
		fileStorage,
		1<<20,
		nil,
		nil,
	)

	alice := auth.Identity{TenantID: tenant, UserID: "alice"}
	bob := auth.Identity{TenantID: tenant, UserID: "bob"}
	eve := auth.Identity{TenantID: tenant, UserID: "eve"}

	detail, err := service.CreateGroup(ctx, CreateGroupInput{
		Identity:  alice,
		Title:     "attachments",
		MemberIDs: []string{"bob"},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = service.UploadAttachment(ctx, UploadAttachmentInput{
		Identity:       eve,
		ConversationID: detail.ID,
		Filename:       "intruder.txt",
		Size:           4,
		Reader:         strings.NewReader("nope"),
	})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("non-member upload error = %v, want ErrForbidden", err)
	}

	attachment, err := service.UploadAttachment(ctx, UploadAttachmentInput{
		Identity:       alice,
		ConversationID: detail.ID,
		Filename:       "../reports/design.pdf",
		ContentType:    "application/pdf",
		Size:           int64(len("hello attachment")),
		Reader:         strings.NewReader("hello attachment"),
	})
	if err != nil {
		t.Fatal(err)
	}

	if attachment.Filename != "design.pdf" {
		t.Fatalf("filename = %q, want sanitized design.pdf", attachment.Filename)
	}
	if attachment.MessageID != nil {
		t.Fatalf("new upload is already attached: %+v", attachment)
	}

	// Another member cannot steal someone else's pending upload.
	if _, _, err := service.SendMessage(ctx, SendMessageInput{
		Identity:       bob,
		ConversationID: detail.ID,
		AttachmentIDs:  []string{attachment.ID},
	}); !errors.Is(err, ErrInvalidAttachment) {
		t.Fatalf("cross-user attach error = %v, want ErrInvalidAttachment", err)
	}

	if attachment.MessageID != nil {
		t.Fatal("cross-user attach bound the pending upload")
	}

	message, created, err := service.SendMessage(ctx, SendMessageInput{
		Identity:       alice,
		ConversationID: detail.ID,
		AttachmentIDs:  []string{attachment.ID},
	})
	if err != nil || !created {
		t.Fatalf("send attachment-only message: created=%v err=%v", created, err)
	}
	if message.Body != "" || len(message.Attachments) != 1 {
		t.Fatalf("message = %+v, want attachment-only", message)
	}

	bound, err := repository.GetAttachment(
		ctx,
		tenant,
		detail.ID,
		attachment.ID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if bound.MessageID == nil || *bound.MessageID != message.ID {
		t.Fatalf("attachment message id = %v, want %s", bound.MessageID, message.ID)
	}

	// The same upload cannot be bound twice.
	if _, _, err := service.SendMessage(ctx, SendMessageInput{
		Identity:       alice,
		ConversationID: detail.ID,
		Body:           "reuse",
		AttachmentIDs:  []string{attachment.ID},
	}); !errors.Is(err, ErrInvalidAttachment) {
		t.Fatalf("reuse error = %v, want ErrInvalidAttachment", err)
	}

	page, err := service.ListMessages(ctx, ListMessagesInput{
		Identity:       alice,
		ConversationID: detail.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || len(page.Items[0].Attachments) != 1 {
		t.Fatalf("history = %+v, want one message with one attachment", page.Items)
	}

	download, err := service.GetAttachmentDownload(
		ctx,
		bob,
		detail.ID,
		attachment.ID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if download.URL == "" || download.ExpiresAt.IsZero() {
		t.Fatalf("download = %+v", download)
	}

	if _, err := service.GetAttachmentDownload(
		ctx,
		eve,
		detail.ID,
		attachment.ID,
	); !errors.Is(err, ErrForbidden) {
		t.Fatalf("non-member download error = %v, want ErrForbidden", err)
	}

	if err := service.CancelAttachment(
		ctx,
		alice,
		detail.ID,
		attachment.ID,
	); !errors.Is(err, ErrAttachmentAttached) {
		t.Fatalf("cancel attached error = %v, want ErrAttachmentAttached", err)
	}

	// Deleting the message removes the metadata and the storage object.
	if _, err := service.DeleteMessage(ctx, DeleteMessageInput{
		Identity:       alice,
		ConversationID: detail.ID,
		MessageID:      message.ID,
	}); err != nil {
		t.Fatal(err)
	}

	if !fileStorage.wasDeleted(bound.ObjectKey) {
		t.Fatal("attachment object was not deleted with the message")
	}

	if _, err := service.GetAttachmentDownload(
		ctx,
		alice,
		detail.ID,
		attachment.ID,
	); !errors.Is(err, ErrAttachmentNotFound) {
		t.Fatalf("download after delete error = %v, want ErrAttachmentNotFound", err)
	}
}

func TestServiceAttachmentValidationAndCancelAndCleanup(t *testing.T) {
	repository, pool, ctx := integrationChatRepository(t)

	const tenant = "tenant-a"

	addTenantMembers(t, pool, ctx, tenant, "alice", "bob")

	fileStorage := newFakeFileStorage()

	service := NewService(
		repository,
		nil,
		nil,
		nil,
		fileStorage,
		1024,
		nil,
		nil,
	)

	alice := auth.Identity{TenantID: tenant, UserID: "alice"}
	bob := auth.Identity{TenantID: tenant, UserID: "bob"}

	detail, err := service.CreateGroup(ctx, CreateGroupInput{
		Identity:  alice,
		Title:     "validation",
		MemberIDs: []string{"bob"},
	})
	if err != nil {
		t.Fatal(err)
	}

	upload := func(filename string, size int64) (Attachment, error) {
		return service.UploadAttachment(ctx, UploadAttachmentInput{
			Identity:       alice,
			ConversationID: detail.ID,
			Filename:       filename,
			Size:           size,
			Reader:         strings.NewReader(strings.Repeat("x", int(size))),
		})
	}

	if _, err := upload("", 1); !errors.Is(err, ErrInvalidAttachmentFilename) {
		t.Fatalf("empty filename error = %v", err)
	}

	if _, err := upload("zero.txt", 0); !errors.Is(err, ErrInvalidAttachment) {
		t.Fatalf("zero size error = %v", err)
	}

	if _, err := upload("big.bin", 2048); !errors.Is(err, ErrAttachmentTooLarge) {
		t.Fatalf("oversized upload error = %v", err)
	}

	first, err := upload("first.txt", 5)
	if err != nil {
		t.Fatal(err)
	}

	second, err := upload("second.txt", 6)
	if err != nil {
		t.Fatal(err)
	}

	if err := service.CancelAttachment(
		ctx,
		alice,
		detail.ID,
		first.ID,
	); err != nil {
		t.Fatal(err)
	}

	if fileStorage.objectCount() != 1 {
		t.Fatalf("object count = %d, want 1 after cancel", fileStorage.objectCount())
	}

	if _, err := repository.GetAttachment(
		ctx,
		tenant,
		detail.ID,
		first.ID,
	); !errors.Is(err, ErrAttachmentNotFound) {
		t.Fatalf("cancelled attachment lookup error = %v", err)
	}

	// Only the uploader can cancel.
	if err := service.CancelAttachment(
		ctx,
		bob,
		detail.ID,
		second.ID,
	); !errors.Is(err, ErrForbidden) {
		t.Fatalf("non-uploader cancel error = %v, want ErrForbidden", err)
	}

	// Age the pending upload so the sweeper picks it up.
	if _, err := pool.Exec(
		ctx,
		`UPDATE message_attachments
		 SET created_at = NOW() - INTERVAL '2 days'
		 WHERE id = $1`,
		second.ID,
	); err != nil {
		t.Fatal(err)
	}

	removed, err := service.CleanupStaleAttachments(ctx, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("cleanup removed %d, want 1", removed)
	}

	if fileStorage.objectCount() != 0 {
		t.Fatalf("object count = %d, want 0 after cleanup", fileStorage.objectCount())
	}

	if _, err := repository.GetAttachment(
		ctx,
		tenant,
		detail.ID,
		second.ID,
	); !errors.Is(err, ErrAttachmentNotFound) {
		t.Fatalf("cleaned attachment lookup error = %v", err)
	}
}
