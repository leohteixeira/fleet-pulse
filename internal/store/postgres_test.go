package store_test

import (
	"context"
	"os/exec"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

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
