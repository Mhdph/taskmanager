package chat

import (
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/mhdph/taskmanager/internal/auth"
)

func TestChatStreamTokenSingleUseAndExpiry(t *testing.T) {
	repository, _, ctx := integrationChatRepository(t)

	secret := make([]byte, streamTokenBytes)
	if _, err := rand.Read(secret); err != nil {
		t.Fatal(err)
	}

	hash := sha256.Sum256(secret)

	if _, err := repository.CreateStreamToken(ctx, CreateStreamTokenParams{
		TokenHash: hash[:],
		TenantID:  "tenant-a",
		UserID:    "alice",
		TTL:       time.Minute,
	}); err != nil {
		t.Fatal(err)
	}

	identity, err := repository.ConsumeStreamToken(ctx, hash[:])
	if err != nil {
		t.Fatal(err)
	}
	if identity.TenantID != "tenant-a" || identity.UserID != "alice" {
		t.Fatalf("identity = %+v", identity)
	}

	// The row is deleted by consumption, so the same ticket cannot be used
	// twice even across instances.
	if _, err := repository.ConsumeStreamToken(
		ctx,
		hash[:],
	); !errors.Is(err, ErrInvalidStreamToken) {
		t.Fatalf("second consume error = %v, want ErrInvalidStreamToken", err)
	}

	// A short-lived ticket becomes unusable once expired.
	expiringSecret := make([]byte, streamTokenBytes)
	if _, err := rand.Read(expiringSecret); err != nil {
		t.Fatal(err)
	}

	expiringHash := sha256.Sum256(expiringSecret)

	if _, err := repository.CreateStreamToken(ctx, CreateStreamTokenParams{
		TokenHash: expiringHash[:],
		TenantID:  "tenant-a",
		UserID:    "bob",
		TTL:       50 * time.Millisecond,
	}); err != nil {
		t.Fatal(err)
	}

	time.Sleep(120 * time.Millisecond)

	if _, err := repository.ConsumeStreamToken(
		ctx,
		expiringHash[:],
	); !errors.Is(err, ErrInvalidStreamToken) {
		t.Fatalf("expired consume error = %v, want ErrInvalidStreamToken", err)
	}

	deleted, err := repository.DeleteExpiredStreamTokens(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Fatalf("deleted expired tokens = %d, want 1", deleted)
	}

	// The expired row is gone, so a second sweep deletes nothing.
	deleted, err = repository.DeleteExpiredStreamTokens(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 0 {
		t.Fatalf("second sweep deleted %d, want 0", deleted)
	}

}

func TestServiceStreamTokenLifecycle(t *testing.T) {
	repository, pool, ctx := integrationChatRepository(t)

	service := NewService(
		repository,
		nil,
		nil,
		nil,
		nil,
		0,
		nil,
		nil,
	)

	identity := auth.Identity{
		TenantID: "tenant-a",
		UserID:   "alice",
	}

	issued, err := service.IssueStreamToken(ctx, identity)
	if err != nil {
		t.Fatal(err)
	}

	if len(issued.Token) != 43 {
		t.Fatalf("token length = %d, want 43", len(issued.Token))
	}

	remaining := time.Until(issued.ExpiresAt)
	if remaining < streamTokenTTL-time.Second ||
		remaining > streamTokenTTL+time.Second {
		t.Fatalf("token expiry = %v, want about %v", remaining, streamTokenTTL)
	}

	consumed, err := service.ConsumeStreamToken(ctx, issued.Token)
	if err != nil {
		t.Fatal(err)
	}
	if consumed.TenantID != identity.TenantID ||
		consumed.UserID != identity.UserID {
		t.Fatalf("consumed identity = %+v", consumed)
	}

	if _, err := service.ConsumeStreamToken(
		ctx,
		issued.Token,
	); !errors.Is(err, ErrInvalidStreamToken) {
		t.Fatalf("reused token error = %v", err)
	}

	for _, invalid := range []string{
		"",
		"not-base64",
		"short",
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	} {
		if _, err := service.ConsumeStreamToken(
			ctx,
			invalid,
		); !errors.Is(err, ErrInvalidStreamToken) {
			t.Fatalf(
				"invalid token %q error = %v, want ErrInvalidStreamToken",
				invalid,
				err,
			)
		}
	}

	// Expire the next ticket directly in the database to avoid a one-minute
	// test sleep. The check constraint still holds because expires_at stays
	// after created_at.
	expiring, err := service.IssueStreamToken(ctx, identity)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(
		ctx,
		`UPDATE chat_stream_tokens
		 SET expires_at = NOW() + INTERVAL '50 milliseconds'`,
	); err != nil {
		t.Fatal(err)
	}

	time.Sleep(120 * time.Millisecond)

	if _, err := service.ConsumeStreamToken(
		ctx,
		expiring.Token,
	); !errors.Is(err, ErrInvalidStreamToken) {
		t.Fatalf("expired token error = %v", err)
	}

	deleted, err := service.CleanupStreamTokens(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Fatalf("cleanup deleted = %d, want 1", deleted)
	}
}
