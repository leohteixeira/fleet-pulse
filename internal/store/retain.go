package store

import (
	"context"
	"fmt"
	"time"
)

const (
	// RetentionWindow is how long finished commands and audit rows are kept.
	RetentionWindow = 7 * 24 * time.Hour
	retentionTick   = time.Hour
)

// Purge deletes finished commands and audit rows older than olderThan.
// In-flight command states (PENDING, SENT, REQUESTED, ARMED) are kept.
func (p *Postgres) Purge(ctx context.Context, olderThan time.Time) error {
	if p == nil || p.q == nil {
		return errPoolClosed()
	}
	cut := timestamptzOf(olderThan)
	commands, err := p.q.DeleteFinishedCommandsOlderThan(ctx, cut)
	if err != nil {
		return fmt.Errorf("purge commands: %w", err)
	}
	audits, err := p.q.DeleteAuditOlderThan(ctx, cut)
	if err != nil {
		return fmt.Errorf("purge audit: %w", err)
	}
	if p.log != nil && (commands > 0 || audits > 0) {
		p.log.Info("retention purged", "commands", commands, "audit", audits)
	}
	return nil
}

// RunRetention purges on boot and then once per hour until ctx is cancelled.
func (p *Postgres) RunRetention(ctx context.Context) {
	if p == nil {
		return
	}
	purge := func() {
		if err := p.Purge(ctx, time.Now().Add(-RetentionWindow)); err != nil {
			if ctx.Err() != nil {
				return
			}
			if p.log != nil {
				p.log.Error("retention purge failed", "err", err)
			}
		}
	}
	purge()
	ticker := time.NewTicker(retentionTick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			purge()
		}
	}
}
