package store_test

import (
	"context"
	"os/exec"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/leohteixeira/fleet-pulse/internal/block"
	"github.com/leohteixeira/fleet-pulse/internal/sim"
	"github.com/leohteixeira/fleet-pulse/internal/store"
)

func TestOpen_RejectsMissingAndInvalidDSN(t *testing.T) {
	t.Parallel()

	t.Run("missing dsn", func(t *testing.T) {
		t.Parallel()
		_, err := store.Open(t.Context(), "", nil)
		if err == nil {
			t.Fatal("empty dsn: expected error")
		}
	})

	t.Run("invalid dsn", func(t *testing.T) {
		t.Parallel()
		_, err := store.Open(t.Context(), "://not-a-dsn", nil)
		if err == nil {
			t.Fatal("invalid dsn: expected error")
		}
	})
}

func TestPostgres_PersistReloadAndMigrations(t *testing.T) {
	dsn := startPostgres(t)
	ctx := t.Context()

	pg, err := store.Open(ctx, dsn, nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	gotTables := publicTables(t, dsn)
	wantTables := []string{
		"audit_log",
		"commands",
		"contracts",
		"customers",
		"installments",
		"outbox",
		"payments",
		"routes",
		"vehicle_state",
		"vehicles",
	}
	if !slices.Equal(gotTables, wantTables) {
		t.Fatalf("tables = %v, want %v", gotTables, wantTables)
	}

	fleet := rosterFromSim(sim.NewFleet())
	pg.Seed(fleet)
	if !pg.Has(sim.OfflineVIN) {
		t.Fatalf("Has(%q) = false, want true", sim.OfflineVIN)
	}
	if pg.Has("UNKNOWN") {
		t.Fatal("Has(UNKNOWN) = true, want false")
	}
	if pg.Has("") {
		t.Fatal("Has(\"\") = true, want false")
	}
	snap := pg.Snapshot()
	if snap.Polygon != store.Centro {
		t.Fatalf("polygon = %+v, want Centro", snap.Polygon)
	}
	if len(snap.Vehicles) != sim.FleetSize {
		t.Fatalf("len(vehicles) = %d, want %d", len(snap.Vehicles), sim.FleetSize)
	}
	if !hasVIN(snap, sim.OfflineVIN) {
		t.Fatalf("snapshot missing offline vin %q", sim.OfflineVIN)
	}

	vin := fleet[0].VIN
	const lat, lng = -23.541, -46.621
	pg.Apply(store.Vehicle{
		VIN:   vin,
		Lat:   lat,
		Lng:   lng,
		Plate: "TST0A01",
		Model: "Fiat Argo",
	})
	if err := pg.Ping(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}
	pg.Close()
	if err := pg.Ping(ctx); err == nil {
		t.Fatal("Ping after Close: expected error")
	}

	reloaded, err := store.Open(ctx, dsn, nil)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(reloaded.Close)
	reloaded.Seed(fleet)
	got := vehicleByVIN(reloaded.Snapshot(), vin)
	if got.Lat != lat || got.Lng != lng {
		t.Fatalf("reloaded last-known = lat=%v lng=%v, want %v,%v", got.Lat, got.Lng, lat, lng)
	}

	again, err := store.Open(ctx, dsn, nil)
	if err != nil {
		t.Fatalf("migration no-op reopen: %v", err)
	}
	again.Close()

	live, err := store.Open(ctx, dsn, nil)
	if err != nil {
		t.Fatalf("open for persist-failure apply: %v", err)
	}
	live.Seed(fleet)
	live.Close()
	const failLat, failLng = -23.542, -46.622
	live.Apply(store.Vehicle{
		VIN: vin,
		Lat: failLat,
		Lng: failLng,
	})
	kept := vehicleByVIN(live.Snapshot(), vin)
	if kept.Lat != failLat || kept.Lng != failLng {
		t.Fatalf("snapshot after persist failure = lat=%v lng=%v, want %v,%v", kept.Lat, kept.Lng, failLat, failLng)
	}
}

func startPostgres(t *testing.T) string {
	t.Helper()
	if err := exec.Command("docker", "info").Run(); err != nil {
		t.Skipf("docker unavailable: %v", err)
	}

	ctx := t.Context()
	container, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("fleetpulse"),
		postgres.WithUsername("fleetpulse"),
		postgres.WithPassword("fleetpulse"),
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}
	t.Cleanup(func() {
		_ = container.Terminate(context.Background())
	})

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("postgres dsn: %v", err)
	}
	return dsn
}

func publicTables(t *testing.T, dsn string) []string {
	t.Helper()
	ctx := t.Context()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("open inspect pool: %v", err)
	}
	defer pool.Close()

	rows, err := pool.Query(ctx, `
		SELECT tablename
		FROM pg_tables
		WHERE schemaname = 'public'
		  AND tablename <> 'goose_db_version'
		ORDER BY 1
	`)
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	defer rows.Close()

	names := make([]string, 0, 10)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan table: %v", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate tables: %v", err)
	}
	return names
}

func TestPostgres_PersistCommandAuditOutboxOneTx(t *testing.T) {
	dsn := startPostgres(t)
	ctx := t.Context()
	pg, err := store.Open(ctx, dsn, nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(pg.Close)

	if err := pg.SeedBook(ctx, []store.LeasingVehicle{{
		VIN:       "FPULSELSG00000001",
		DisplayID: "L01",
		Plate:     "LCS0B01",
		Model:     "Fiat Argo",
	}}, nil); err != nil {
		t.Fatalf("seed leasing vin: %v", err)
	}

	rec := block.Record{
		ID:            "cmd-lease-1",
		VIN:           "FPULSELSG00000001",
		Action:        block.ActionBlock,
		State:         block.StateSent,
		CorrelationID: "corr-1",
		SentAt:        time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC),
	}
	if err := pg.Persist(ctx, block.PersistWrite{
		Command: rec,
		Insert:  true,
		SentAt:  rec.SentAt,
		Audit:   block.AuditEntry{Action: "block.SENT", Payload: []byte(`{"origin":"SISTEMA"}`)},
		Outbox:  &block.OutboxEntry{Topic: block.Topic(rec.VIN), Payload: []byte(`{"id":"cmd-lease-1"}`)},
	}); err != nil {
		t.Fatalf("persist: %v", err)
	}

	got, ok, err := pg.GetCommand(ctx, rec.ID)
	if err != nil || !ok {
		t.Fatalf("GetCommand() = %v ok=%v err=%v", got, ok, err)
	}
	if got.State != block.StateSent || got.VIN != rec.VIN {
		t.Fatalf("stored command = %+v", got)
	}
	rows, err := pg.ListUnsent(ctx, 10)
	if err != nil {
		t.Fatalf("ListUnsent: %v", err)
	}
	if len(rows) != 1 || rows[0].Topic != block.Topic(rec.VIN) {
		t.Fatalf("unsent = %+v, want one leasing topic", rows)
	}

	if err := pg.Persist(ctx, block.PersistWrite{
		Command: block.Record{ID: "bad", VIN: "MISSING", Action: block.ActionBlock, State: block.StateRequested},
		Insert:  true,
		Audit:   block.AuditEntry{Action: "block.REQUESTED", Payload: []byte(`{}`)},
		Outbox:  &block.OutboxEntry{Topic: "leasing/MISSING/commands", Payload: []byte(`{}`)},
	}); err == nil {
		t.Fatal("persist unknown vin: expected error")
	}
	rows, err = pg.ListUnsent(ctx, 10)
	if err != nil {
		t.Fatalf("ListUnsent after rollback: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("outbox after failed persist = %d, want 1 (rollback)", len(rows))
	}

	snap := pg.Snapshot()
	if len(snap.Vehicles) != 0 {
		t.Fatalf("rental snapshot = %d, want 0 after leasing persist", len(snap.Vehicles))
	}

	active, err := pg.ListActive(ctx)
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(active) != 1 || active[0].ID != rec.ID || active[0].State != block.StateSent {
		t.Fatalf("ListActive = %+v, want SENT %s", active, rec.ID)
	}

	rec.State = block.StateAcked
	if err := pg.Persist(ctx, block.PersistWrite{
		Command: rec,
		Insert:  false,
		Audit:   block.AuditEntry{Action: "block.ACKED", Payload: []byte(`{"origin":"SISTEMA"}`)},
	}); err != nil {
		t.Fatalf("persist update: %v", err)
	}

	got, ok, err = pg.GetCommand(ctx, rec.ID)
	if err != nil || !ok || got.State != block.StateAcked || got.VIN != rec.VIN {
		t.Fatalf("GetCommand after update = %+v ok=%v err=%v", got, ok, err)
	}

	active, err = pg.ListActive(ctx)
	if err != nil {
		t.Fatalf("ListActive after ACKED: %v", err)
	}
	if len(active) != 0 {
		t.Fatalf("ListActive after ACKED = %+v, want empty", active)
	}

	acked, err := pg.ListLatestAcked(ctx)
	if err != nil {
		t.Fatalf("ListLatestAcked: %v", err)
	}
	if len(acked) != 1 || acked[0].ID != rec.ID || acked[0].State != block.StateAcked {
		t.Fatalf("ListLatestAcked = %+v, want ACKED %s", acked, rec.ID)
	}

	if err := pg.MarkSent(ctx, rows[0].ID, time.Date(2026, 9, 21, 12, 0, 1, 0, time.UTC)); err != nil {
		t.Fatalf("MarkSent: %v", err)
	}
	unsent, err := pg.ListUnsent(ctx, 10)
	if err != nil {
		t.Fatalf("ListUnsent after MarkSent: %v", err)
	}
	if len(unsent) != 0 {
		t.Fatalf("ListUnsent after MarkSent = %+v, want empty", unsent)
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("audit inspect pool: %v", err)
	}
	var audits int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_log`).Scan(&audits); err != nil {
		pool.Close()
		t.Fatalf("count audit_log: %v", err)
	}
	pool.Close()
	if audits < 1 {
		t.Fatalf("audit_log rows = %d, want at least 1", audits)
	}

	pg.Seed(rosterFromSim(sim.NewFleet()))
	if err := pg.UpsertTelem(ctx, block.LastKnown{
		VIN:      rec.VIN,
		Lat:      -23.55,
		Lng:      -46.63,
		Speed:    14,
		Ignition: true,
	}); err != nil {
		t.Fatalf("UpsertTelem: %v", err)
	}
	snap = pg.Snapshot()
	if len(snap.Vehicles) != sim.FleetSize {
		t.Fatalf("rental snapshot after UpsertTelem = %d, want %d", len(snap.Vehicles), sim.FleetSize)
	}
	if hasVIN(snap, rec.VIN) {
		t.Fatal("Snapshot included leasing VIN after UpsertTelem")
	}
}
