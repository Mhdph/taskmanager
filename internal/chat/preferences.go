package chat

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	db "github.com/mhdph/taskmanager/internal/database/generated"
)

// NotificationMode controls how chat notifications are routed for one
// conversation. It never changes what the WebSocket stream delivers.
type NotificationMode string

const (
	// NotificationModeAll notifies for every message. It is the default
	// for members without an explicit preference.
	NotificationModeAll NotificationMode = "all"

	// NotificationModeMentions notifies only when the member is mentioned
	// in the message body.
	NotificationModeMentions NotificationMode = "mentions"

	// NotificationModeMute silences notifications for the conversation.
	NotificationModeMute NotificationMode = "mute"
)

func (m NotificationMode) valid() bool {
	switch m {
	case NotificationModeAll,
		NotificationModeMentions,
		NotificationModeMute:

		return true
	default:
		return false
	}
}

// ParseNotificationMode normalizes a client supplied mode.
func ParseNotificationMode(value string) (NotificationMode, error) {
	mode := NotificationMode(
		strings.ToLower(strings.TrimSpace(value)),
	)

	if !mode.valid() {
		return "", ErrInvalidNotificationMode
	}

	return mode, nil
}

// GetNotificationPreference returns the stored mode for a member. Members
// without a stored preference use the default.
func (r *Repository) GetNotificationPreference(
	ctx context.Context,
	tenantID string,
	conversationID string,
	userID string,
) (NotificationMode, error) {
	id, err := parseUUID(conversationID)
	if err != nil {
		return "", fmt.Errorf("parse conversation id: %w", err)
	}

	mode, err := r.queries.GetChatNotificationPreference(
		ctx,
		db.GetChatNotificationPreferenceParams{
			ConversationID: id,
			UserID:         userID,
		},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return NotificationModeAll, nil
	}

	if err != nil {
		return "", fmt.Errorf("get notification preference: %w", err)
	}

	return NotificationMode(mode), nil
}

// SetNotificationPreference stores the mode for a member.
func (r *Repository) SetNotificationPreference(
	ctx context.Context,
	tenantID string,
	conversationID string,
	userID string,
	mode NotificationMode,
) error {
	id, err := parseUUID(conversationID)
	if err != nil {
		return fmt.Errorf("parse conversation id: %w", err)
	}

	if err := r.queries.UpsertChatNotificationPreference(
		ctx,
		db.UpsertChatNotificationPreferenceParams{
			ConversationID: id,
			TenantID:       tenantID,
			UserID:         userID,
			Mode:           string(mode),
		},
	); err != nil {
		return fmt.Errorf("upsert notification preference: %w", err)
	}

	return nil
}

// ListNotificationPreferences returns the explicit preferences of the given
// members. Members without a stored preference are absent from the map and
// use the default mode.
func (r *Repository) ListNotificationPreferences(
	ctx context.Context,
	tenantID string,
	conversationID string,
	userIDs []string,
) (map[string]NotificationMode, error) {
	preferences := make(map[string]NotificationMode, len(userIDs))

	if len(userIDs) == 0 {
		return preferences, nil
	}

	id, err := parseUUID(conversationID)
	if err != nil {
		return nil, fmt.Errorf("parse conversation id: %w", err)
	}

	rows, err := r.queries.ListChatNotificationPreferences(
		ctx,
		db.ListChatNotificationPreferencesParams{
			TenantID:       tenantID,
			ConversationID: id,
			UserIds:        userIDs,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("list notification preferences: %w", err)
	}

	for _, row := range rows {
		preferences[row.UserID] = NotificationMode(row.Mode)
	}

	return preferences, nil
}
