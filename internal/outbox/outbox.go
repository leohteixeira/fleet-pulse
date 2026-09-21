// Package outbox relays committed leasing MQTT payloads and marks them sent.
package outbox

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"
)

const (
	pollInterval = 100 * time.Millisecond
	flushLimit   = int32(64)
	flushTimeout = 2 * time.Second
)

// Row is one unpublished outbox payload.
type Row struct {
	ID      int64
	Topic   string
	Payload []byte
}

// Store is the outbox persist port declared by the relay.
type Store interface {
	ListUnsent(ctx context.Context, limit int32) ([]Row, error)
	MarkSent(ctx context.Context, id int64, sentAt time.Time) error
}

// Publisher is the MQTT publish port declared by the relay.
type Publisher interface {
	Publish(ctx context.Context, topic string, payload []byte) error
}

// Relay polls unsent rows, publishes them, and sets sent_at.
type Relay struct {
	mu    sync.Mutex
	store Store
	pub   Publisher
	log   *slog.Logger
	now   func() time.Time
}

// New wires persist and a consumer-owned publisher.
func New(store Store, pub Publisher, log *slog.Logger) *Relay {
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Relay{
		store: store,
		pub:   pub,
		log:   log,
		now:   time.Now,
	}
}

// Flush publishes every unsent row and marks it sent. Publish happens only
// after the domain write has committed.
func (r *Relay) Flush(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.store == nil || r.pub == nil {
		return nil
	}
	for {
		rows, err := r.store.ListUnsent(ctx, flushLimit)
		if err != nil {
			return fmt.Errorf("list unsent: %w", err)
		}
		if len(rows) == 0 {
			return nil
		}
		for _, row := range rows {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := r.pub.Publish(ctx, row.Topic, row.Payload); err != nil {
				return fmt.Errorf("publish %s: %w", row.Topic, err)
			}
			if err := r.store.MarkSent(ctx, row.ID, r.now()); err != nil {
				return fmt.Errorf("mark sent %d: %w", row.ID, err)
			}
		}
	}
}

// Run polls until ctx is cancelled, then flushes once more.
func (r *Relay) Run(ctx context.Context) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			flushCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), flushTimeout)
			defer cancel()
			if err := r.Flush(flushCtx); err != nil {
				r.log.Error("outbox shutdown flush failed", "err", err)
			}
			return
		case <-ticker.C:
			if err := r.Flush(ctx); err != nil {
				r.log.Error("outbox flush failed", "err", err)
			}
		}
	}
}
