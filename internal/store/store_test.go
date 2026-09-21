package store_test

import (
	"testing"

	"github.com/leohteixeira/fleet-pulse/internal/sim"
	"github.com/leohteixeira/fleet-pulse/internal/store"
)

func TestMemory_SeedSnapshotAndApply(t *testing.T) {
	t.Parallel()

	t.Run("seed 20 vehicles including offline vin", func(t *testing.T) {
		t.Parallel()

		mem := store.New()
		mem.Seed(rosterFromSim(sim.NewFleet()))
		snap := mem.Snapshot()
		if len(snap.Vehicles) != sim.FleetSize {
			t.Fatalf("len(vehicles) = %d, want %d", len(snap.Vehicles), sim.FleetSize)
		}
		if !hasVIN(snap, sim.OfflineVIN) {
			t.Fatalf("snapshot missing offline vin %q", sim.OfflineVIN)
		}
		for _, v := range snap.Vehicles {
			if v.VIN == "" || v.DisplayID == "" {
				t.Fatalf("vehicle missing vin or displayId: %+v", v)
			}
		}
	})

	t.Run("snapshot includes centro polygon", func(t *testing.T) {
		t.Parallel()

		mem := store.New()
		mem.Seed(rosterFromSim(sim.NewFleet()))
		got := mem.Snapshot().Polygon
		if got.South != -23.585 || got.North != -23.525 || got.West != -46.685 || got.East != -46.60 {
			t.Fatalf("polygon = %+v, want south=-23.585 north=-23.525 west=-46.685 east=-46.60", got)
		}
	})

	t.Run("has reports seeded vins", func(t *testing.T) {
		t.Parallel()

		mem := store.New()
		mem.Seed(rosterFromSim(sim.NewFleet()))
		if !mem.Has(sim.OfflineVIN) {
			t.Fatalf("Has(%q) = false, want true", sim.OfflineVIN)
		}
		if mem.Has("UNKNOWN") {
			t.Fatal("Has(UNKNOWN) = true, want false")
		}
		if mem.Has("") {
			t.Fatal("Has(\"\") = true, want false")
		}
	})

	t.Run("apply updates lat and lng", func(t *testing.T) {
		t.Parallel()

		fleet := sim.NewFleet()
		mem := store.New()
		mem.Seed(rosterFromSim(fleet))
		vin := fleet[0].VIN
		seededID := fleet[0].DisplayID
		if _, crossed := mem.Apply(store.Vehicle{
			VIN:   vin,
			Lat:   -23.54,
			Lng:   -46.62,
			Plate: "BAL0A01",
			Model: "Fiat Argo",
		}); crossed {
			t.Fatal("inside update reported an area exit")
		}

		got := vehicleByVIN(mem.Snapshot(), vin)
		if got.Lat != -23.54 || got.Lng != -46.62 {
			t.Fatalf("last-known = lat=%v lng=%v, want -23.54,-46.62", got.Lat, got.Lng)
		}
		if got.DisplayID != seededID {
			t.Fatalf("displayId = %q, want seeded %q", got.DisplayID, seededID)
		}
	})
}

func TestMemory_AreaExit(t *testing.T) {
	t.Parallel()

	t.Run("inside to west of centro emits exit", func(t *testing.T) {
		t.Parallel()

		mem := store.New()
		mem.Seed([]store.Vehicle{{
			VIN:       "FPULSESAO00000002",
			DisplayID: "V02",
			Lat:       -23.55,
			Lng:       -46.63,
		}})
		exit, crossed := mem.Apply(store.Vehicle{
			VIN:       "FPULSESAO00000002",
			DisplayID: "V02",
			Lat:       -23.55,
			Lng:       -46.686,
		})
		if !crossed {
			t.Fatal("inside→outside Apply did not report a crossing")
		}
		if exit.VIN != "FPULSESAO00000002" || exit.Lng != -46.686 || exit.DisplayID != "V02" {
			t.Fatalf("exit = %+v, want vin V02 west of Centro", exit)
		}
	})

	t.Run("already outside is not an exit", func(t *testing.T) {
		t.Parallel()

		mem := store.New()
		_, first := mem.Apply(store.Vehicle{
			VIN: "FPULSESAO00000003",
			Lat: -23.55,
			Lng: -46.70,
		})
		if first {
			t.Fatal("first point already outside reported an exit")
		}
		_, again := mem.Apply(store.Vehicle{
			VIN: "FPULSESAO00000003",
			Lat: -23.55,
			Lng: -46.71,
		})
		if again {
			t.Fatal("still-outside Apply reported an exit")
		}
	})
}

func rosterFromSim(fleet []sim.Vehicle) []store.Vehicle {
	out := make([]store.Vehicle, 0, len(fleet))
	for _, v := range fleet {
		out = append(out, store.Vehicle{
			VIN:       v.VIN,
			DisplayID: v.DisplayID,
		})
	}
	return out
}

func hasVIN(snap store.Snapshot, vin string) bool {
	return vehicleByVIN(snap, vin).VIN == vin
}

func vehicleByVIN(snap store.Snapshot, vin string) store.Vehicle {
	for _, v := range snap.Vehicles {
		if v.VIN == vin {
			return v
		}
	}
	return store.Vehicle{}
}
