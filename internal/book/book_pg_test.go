package book

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/leohteixeira/fleet-pulse/internal/clock"
	"github.com/leohteixeira/fleet-pulse/internal/sim"
	"github.com/leohteixeira/fleet-pulse/internal/store"
)

func TestBook_SeedLeavesRentalSnapshotAlone(t *testing.T) {
	dsn := startPostgres(t)
	ctx := t.Context()
	pg, err := store.Open(ctx, dsn, nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(pg.Close)

	pg.Seed(rosterFromSim(sim.NewFleet()))
	origin := clock.Origin
	clk := clock.Fixed(clock.ParseRate(clock.Rate4h), origin, origin)
	t.Setenv("SIM_SEED", "1")
	b := New(pg, clk, nil)
	if err := b.Seed(ctx); err != nil {
		t.Fatalf("seed book: %v", err)
	}

	snap := pg.Snapshot()
	if len(snap.Vehicles) != sim.FleetSize {
		t.Fatalf("rental snapshot = %d, want %d", len(snap.Vehicles), sim.FleetSize)
	}
	for _, v := range snap.Vehicles {
		if strings.HasPrefix(v.VIN, "FPULSELSG") {
			t.Fatalf("snapshot contains leasing vin %q", v.VIN)
		}
	}

	list, err := b.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	assertBookShape(t, list)

	n, err := pg.CountLeasingVehicles(ctx)
	if err != nil {
		t.Fatalf("count leasing: %v", err)
	}
	if n != sim.LeasingSize {
		t.Fatalf("leasing vehicles = %d, want %d", n, sim.LeasingSize)
	}
}

func TestBook_TickPontualPersistsOnPostgres(t *testing.T) {
	dsn := startPostgres(t)
	ctx := t.Context()
	pg, err := store.Open(ctx, dsn, nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(pg.Close)

	origin := clock.Origin
	clk := clock.Fixed(clock.ParseRate(clock.Rate4h), origin, origin)
	if err := pg.SeedBook(ctx, []store.LeasingVehicle{{
		VIN:       "FPULSELSG00000001",
		DisplayID: "L01",
		Plate:     "LCS0B01",
		Model:     "Fiat Argo",
	}}, []store.SeedContract{{
		VIN:          "FPULSELSG00000001",
		CustomerName: "Ana Costa",
		Profile:      ProfilePontual,
		Amount:       defaultInstallment,
		StartedOn:    origin,
		Installments: []store.SeedInstallment{{
			DueOn:  origin,
			Amount: defaultInstallment,
		}},
	}}); err != nil {
		t.Fatalf("seed book: %v", err)
	}

	b := New(pg, clk, nil)
	if err := b.Tick(ctx); err != nil {
		t.Fatalf("tick: %v", err)
	}
	list, err := b.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].DaysLate != 0 || list[0].OverdueBand != BandEmDia {
		t.Fatalf("contract = %+v, want daysLate=0 em_dia", list)
	}
	payments, err := pg.ListActivePayments(ctx)
	if err != nil {
		t.Fatalf("list payments: %v", err)
	}
	if len(payments) != 1 || payments[0].Source != store.PaymentSourceSystem {
		t.Fatalf("payments = %+v, want 1 system payment", payments)
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

func rosterFromSim(fleet []sim.Vehicle) []store.Vehicle {
	out := make([]store.Vehicle, 0, len(fleet))
	for _, v := range fleet {
		out = append(out, store.Vehicle{
			VIN:       v.VIN,
			DisplayID: v.DisplayID,
			Plate:     v.Plate,
			Model:     v.Model,
		})
	}
	return out
}
