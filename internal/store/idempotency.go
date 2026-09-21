package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/leohteixeira/fleet-pulse/internal/store/queries"
)

// LookupIdempotency returns the first write stored for contract+action+key.
func (p *Postgres) LookupIdempotency(ctx context.Context, contractID, action, key string) (Replay, bool, error) {
	uid, err := parseUUID(contractID)
	if err != nil {
		return Replay{}, false, err
	}
	row, err := p.q.GetIdempotency(ctx, queries.GetIdempotencyParams{
		ContractID: uid,
		Action:     action,
		Key:        key,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Replay{}, false, nil
		}
		return Replay{}, false, fmt.Errorf("lookup idempotency: %w", err)
	}
	return Replay{
		ContractID: uuidString(row.ContractID),
		Action:     row.Action,
		Key:        row.Key,
		ResourceID: row.ResourceID,
		Status:     int(row.StatusCode),
		Body:       row.Body,
	}, true, nil
}

// StoreIdempotency records the first successful write for later replay.
func (p *Postgres) StoreIdempotency(ctx context.Context, rec Replay) error {
	uid, err := parseUUID(rec.ContractID)
	if err != nil {
		return err
	}
	body := rec.Body
	if body == nil {
		body = []byte("{}")
	}
	if err := p.q.InsertIdempotency(ctx, queries.InsertIdempotencyParams{
		ContractID: uid,
		Action:     rec.Action,
		Key:        rec.Key,
		ResourceID: rec.ResourceID,
		StatusCode: int32(rec.Status),
		Body:       body,
	}); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return errIdempotencyExists
		}
		return fmt.Errorf("store idempotency: %w", err)
	}
	return nil
}

var errIdempotencyExists = errors.New("store: idempotency key exists")
