package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/leohteixeira/fleet-pulse/internal/httpapi"
	"github.com/leohteixeira/fleet-pulse/internal/ingest"
	"github.com/leohteixeira/fleet-pulse/internal/store"
)

func TestReadySub_SignalsAfterN(t *testing.T) {
	t.Parallel()

	ready := newReadySub(nopSub{}, 2)
	if err := ready.Subscribe(t.Context(), "fleet/+/telemetry", nil); err != nil {
		t.Fatalf("first Subscribe() error = %v", err)
	}
	select {
	case err := <-ready.done:
		t.Fatalf("done signaled after first subscribe: %v", err)
	default:
	}

	if err := ready.Subscribe(t.Context(), "fleet/+/ack", nil); err != nil {
		t.Fatalf("second Subscribe() error = %v", err)
	}
	select {
	case err := <-ready.done:
		if err != nil {
			t.Fatalf("done = %v, want nil", err)
		}
	default:
		t.Fatal("done not signaled after second subscribe")
	}
}

type nopSub struct{}

func (nopSub) Subscribe(context.Context, string, ingest.MessageHandler) error {
	return nil
}

func (nopSub) Unsubscribe(context.Context, string) error { return nil }

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

func TestTelemetrySink_AreaExit(t *testing.T) {
	t.Parallel()

	mem := store.New()
	mem.Seed([]store.Vehicle{{
		VIN:       "FPULSESAO00000002",
		DisplayID: "V02",
		Lat:       -23.55,
		Lng:       -46.63,
	}})
	hub := httpapi.NewHub()
	events, unsubscribe := hub.Subscribe()
	t.Cleanup(unsubscribe)

	telemetrySink{mem: mem, hub: hub}.Apply(ingest.Telemetry{
		VIN:       "FPULSESAO00000002",
		DisplayID: "V02",
		Lat:       -23.55,
		Lng:       -46.686,
	})

	gotTel := false
	gotExit := false
	for range 2 {
		select {
		case ev := <-events:
			switch ev.Name {
			case "telemetry":
				gotTel = true
			case "area-exit":
				gotExit = true
				var body struct {
					VIN       string  `json:"vin"`
					DisplayID string  `json:"displayId"`
					Lat       float64 `json:"lat"`
					Lng       float64 `json:"lng"`
				}
				if err := json.Unmarshal(ev.Data, &body); err != nil {
					t.Fatalf("decode area-exit: %v", err)
				}
				if body.VIN != "FPULSESAO00000002" || body.DisplayID != "V02" || body.Lng != -46.686 {
					t.Fatalf("area-exit = %+v", body)
				}
			default:
				t.Fatalf("unexpected event %q", ev.Name)
			}
		default:
			t.Fatal("expected telemetry and area-exit events")
		}
	}
	if !gotTel || !gotExit {
		t.Fatalf("telemetry=%v area-exit=%v, want both", gotTel, gotExit)
	}

	telemetrySink{mem: mem, hub: hub}.Apply(ingest.Telemetry{
		VIN: "FPULSESAO00000003",
		Lat: -23.55,
		Lng: -46.70,
	})
	select {
	case ev := <-events:
		if ev.Name == "area-exit" {
			t.Fatal("first point already outside emitted area-exit")
		}
	default:
		t.Fatal("expected telemetry for the already-outside VIN")
	}
}
