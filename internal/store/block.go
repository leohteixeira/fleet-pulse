package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/leohteixeira/fleet-pulse/internal/block"
	"github.com/leohteixeira/fleet-pulse/internal/outbox"
	"github.com/leohteixeira/fleet-pulse/internal/store/queries"
)

const outboxFlushLimit int32 = 64

// InTx runs fn inside one transaction. Persist failures roll back command,
// audit, and outbox together.
func (p *Postgres) InTx(ctx context.Context, fn func(*queries.Queries) error) error {
	if p.pool == nil {
		return errPoolClosed()
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(context.Background())

	if err := fn(p.q.WithTx(tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// Persist writes command, audit, and optional outbox in one transaction.
func (p *Postgres) Persist(ctx context.Context, w block.PersistWrite) error {
	return p.InTx(ctx, func(q *queries.Queries) error {
		if w.Insert {
			if err := q.InsertCommand(ctx, queries.InsertCommandParams{
				ID:            w.Command.ID,
				Vin:           w.Command.VIN,
				Fleet:         block.Fleet,
				Action:        w.Command.Action,
				State:         w.Command.State,
				CorrelationID: w.Command.CorrelationID,
				SentAt:        optionalTimestamptz(w.SentAt),
			}); err != nil {
				return fmt.Errorf("insert command: %w", err)
			}
		} else {
			if err := q.UpdateCommandState(ctx, queries.UpdateCommandStateParams{
				ID:     w.Command.ID,
				State:  w.Command.State,
				SentAt: optionalTimestamptz(w.SentAt),
			}); err != nil {
				return fmt.Errorf("update command: %w", err)
			}
		}
		if err := q.InsertAuditLog(ctx, queries.InsertAuditLogParams{
			Action:  w.Audit.Action,
			Payload: w.Audit.Payload,
		}); err != nil {
			return fmt.Errorf("insert audit: %w", err)
		}
		if w.Outbox != nil {
			if err := q.InsertOutbox(ctx, queries.InsertOutboxParams{
				Topic:   w.Outbox.Topic,
				Payload: w.Outbox.Payload,
			}); err != nil {
				return fmt.Errorf("insert outbox: %w", err)
			}
		}
		return nil
	})
}

// GetCommand returns a leasing (or any) command row by id.
func (p *Postgres) GetCommand(ctx context.Context, id string) (block.Record, bool, error) {
	row, err := p.q.GetCommand(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return block.Record{}, false, nil
		}
		return block.Record{}, false, fmt.Errorf("get command: %w", err)
	}
	return recordFromRow(row), true, nil
}

// ListActive returns in-flight leasing commands.
func (p *Postgres) ListActive(ctx context.Context) ([]block.Record, error) {
	rows, err := p.q.ListActiveLeasingCommands(ctx)
	if err != nil {
		return nil, fmt.Errorf("list active commands: %w", err)
	}
	out := make([]block.Record, 0, len(rows))
	for _, row := range rows {
		out = append(out, recordFromRow(row))
	}
	return out, nil
}

// ListLatestAcked returns the latest ACKED command per leasing VIN.
func (p *Postgres) ListLatestAcked(ctx context.Context) ([]block.Record, error) {
	rows, err := p.q.ListLatestAckedLeasing(ctx)
	if err != nil {
		return nil, fmt.Errorf("list acked commands: %w", err)
	}
	out := make([]block.Record, 0, len(rows))
	for _, row := range rows {
		out = append(out, recordFromRow(row))
	}
	return out, nil
}

// HasVIN reports whether vin is a financed leasing vehicle.
func (p *Postgres) HasVIN(ctx context.Context, vin string) (bool, error) {
	ok, err := p.q.HasLeasingVehicle(ctx, vin)
	if err != nil {
		return false, fmt.Errorf("has leasing vehicle: %w", err)
	}
	return ok, nil
}

// UpsertTelem writes leasing last-known into vehicle_state without the rental cache.
func (p *Postgres) UpsertTelem(ctx context.Context, t block.LastKnown) error {
	if err := p.q.UpsertVehicleState(ctx, queries.UpsertVehicleStateParams{
		Vin:      t.VIN,
		Lat:      t.Lat,
		Lng:      t.Lng,
		Battery:  int32(t.Battery),
		Speed:    int32(t.Speed),
		Heading:  int32(t.Heading),
		Ignition: t.Ignition,
		Locked:   t.Locked,
		Odometer: t.Odometer,
		Trip:     t.Trip,
	}); err != nil {
		return fmt.Errorf("upsert leasing telem: %w", err)
	}
	return nil
}

// ListUnsent returns unpublished outbox rows.
func (p *Postgres) ListUnsent(ctx context.Context, limit int32) ([]outbox.Row, error) {
	if limit <= 0 {
		limit = outboxFlushLimit
	}
	rows, err := p.q.ListUnsentOutbox(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("list unsent outbox: %w", err)
	}
	out := make([]outbox.Row, 0, len(rows))
	for _, row := range rows {
		out = append(out, outbox.Row{ID: row.ID, Topic: row.Topic, Payload: row.Payload})
	}
	return out, nil
}

// MarkSent records that an outbox row was published.
func (p *Postgres) MarkSent(ctx context.Context, id int64, sentAt time.Time) error {
	if err := p.q.MarkOutboxSent(ctx, queries.MarkOutboxSentParams{
		ID:     id,
		SentAt: timestamptzOf(sentAt),
	}); err != nil {
		return fmt.Errorf("mark outbox sent: %w", err)
	}
	return nil
}

func recordFromRow(row queries.Command) block.Record {
	return block.Record{
		ID:            row.ID,
		VIN:           row.Vin,
		Action:        row.Action,
		State:         row.State,
		CorrelationID: row.CorrelationID,
		CreatedAt:     timeOfTimestamptz(row.CreatedAt),
		SentAt:        timeOfTimestamptz(row.SentAt),
		UpdatedAt:     timeOfTimestamptz(row.UpdatedAt),
	}
}

func timeOfTimestamptz(t pgtype.Timestamptz) time.Time {
	if !t.Valid {
		return time.Time{}
	}
	return t.Time
}

func optionalTimestamptz(t time.Time) pgtype.Timestamptz {
	if t.IsZero() {
		return pgtype.Timestamptz{}
	}
	return timestamptzOf(t)
}
