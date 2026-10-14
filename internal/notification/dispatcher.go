package notification

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
)

var (
	ErrQueueFull        = errors.New("notification queue is full")
	ErrDispatcherClosed = errors.New("notification dispatcher is closed")
)

// Dispatcher delivers notifications from a bounded in-memory queue. Send only
// enqueues; Close drains accepted jobs until its context expires.
type Dispatcher struct {
	client     *Client
	jobs       chan deliveryJob
	done       chan struct{}
	ctx        context.Context
	cancel     context.CancelFunc
	retryDelay time.Duration
	mu         sync.Mutex
	closed     bool
}

type deliveryJob struct {
	payload []byte
	id      string
}

func NewDispatcher(client *Client) *Dispatcher {
	return newDispatcher(client, 2, 256, time.Second)
}

func newDispatcher(client *Client, workers, capacity int, retryDelay time.Duration) *Dispatcher {
	ctx, cancel := context.WithCancel(context.Background())
	d := &Dispatcher{
		client: client, jobs: make(chan deliveryJob, capacity), done: make(chan struct{}),
		ctx: ctx, cancel: cancel, retryDelay: retryDelay,
	}
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-d.ctx.Done():
					return
				case job, ok := <-d.jobs:
					if !ok || d.ctx.Err() != nil {
						return
					}
					if err := d.deliver(job); err != nil {
						log.Printf("notification delivery failed delivery_id=%s: %v", job.id, err)
					}
				}
			}
		}()
	}
	go func() {
		wg.Wait()
		close(d.done)
	}()
	return d
}

func (d *Dispatcher) Send(ctx context.Context, input SendInput) error {
	// Serialize before enqueueing so caller-owned maps and pointers cannot change
	// a queued notification or race with the delivery workers.
	payload, err := json.Marshal(input)
	if err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return ErrDispatcherClosed
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case d.jobs <- deliveryJob{payload: payload, id: uuid.NewString()}:
		return nil
	default:
		return ErrQueueFull
	}
}

func (d *Dispatcher) deliver(job deliveryJob) error {
	for attempt := 0; attempt < 3; attempt++ {
		ctx, cancel := context.WithTimeout(d.ctx, 5*time.Second)
		err := d.client.sendPayload(ctx, job.payload, job.id)
		cancel()
		if err == nil || !isRetryable(err) || attempt == 2 {
			return err
		}
		timer := time.NewTimer(d.retryDelay * time.Duration(1<<attempt))
		select {
		case <-d.ctx.Done():
			timer.Stop()
			return d.ctx.Err()
		case <-timer.C:
		}
	}
	return nil
}

func (d *Dispatcher) Close(ctx context.Context) error {
	d.mu.Lock()
	if !d.closed {
		d.closed = true
		close(d.jobs)
	}
	d.mu.Unlock()
	select {
	case <-d.done:
		d.cancel()
		return nil
	case <-ctx.Done():
		d.cancel()
		return ctx.Err()
	}
}
