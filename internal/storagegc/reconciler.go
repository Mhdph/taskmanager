// Package storagegc reconciles the object store with the database.
//
// Attachment objects are removed from storage on the happy path, but a
// crash, a failed cleanup or a cascading database delete can leave objects
// behind. The reconciler is the safety net: it lists the objects the
// application owns and deletes the ones that no live attachment row
// references any more.
package storagegc

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	db "github.com/mhdph/taskmanager/internal/database/generated"
	"github.com/mhdph/taskmanager/internal/storage"
)

const (
	// DefaultPrefix is the object prefix the reconciler walks.
	DefaultPrefix = storage.AttachmentObjectPrefix

	// DefaultGracePeriod protects objects that were just written. An
	// upload creates the object first and the attachment row second, so a
	// very fresh object may not be referenced yet.
	DefaultGracePeriod = 10 * time.Minute

	// DefaultBatchSize is how many keys are checked against the database
	// per query.
	DefaultBatchSize = 500

	// DefaultMaxDeletes bounds how many objects a single run removes, so
	// a large backlog cannot monopolise the process.
	DefaultMaxDeletes = 1000
)

// errStopScan ends the object walk early without failing the run.
var errStopScan = errors.New("stop storage scan")

// ObjectStore is the part of the object store the reconciler needs.
type ObjectStore interface {
	Delete(ctx context.Context, objectKey string) error

	List(
		ctx context.Context,
		prefix string,
		fn func(storage.ObjectInfo) error,
	) error
}

// ReferenceChecker reports which object keys are still referenced by a live
// attachment row.
type ReferenceChecker interface {
	LiveObjectKeys(
		ctx context.Context,
		objectKeys []string,
	) ([]string, error)
}

// SQLReferences checks references against the database.
type SQLReferences struct {
	queries *db.Queries
}

func NewSQLReferences(queries *db.Queries) *SQLReferences {
	return &SQLReferences{queries: queries}
}

func (s *SQLReferences) LiveObjectKeys(
	ctx context.Context,
	objectKeys []string,
) ([]string, error) {
	if len(objectKeys) == 0 {
		return nil, nil
	}

	keys, err := s.queries.ListLiveAttachmentObjectKeys(ctx, objectKeys)
	if err != nil {
		return nil, fmt.Errorf("list live attachment object keys: %w", err)
	}

	return keys, nil
}

// Reconciler removes attachment objects that no live attachment row
// references any more.
type Reconciler struct {
	store      ObjectStore
	references ReferenceChecker
}

func NewReconciler(
	store ObjectStore,
	references ReferenceChecker,
) *Reconciler {
	return &Reconciler{
		store:      store,
		references: references,
	}
}

// Options tunes a single reconciliation run.
type Options struct {
	// Prefix is the object prefix to walk.
	Prefix string

	// GracePeriod skips objects modified within this window.
	GracePeriod time.Duration

	// BatchSize is how many keys are checked per database query.
	BatchSize int

	// MaxDeletes bounds the objects removed in one run.
	MaxDeletes int

	// MaxScanned bounds the objects inspected in one run. Zero means the
	// whole prefix is walked.
	MaxScanned int
}

func (o Options) withDefaults() Options {
	if o.Prefix == "" {
		o.Prefix = DefaultPrefix
	}

	if o.GracePeriod <= 0 {
		o.GracePeriod = DefaultGracePeriod
	}

	if o.BatchSize <= 0 {
		o.BatchSize = DefaultBatchSize
	}

	if o.MaxDeletes <= 0 {
		o.MaxDeletes = DefaultMaxDeletes
	}

	return o
}

// Stats reports what a run did.
type Stats struct {
	// Scanned is the number of attachment objects inspected.
	Scanned int

	// Deleted is the number of orphan objects removed.
	Deleted int

	// SkippedRecent is the number of objects left alone because they were
	// modified inside the grace period.
	SkippedRecent int

	// Failed is the number of objects that could not be removed.
	Failed int
}

// Reconcile walks the object prefix once and removes the orphans.
func (r *Reconciler) Reconcile(
	ctx context.Context,
	options Options,
) (Stats, error) {
	options = options.withDefaults()

	cutoff := time.Now().Add(-options.GracePeriod)

	stats := Stats{}

	batch := make([]string, 0, options.BatchSize)

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}

		live, err := r.references.LiveObjectKeys(ctx, batch)
		if err != nil {
			return err
		}

		liveSet := make(map[string]struct{}, len(live))
		for _, key := range live {
			liveSet[key] = struct{}{}
		}

		for _, key := range batch {
			if _, ok := liveSet[key]; ok {
				continue
			}

			if stats.Deleted >= options.MaxDeletes {
				continue
			}

			if err := r.store.Delete(ctx, key); err != nil {
				stats.Failed++

				log.Printf(
					"storage reconcile: delete %s: %v",
					key,
					err,
				)

				continue
			}

			stats.Deleted++
		}

		batch = batch[:0]

		return nil
	}

	scanErr := r.store.List(
		ctx,
		options.Prefix,
		func(object storage.ObjectInfo) error {
			// Only objects the application owns may be removed.
			if !storage.IsAttachmentObjectKey(object.Key) {
				return nil
			}

			if object.LastModified.After(cutoff) {
				stats.SkippedRecent++
				return nil
			}

			stats.Scanned++

			batch = append(batch, object.Key)

			if len(batch) >= options.BatchSize {
				return flush()
			}

			if options.MaxScanned > 0 &&
				stats.Scanned >= options.MaxScanned {

				return errStopScan
			}

			return nil
		},
	)

	if errors.Is(scanErr, errStopScan) {
		scanErr = nil
	}

	if scanErr != nil {
		return stats, scanErr
	}

	if err := flush(); err != nil {
		return stats, err
	}

	return stats, nil
}

// Run reconciles on every interval until the context is canceled. It runs
// once at startup so a restart also cleans up after a crash.
func (r *Reconciler) Run(
	ctx context.Context,
	interval time.Duration,
	options Options,
) {
	if interval <= 0 {
		interval = time.Hour
	}

	r.runOnce(ctx, options)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.runOnce(ctx, options)
		}
	}
}

func (r *Reconciler) runOnce(
	ctx context.Context,
	options Options,
) {
	stats, err := r.Reconcile(ctx, options)
	if err != nil {
		log.Printf("storage reconcile: %v", err)
		return
	}

	if stats.Scanned > 0 || stats.Deleted > 0 || stats.Failed > 0 {
		log.Printf(
			"storage reconcile: scanned=%d deleted=%d failed=%d skipped_recent=%d",
			stats.Scanned,
			stats.Deleted,
			stats.Failed,
			stats.SkippedRecent,
		)
	}
}
