package httpapi

import (
	"context"
	"errors"
	"sync"

	"github.com/leohteixeira/fleet-pulse/internal/store"
)

// Replay is the first successful write for contract+action+Idempotency-Key.
type Replay = store.Replay

// Idempotency looks up and stores write replays. Declared by HTTP.
type Idempotency interface {
	Lookup(ctx context.Context, contractID, action, key string) (Replay, bool, error)
	Store(ctx context.Context, rec Replay) error
}

type memKeys struct {
	mu   sync.Mutex
	rows map[string]Replay
}

func newMemKeys() *memKeys {
	return &memKeys{rows: make(map[string]Replay)}
}

// NewMemoryKeys is an in-process idempotency store for tests.
func NewMemoryKeys() Idempotency {
	return newMemKeys()
}

func idempotencySlot(contractID, action, key string) string {
	return contractID + "\x00" + action + "\x00" + key
}

func (m *memKeys) Lookup(_ context.Context, contractID, action, key string) (Replay, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.rows[idempotencySlot(contractID, action, key)]
	return rec, ok, nil
}

func (m *memKeys) Store(_ context.Context, rec Replay) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.rows == nil {
		m.rows = make(map[string]Replay)
	}
	slot := idempotencySlot(rec.ContractID, rec.Action, rec.Key)
	if _, ok := m.rows[slot]; ok {
		return errIdempotencyExists
	}
	m.rows[slot] = rec
	return nil
}

var errIdempotencyExists = errors.New("httpapi: idempotency key exists")

type storeKeys struct {
	pg interface {
		LookupIdempotency(ctx context.Context, contractID, action, key string) (store.Replay, bool, error)
		StoreIdempotency(ctx context.Context, rec store.Replay) error
	}
}

// KeysFromStore adapts Postgres idempotency methods to the HTTP port.
func KeysFromStore(pg interface {
	LookupIdempotency(ctx context.Context, contractID, action, key string) (store.Replay, bool, error)
	StoreIdempotency(ctx context.Context, rec store.Replay) error
}) Idempotency {
	if pg == nil {
		return nil
	}
	return storeKeys{pg: pg}
}

func (s storeKeys) Lookup(ctx context.Context, contractID, action, key string) (Replay, bool, error) {
	return s.pg.LookupIdempotency(ctx, contractID, action, key)
}

func (s storeKeys) Store(ctx context.Context, rec Replay) error {
	return s.pg.StoreIdempotency(ctx, rec)
}
