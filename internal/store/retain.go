package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/leohteixeira/fleet-pulse/internal/store/queries"
)

const (
	// RetentionWindow is how long finished commands, audit rows and ended
	// contracts are kept.
	RetentionWindow = 7 * 24 * time.Hour
	retentionTick   = time.Hour

	// contractPurgeBatch bounds one delete transaction. The book rotates
	// thousands of contracts per day at the default calendar rate, so a
	// backlog is drained in short transactions instead of one long one.
	contractPurgeBatch int32 = 1000
)

// Purge deletes finished commands, audit rows and ended contracts older than
// olderThan. In-flight command states (PENDING, SENT, REQUESTED, ARMED) and
// active contracts are kept.
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
	contracts, err := p.purgeEndedContracts(ctx, cut)
	if err != nil {
		return err
	}
	customers, err := p.q.DeleteCustomersWithoutContractOlderThan(ctx, cut)
	if err != nil {
		return fmt.Errorf("purge customers: %w", err)
	}
	if p.log != nil && (commands > 0 || audits > 0 || contracts > 0 || customers > 0) {
		p.log.Info("retention purged",
			"commands", commands,
			"audit", audits,
			"contracts", contracts,
			"customers", customers,
		)
	}
	return nil
}

// purgeEndedContracts deletes ended contracts created before cut, with their
// installments, payments and idempotency keys, one batch per transaction.
// A contract that still has audit rows stays until those rows age out, so
// the audit trail is never cut short.
func (p *Postgres) purgeEndedContracts(ctx context.Context, cut pgtype.Timestamptz) (int64, error) {
	var total int64
	for {
		var deleted int64
		err := p.InTx(ctx, func(q *queries.Queries) error {
			ids, err := q.ListEndedContractsOlderThan(ctx, queries.ListEndedContractsOlderThanParams{
				CreatedAt: cut,
				Limit:     contractPurgeBatch,
			})
			if err != nil {
				return fmt.Errorf("list ended contracts: %w", err)
			}
			if len(ids) == 0 {
				return nil
			}
			if err := q.DeleteIdempotencyByContracts(ctx, ids); err != nil {
				return fmt.Errorf("delete idempotency keys: %w", err)
			}
			if err := q.DeletePaymentsByContracts(ctx, ids); err != nil {
				return fmt.Errorf("delete payments: %w", err)
			}
			if err := q.DeleteInstallmentsByContracts(ctx, ids); err != nil {
				return fmt.Errorf("delete installments: %w", err)
			}
			deleted, err = q.DeleteContracts(ctx, ids)
			if err != nil {
				return fmt.Errorf("delete contracts: %w", err)
			}
			return nil
		})
		if err != nil {
			return total, fmt.Errorf("purge contracts: %w", err)
		}
		total += deleted
		if deleted < int64(contractPurgeBatch) {
			return total, nil
		}
	}
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
