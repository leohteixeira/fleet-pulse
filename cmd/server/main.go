// Command server starts the embedded MQTT broker, vehicle simulator, telemetry ingest, and HTTP API.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/leohteixeira/fleet-pulse/internal/broker"
	"github.com/leohteixeira/fleet-pulse/internal/httpapi"
	"github.com/leohteixeira/fleet-pulse/internal/ingest"
	"github.com/leohteixeira/fleet-pulse/internal/sim"
	"github.com/leohteixeira/fleet-pulse/internal/store"
)

const defaultBindAddr = "0.0.0.0:1883"

var (
	_ ingest.Subscriber = (*broker.Broker)(nil)
	_ ingest.Sink       = telemetrySink{}
	_ httpapi.Store     = (*store.Memory)(nil)
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(log)

	if err := run(log); err != nil {
		log.Error("server failed", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	b := broker.New(defaultBindAddr, log)
	if err := b.Start(ctx); err != nil {
		return fmt.Errorf("start broker: %w", err)
	}

	mem := store.New()
	mem.Seed(rosterFromSim(sim.NewFleet()))
	hub := httpapi.NewHub()
	sink := telemetrySink{mem: mem, hub: hub}

	var wg sync.WaitGroup
	errCh := make(chan error, 3)
	subscribed := make(chan error, 1)

	wg.Go(func() {
		if err := ingest.Run(
			ctx,
			readySub{Subscriber: b, ready: subscribed},
			sink,
			log,
		); err != nil && !errors.Is(err, context.Canceled) {
			errCh <- fmt.Errorf("ingest: %w", err)
		}
	})
	if err := <-subscribed; err != nil {
		stop()
		wg.Wait()
		return errors.Join(fmt.Errorf("subscribe telemetry: %w", err), b.Close())
	}

	handler := httpapi.New(mem, hub).Handler()
	wg.Go(func() {
		if err := httpapi.Listen(ctx, httpapi.ListenAddr, handler); err != nil && !errors.Is(err, context.Canceled) {
			errCh <- fmt.Errorf("http: %w", err)
		}
	})

	wg.Go(func() {
		if err := sim.Run(ctx, b.DialAddr()); err != nil && !errors.Is(err, context.Canceled) {
			errCh <- fmt.Errorf("simulator: %w", err)
		}
	})

	var runErr error
	select {
	case <-ctx.Done():
	case err := <-errCh:
		runErr = err
		stop()
	}

	wg.Wait()
	for {
		select {
		case err := <-errCh:
			runErr = errors.Join(runErr, err)
		default:
			if err := b.Close(); err != nil {
				return errors.Join(runErr, fmt.Errorf("close broker: %w", err))
			}
			return runErr
		}
	}
}

type readySub struct {
	ingest.Subscriber
	ready chan error
}

func (s readySub) Subscribe(ctx context.Context, filter string, handler ingest.MessageHandler) error {
	err := s.Subscriber.Subscribe(ctx, filter, handler)
	s.ready <- err
	return err
}

type telemetrySink struct {
	mem *store.Memory
	hub *httpapi.Hub
}

func (s telemetrySink) Apply(t ingest.Telemetry) {
	v := store.Vehicle{
		VIN:       t.VIN,
		DisplayID: t.DisplayID,
		Lat:       t.Lat,
		Lng:       t.Lng,
		Plate:     t.Plate,
		Model:     t.Model,
		Battery:   t.Battery,
		Speed:     t.Speed,
		Heading:   t.Heading,
		Ignition:  t.Ignition,
		Locked:    t.Locked,
		Odometer:  t.Odometer,
		Trip:      t.Trip,
	}
	s.mem.Apply(v)
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	s.hub.Publish(httpapi.Event{Name: "telemetry", Data: data})
}

func rosterFromSim(fleet []sim.Vehicle) []store.Vehicle {
	out := make([]store.Vehicle, 0, len(fleet))
	for _, v := range fleet {
		out = append(out, store.Vehicle{
			VIN:       v.VIN,
			DisplayID: v.DisplayID,
			Lat:       v.Lat,
			Lng:       v.Lng,
			Plate:     v.Plate,
			Model:     v.Model,
			Battery:   v.Battery,
			Speed:     v.Speed,
			Heading:   v.Heading,
			Ignition:  v.Ignition,
			Locked:    v.Locked,
			Odometer:  v.Odometer,
			Trip:      v.Trip,
		})
	}
	return out
}
