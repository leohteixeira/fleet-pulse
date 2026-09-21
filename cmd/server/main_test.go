package main

import (
	"encoding/json"
	"testing"

	"github.com/leohteixeira/fleet-pulse/internal/httpapi"
	"github.com/leohteixeira/fleet-pulse/internal/ingest"
	"github.com/leohteixeira/fleet-pulse/internal/store"
)

func TestTelemetrySink_Apply(t *testing.T) {
	t.Parallel()

	mem := store.New()
	hub := httpapi.NewHub()
	events, unsubscribe := hub.Subscribe()
	t.Cleanup(unsubscribe)

	const vin = "FPULSESAO00000001"
	telemetrySink{mem: mem, hub: hub}.Apply(ingest.Telemetry{
		VIN: vin,
		Lat: -23.54,
		Lng: -46.62,
	})

	got := mem.Snapshot()
	if len(got.Vehicles) != 1 {
		t.Fatalf("len(vehicles) = %d, want 1", len(got.Vehicles))
	}
	if got.Vehicles[0].VIN != vin || got.Vehicles[0].Lat != -23.54 || got.Vehicles[0].Lng != -46.62 {
		t.Fatalf("snapshot = %+v, want vin=%s lat=-23.54 lng=-46.62", got.Vehicles[0], vin)
	}

	select {
	case ev := <-events:
		if ev.Name != "telemetry" {
			t.Fatalf("event name = %q, want telemetry", ev.Name)
		}
		var body struct {
			VIN string `json:"vin"`
		}
		if err := json.Unmarshal(ev.Data, &body); err != nil {
			t.Fatalf("decode event: %v", err)
		}
		if body.VIN != vin {
			t.Fatalf("event vin = %q, want %s", body.VIN, vin)
		}
	default:
		t.Fatal("expected telemetry event")
	}
}
