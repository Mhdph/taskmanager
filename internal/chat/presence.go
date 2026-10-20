package chat

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/mhdph/taskmanager/internal/database/generated"
)

const (
	// presenceLeaseTTL tolerates two missed heartbeats. The stream pings
	// (and refreshes the lease) every 30 seconds.
	presenceLeaseTTL = 90 * time.Second

	// presenceSweepInterval bounds how long a crashed instance can look
	// online. Worst case is TTL + interval.
	presenceSweepInterval = 30 * time.Second

	presenceSweepBatch = 100
)

// PresenceLease is one live WebSocket connection. Leases are stored in
// Postgres so every API instance sees the same online state.
type PresenceLease struct {
	ConnectionID string
	TenantID     string
	UserID       string
	InstanceID   string
	TTL          time.Duration
}

// PresenceUpdate is a committed online/offline transition that must be
// published to the broker.
type PresenceUpdate struct {
	TenantID string
	UserID   string
	Online   bool
}

func presenceLockKey(tenantID string, userID string) string {
	// Keep presence locks in their own hash namespace so a busy
	// conversation cannot serialize unrelated presence work by accident.
	return "presence:" + tenantID + ":" + userID
}

func validatePresenceLease(lease PresenceLease) (pgtype.UUID, error) {
	if lease.TTL <= 0 {
		return pgtype.UUID{}, fmt.Errorf("presence lease ttl must be positive")
	}

	connectionID, err := parseUUID(lease.ConnectionID)
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("parse presence connection id: %w", err)
	}

	return connectionID, nil
}

// AcquirePresence records a connection and reports whether the user became
// globally online (no other live lease existed). The per-user advisory lock
// makes the check-and-set atomic across instances.
func (r *Repository) AcquirePresence(
	ctx context.Context,
	lease PresenceLease,
) (bool, error) {
	connectionID, err := validatePresenceLease(lease)
	if err != nil {
		return false, err
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin presence transaction: %w", err)
	}

	defer tx.Rollback(ctx)

	qtx := r.queries.WithTx(tx)

	if err := qtx.LockChatPresence(
		ctx,
		presenceLockKey(lease.TenantID, lease.UserID),
	); err != nil {
		return false, fmt.Errorf("lock presence user: %w", err)
	}

	if err := qtx.UpsertChatPresenceLease(
		ctx,
		db.UpsertChatPresenceLeaseParams{
			ConnectionID:    connectionID,
			TenantID:        lease.TenantID,
			UserID:          lease.UserID,
			InstanceID:      lease.InstanceID,
			TtlMilliseconds: lease.TTL.Milliseconds(),
		},
	); err != nil {
		return false, fmt.Errorf("upsert presence lease: %w", err)
	}

	rows, err := qtx.MarkChatPresenceOnline(
		ctx,
		db.MarkChatPresenceOnlineParams{
			TenantID: lease.TenantID,
			UserID:   lease.UserID,
		},
	)
	if err != nil {
		return false, fmt.Errorf("mark presence online: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit presence acquisition: %w", err)
	}

	return rows > 0, nil
}

// RefreshPresence extends a live lease. It returns false when the row is
// missing or already expired; the caller must then run AcquirePresence,
// which may need to republish presence.online after a sweep.
func (r *Repository) RefreshPresence(
	ctx context.Context,
	connectionID string,
	instanceID string,
	ttl time.Duration,
) (bool, error) {
	id, err := validatePresenceLease(PresenceLease{
		ConnectionID: connectionID,
		TTL:          ttl,
	})
	if err != nil {
		return false, err
	}

	rows, err := r.queries.RefreshChatPresenceLease(
		ctx,
		db.RefreshChatPresenceLeaseParams{
			ConnectionID:    id,
			InstanceID:      instanceID,
			TtlMilliseconds: ttl.Milliseconds(),
		},
	)
	if err != nil {
		return false, fmt.Errorf("refresh presence lease: %w", err)
	}

	return rows > 0, nil
}

// ReleasePresence removes one connection and reports whether the user became
// globally offline. Deleting a connection that a sweeper already removed is
// safe; the state row makes the transition idempotent.
func (r *Repository) ReleasePresence(
	ctx context.Context,
	connectionID string,
	tenantID string,
	userID string,
) (bool, error) {
	id, err := parseUUID(connectionID)
	if err != nil {
		return false, fmt.Errorf("parse presence connection id: %w", err)
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin presence transaction: %w", err)
	}

	defer tx.Rollback(ctx)

	qtx := r.queries.WithTx(tx)

	if err := qtx.LockChatPresence(
		ctx,
		presenceLockKey(tenantID, userID),
	); err != nil {
		return false, fmt.Errorf("lock presence user: %w", err)
	}

	if err := qtx.DeleteChatPresenceLease(
		ctx,
		id,
	); err != nil {
		return false, fmt.Errorf("delete presence lease: %w", err)
	}

	if err := qtx.DeleteExpiredChatPresenceLeasesForUser(
		ctx,
		db.DeleteExpiredChatPresenceLeasesForUserParams{
			TenantID: tenantID,
			UserID:   userID,
		},
	); err != nil {
		return false, fmt.Errorf("delete expired presence leases: %w", err)
	}

	changed, err := r.reconcilePresenceTx(
		ctx,
		qtx,
		tenantID,
		userID,
	)
	if err != nil {
		return false, err
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit presence release: %w", err)
	}

	return changed, nil
}

// IsUserOnline reports whether the user has any live lease on any instance.
func (r *Repository) IsUserOnline(
	ctx context.Context,
	tenantID string,
	userID string,
) (bool, error) {
	online, err := r.queries.IsChatUserOnline(
		ctx,
		db.IsChatUserOnlineParams{
			TenantID: tenantID,
			UserID:   userID,
		},
	)
	if err != nil {
		return false, fmt.Errorf("check user presence: %w", err)
	}

	return online, nil
}

// OnlineUserIDs returns the subset of userIDs with a live lease on any
// instance. One query replaces the per-member local checks.
func (r *Repository) OnlineUserIDs(
	ctx context.Context,
	tenantID string,
	userIDs []string,
) ([]string, error) {
	if len(userIDs) == 0 {
		return []string{}, nil
	}

	online, err := r.queries.ListOnlineChatUserIDs(
		ctx,
		db.ListOnlineChatUserIDsParams{
			TenantID: tenantID,
			UserIds:  userIDs,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("list online users: %w", err)
	}

	return online, nil
}

// SweepExpiredPresence deletes expired leases and reports users whose last
// connection disappeared. Multiple sweepers may run this concurrently; the
// per-user lock and state row ensure only one publishes the offline event.
func (r *Repository) SweepExpiredPresence(
	ctx context.Context,
	limit int32,
) ([]PresenceUpdate, error) {
	if limit <= 0 {
		limit = presenceSweepBatch
	}

	users, err := r.queries.ListChatPresenceUsersWithExpiredLeases(
		ctx,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list expired presence users: %w", err)
	}

	updates := make([]PresenceUpdate, 0, len(users))

	for _, user := range users {
		changed, err := r.sweepPresenceUser(
			ctx,
			user.TenantID,
			user.UserID,
		)
		if err != nil {
			return nil, err
		}

		if changed {
			updates = append(updates, PresenceUpdate{
				TenantID: user.TenantID,
				UserID:   user.UserID,
				Online:   false,
			})
		}
	}

	return updates, nil
}

func (r *Repository) sweepPresenceUser(
	ctx context.Context,
	tenantID string,
	userID string,
) (bool, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin presence sweep transaction: %w", err)
	}

	defer tx.Rollback(ctx)

	qtx := r.queries.WithTx(tx)

	if err := qtx.LockChatPresence(
		ctx,
		presenceLockKey(tenantID, userID),
	); err != nil {
		return false, fmt.Errorf("lock presence user: %w", err)
	}

	if err := qtx.DeleteExpiredChatPresenceLeasesForUser(
		ctx,
		db.DeleteExpiredChatPresenceLeasesForUserParams{
			TenantID: tenantID,
			UserID:   userID,
		},
	); err != nil {
		return false, fmt.Errorf("delete expired presence leases: %w", err)
	}

	changed, err := r.reconcilePresenceTx(
		ctx,
		qtx,
		tenantID,
		userID,
	)
	if err != nil {
		return false, err
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit presence sweep: %w", err)
	}

	return changed, nil
}

// reconcilePresenceTx aligns the transition row with the live leases. The
// caller must hold the per-user advisory lock. It returns true only when the
// published online state changes.
func (r *Repository) reconcilePresenceTx(
	ctx context.Context,
	qtx *db.Queries,
	tenantID string,
	userID string,
) (bool, error) {
	count, err := qtx.CountLiveChatPresenceLeases(
		ctx,
		db.CountLiveChatPresenceLeasesParams{
			TenantID: tenantID,
			UserID:   userID,
		},
	)
	if err != nil {
		return false, fmt.Errorf("count live presence leases: %w", err)
	}

	if count > 0 {
		rows, err := qtx.MarkChatPresenceOnline(
			ctx,
			db.MarkChatPresenceOnlineParams{
				TenantID: tenantID,
				UserID:   userID,
			},
		)
		if err != nil {
			return false, fmt.Errorf("mark presence online: %w", err)
		}

		return rows > 0, nil
	}

	rows, err := qtx.MarkChatPresenceOffline(
		ctx,
		db.MarkChatPresenceOfflineParams{
			TenantID: tenantID,
			UserID:   userID,
		},
	)
	if err != nil {
		return false, fmt.Errorf("mark presence offline: %w", err)
	}

	return rows > 0, nil
}
