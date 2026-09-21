package store

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/leohteixeira/fleet-pulse/internal/store/queries"
	"github.com/leohteixeira/fleet-pulse/migrations"
)

const persistTimeout = 2 * time.Second

// ErrDatabaseURL is returned when Open is called without a DSN.
var ErrDatabaseURL = errors.New("store: database url is required")

// Postgres is the production fleet store: PostgreSQL write-through plus Memory cache.
type Postgres struct {
	pool  *pgxpool.Pool
	q     *queries.Queries
	cache *Memory
	life  context.Context
	log   *slog.Logger
}

// Open parses dsn, pings the pool, applies embedded goose migrations, and
// loads persisted rental rows into the cache. Seed missing VINs after Open.
// life is the process lifecycle used to derive persist timeouts; Seed and Apply
// cannot take a context without changing their caller signatures.
func Open(ctx context.Context, dsn string, log *slog.Logger) (*Postgres, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, ErrDatabaseURL
	}
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	cfg.MaxConns = 8
	cfg.MinConns = 1
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.MaxConnIdleTime = 5 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("open pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	if err := migrate(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}

	p := &Postgres{
		pool:  pool,
		q:     queries.New(pool),
		cache: New(),
		life:  ctx,
		log:   log,
	}
	if err := p.load(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return p, nil
}

func migrate(ctx context.Context, pool *pgxpool.Pool) error {
	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()

	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations.FS)
	if err != nil {
		return fmt.Errorf("migration provider: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}
	return nil
}

func (p *Postgres) load(ctx context.Context) error {
	vehicles, err := p.q.ListRentalVehicles(ctx)
	if err != nil {
		return fmt.Errorf("list rental vehicles: %w", err)
	}
	states, err := p.q.ListVehicleState(ctx)
	if err != nil {
		return fmt.Errorf("list vehicle state: %w", err)
	}
	byVIN := make(map[string]queries.ListVehicleStateRow, len(states))
	for _, s := range states {
		byVIN[s.Vin] = s
	}
	out := make([]Vehicle, 0, len(vehicles))
	for _, v := range vehicles {
		item := Vehicle{
			VIN:       v.Vin,
			DisplayID: v.DisplayID,
			Plate:     v.Plate,
			Model:     v.Model,
		}
		if s, ok := byVIN[v.Vin]; ok {
			item.Lat = s.Lat
			item.Lng = s.Lng
			item.Battery = int(s.Battery)
			item.Speed = int(s.Speed)
			item.Heading = int(s.Heading)
			item.Ignition = s.Ignition
			item.Locked = s.Locked
			item.Odometer = s.Odometer
			item.Trip = s.Trip
		}
		out = append(out, item)
	}
	p.cache.Seed(out)
	return nil
}

func (p *Postgres) persistCtx() (context.Context, context.CancelFunc) {
	parent := p.life
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(parent, persistTimeout)
}

// Seed inserts any VIN not already in the cache (loaded from Postgres) and
// write-through persists those new rental rows. Persist errors are logged.
func (p *Postgres) Seed(vehicles []Vehicle) {
	missing := make([]Vehicle, 0, len(vehicles))
	for _, v := range vehicles {
		if v.VIN == "" || p.cache.Has(v.VIN) {
			continue
		}
		missing = append(missing, v)
	}
	if len(missing) == 0 {
		return
	}
	p.cache.Seed(missing)
	if err := p.persistSeed(missing); err != nil {
		p.log.Error("persist seed failed", "count", len(missing), "err", err)
	}
}

func (p *Postgres) persistSeed(vehicles []Vehicle) error {
	ctx, cancel := p.persistCtx()
	defer cancel()

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin seed: %w", err)
	}
	defer tx.Rollback(context.Background())

	q := p.q.WithTx(tx)
	for _, v := range vehicles {
		if err := q.InsertRentalVehicle(ctx, queries.InsertRentalVehicleParams{
			Vin:       v.VIN,
			DisplayID: v.DisplayID,
			Plate:     v.Plate,
			Model:     v.Model,
		}); err != nil {
			return fmt.Errorf("insert vehicle: %w", err)
		}
		if err := q.UpsertVehicleState(ctx, stateParams(v)); err != nil {
			return fmt.Errorf("upsert vehicle state: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit seed: %w", err)
	}
	return nil
}

// Apply updates the cache first so SSE and crossing stay synchronous, then
// upserts vehicle_state. A persist error is logged and not returned.
func (p *Postgres) Apply(update Vehicle) (exit Vehicle, crossed bool) {
	exit, crossed = p.cache.Apply(update)
	if update.VIN == "" {
		return exit, crossed
	}
	cur, ok := p.cache.lookup(update.VIN)
	if !ok {
		return exit, crossed
	}
	if err := p.persistState(cur); err != nil {
		p.log.Error("persist vehicle state failed", "vin", update.VIN, "err", err)
	}
	return exit, crossed
}

func (p *Postgres) persistState(v Vehicle) error {
	ctx, cancel := p.persistCtx()
	defer cancel()
	if err := p.q.InsertRentalVehicle(ctx, queries.InsertRentalVehicleParams{
		Vin:       v.VIN,
		DisplayID: v.DisplayID,
		Plate:     v.Plate,
		Model:     v.Model,
	}); err != nil {
		return fmt.Errorf("insert vehicle: %w", err)
	}
	if err := p.q.UpsertVehicleState(ctx, stateParams(v)); err != nil {
		return fmt.Errorf("upsert vehicle state: %w", err)
	}
	return nil
}

func stateParams(v Vehicle) queries.UpsertVehicleStateParams {
	return queries.UpsertVehicleStateParams{
		Vin:      v.VIN,
		Lat:      v.Lat,
		Lng:      v.Lng,
		Battery:  int32(v.Battery),
		Speed:    int32(v.Speed),
		Heading:  int32(v.Heading),
		Ignition: v.Ignition,
		Locked:   v.Locked,
		Odometer: v.Odometer,
		Trip:     v.Trip,
	}
}

// Has reports whether vin exists in the cache.
func (p *Postgres) Has(vin string) bool {
	return p.cache.Has(vin)
}

// Snapshot copies current last-known state and the Centro polygon from the cache.
func (p *Postgres) Snapshot() Snapshot {
	return p.cache.Snapshot()
}

// Ping reports whether the pool is reachable. Used by the HTTP ready port.
func (p *Postgres) Ping(ctx context.Context) error {
	if p.pool == nil {
		return errors.New("store: pool is closed")
	}
	return p.pool.Ping(ctx)
}

// Close releases the pool. Safe to call more than once.
func (p *Postgres) Close() {
	if p.pool != nil {
		p.pool.Close()
	}
}
