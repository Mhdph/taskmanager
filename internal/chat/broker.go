package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	// ChatEventsChannel is the Postgres NOTIFY channel shared by every
	// instance connected to this database.
	ChatEventsChannel = "chat_events"

	// maxNotifyPayload stays below Postgres' 8000 byte NOTIFY payload
	// limit. Message events are references (ids, not bodies), so this is
	// only a guard against accidental inline blobs.
	maxNotifyPayload = 7000
)

// EventPublisher is the subset of the broker used by the repository to
// publish inside the write transaction.
type EventPublisher interface {
	PublishTx(
		ctx context.Context,
		tx pgx.Tx,
		event Event,
	) error
}

// Broker is the fan-out transport. The in-process hub is deliberately not
// the source of truth: Postgres NOTIFY survives process restarts and fans
// out to every API instance without extra infrastructure. The interface is
// small so it can be swapped for Redis pub/sub later.
type Broker interface {
	EventPublisher

	Publish(ctx context.Context, event Event) error

	Start(
		ctx context.Context,
		handler func(context.Context, Event),
	) error

	Close() error
}

// PostgresBroker publishes events with pg_notify and listens on a dedicated
// pooled connection.
type PostgresBroker struct {
	pool *pgxpool.Pool

	handler func(context.Context, Event)

	cancel context.CancelFunc

	done chan struct{}

	startOnce sync.Once

	closeOnce sync.Once

	startErr error
}

func NewPostgresBroker(pool *pgxpool.Pool) *PostgresBroker {
	return &PostgresBroker{
		pool: pool,
	}
}

// PublishTx sends the event inside tx. NOTIFY is delivered only when the
// transaction commits, so a rolled back write never fans out.
func (b *PostgresBroker) PublishTx(
	ctx context.Context,
	tx pgx.Tx,
	event Event,
) error {
	payload, err := encodeEvent(event)
	if err != nil {
		return err
	}

	if _, err := tx.Exec(
		ctx,
		`SELECT pg_notify($1, $2)`,
		ChatEventsChannel,
		payload,
	); err != nil {
		return fmt.Errorf("notify chat event: %w", err)
	}

	return nil
}

// Publish sends an event after the database write has committed. Used for
// events that are not tied to a transaction, such as read receipts.
func (b *PostgresBroker) Publish(
	ctx context.Context,
	event Event,
) error {
	payload, err := encodeEvent(event)
	if err != nil {
		return err
	}

	if _, err := b.pool.Exec(
		ctx,
		`SELECT pg_notify($1, $2)`,
		ChatEventsChannel,
		payload,
	); err != nil {
		return fmt.Errorf("notify chat event: %w", err)
	}

	return nil
}

// Start opens the LISTEN connection synchronously, so the process does not
// accept traffic until events can actually be received, then consumes
// notifications in the background.
func (b *PostgresBroker) Start(
	ctx context.Context,
	handler func(context.Context, Event),
) error {
	if handler == nil {
		return errors.New("chat broker handler is required")
	}

	b.startOnce.Do(func() {
		listenCtx, cancel := context.WithCancel(ctx)

		b.cancel = cancel
		b.handler = handler
		b.done = make(chan struct{})

		conn, err := b.acquireListener(listenCtx)
		if err != nil {
			cancel()

			// Close waits on done; without a listener goroutine it must be
			// closed here or shutdown would stall for the full timeout.
			close(b.done)

			b.startErr = err
			return
		}

		go b.listenLoop(listenCtx, conn)
	})

	return b.startErr
}

// Close stops the listener and waits for its goroutine to exit.
func (b *PostgresBroker) Close() error {
	b.closeOnce.Do(func() {
		if b.cancel == nil {
			return
		}

		b.cancel()

		select {
		case <-b.done:
		case <-time.After(5 * time.Second):
			log.Printf("chat broker: listener did not stop in time")
		}
	})

	return nil
}

func (b *PostgresBroker) listenLoop(
	ctx context.Context,
	conn *pgxpool.Conn,
) {
	defer close(b.done)

	for {
		notification, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			if ctx.Err() != nil {
				conn.Release()
				return
			}

			log.Printf(
				"chat broker: wait for notification: %v; reconnecting",
				err,
			)

			conn.Release()

			conn = b.reconnect(ctx)
			if conn == nil {
				return
			}

			continue
		}

		var event Event

		if err := json.Unmarshal(
			[]byte(notification.Payload),
			&event,
		); err != nil {
			log.Printf(
				"chat broker: decode notification: %v",
				err,
			)
			continue
		}

		b.handler(ctx, event)
	}
}

// reconnect re-establishes LISTEN with capped exponential backoff. It
// returns nil when the context is canceled.
func (b *PostgresBroker) reconnect(
	ctx context.Context,
) *pgxpool.Conn {
	backoff := time.Second

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}

		conn, err := b.acquireListener(ctx)
		if err == nil {
			return conn
		}

		if ctx.Err() != nil {
			return nil
		}

		log.Printf("chat broker: reconnect: %v", err)

		backoff *= 2
		if backoff > 30*time.Second {
			backoff = 30 * time.Second
		}
	}
}

func (b *PostgresBroker) acquireListener(
	ctx context.Context,
) (*pgxpool.Conn, error) {
	conn, err := b.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire listen connection: %w", err)
	}

	if _, err := conn.Exec(
		ctx,
		`LISTEN `+pgx.Identifier{ChatEventsChannel}.Sanitize(),
	); err != nil {
		conn.Release()

		return nil, fmt.Errorf("listen on chat events: %w", err)
	}

	return conn, nil
}

func encodeEvent(event Event) (string, error) {
	payload, err := json.Marshal(event)
	if err != nil {
		return "", fmt.Errorf("encode chat event: %w", err)
	}

	if len(payload) >= maxNotifyPayload {
		return "", fmt.Errorf(
			"chat event payload too large: %d bytes",
			len(payload),
		)
	}

	return string(payload), nil
}
