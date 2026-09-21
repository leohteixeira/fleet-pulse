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
	"strings"
	"sync"
	"syscall"

	"github.com/leohteixeira/fleet-pulse/internal/broker"
	"github.com/leohteixeira/fleet-pulse/internal/command"
	"github.com/leohteixeira/fleet-pulse/internal/httpapi"
	"github.com/leohteixeira/fleet-pulse/internal/ingest"
	"github.com/leohteixeira/fleet-pulse/internal/sim"
	"github.com/leohteixeira/fleet-pulse/internal/store"
)

const defaultBindAddr = "0.0.0.0:1883"

var (
	_ ingest.Subscriber = (*broker.Broker)(nil)
	_ ingest.Sink       = telemetrySink{}
	_ ingest.AckSink    = (*command.Service)(nil)
	_ httpapi.Store     = (*store.Postgres)(nil)
	_ httpapi.Ready     = (*store.Postgres)(nil)
	_ httpapi.Unlocker  = (*command.Service)(nil)
	_ command.Publisher = (*broker.Broker)(nil)
	_ command.Vehicles  = (*store.Postgres)(nil)
)

var errDatabaseURLRequired = errors.New("database_url is required")

func requireDatabaseURL() (string, error) {
	dsn := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if dsn == "" {
		return "", errDatabaseURLRequired
	}
	return dsn, nil
}

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

	dsn, err := requireDatabaseURL()
	if err != nil {
		return err
	}
	pg, err := store.Open(ctx, dsn, log)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer pg.Close()
	pg.Seed(rosterFromSim(sim.NewFleet()))

	b := broker.New(defaultBindAddr, log)
	if err := b.Start(ctx); err != nil {
		return fmt.Errorf("start broker: %w", err)
	}

	hub := httpapi.NewHub()
	sink := telemetrySink{mem: pg, hub: hub}
	cmds := command.New(b, pg, log)
	cmds.SetListener(func(rec command.Record) {
		data, err := json.Marshal(rec)
		if err != nil {
			return
		}
		hub.Publish(httpapi.Event{Name: "command", Data: data})
	})

	var wg sync.WaitGroup
	errCh := make(chan error, 4)
	ready := newReadySub(b, 4)

	wg.Go(func() {
		if err := ingest.Run(ctx, ready, sink, cmds, log); err != nil && !errors.Is(err, context.Canceled) {
			errCh <- fmt.Errorf("ingest: %w", err)
		}
	})
	if err := <-ready.done; err != nil {
		stop()
		wg.Wait()
		return errors.Join(fmt.Errorf("subscribe ingest: %w", err), b.Close())
	}

	httpCtx, stopHTTP := context.WithCancel(context.Background())
	simCtx, stopSim := context.WithCancel(context.Background())
	handler := httpapi.New(pg, hub, cmds, pg).Handler()
	httpDone := make(chan struct{})
	wg.Go(func() {
		defer close(httpDone)
		if err := httpapi.Listen(httpCtx, httpapi.ListenAddr, handler); err != nil && !errors.Is(err, context.Canceled) {
			errCh <- fmt.Errorf("http: %w", err)
		}
	})
	wg.Go(func() {
		cmds.RunExpiry(simCtx)
	})

	wg.Go(func() {
		if err := sim.Run(simCtx, b.DialAddr()); err != nil && !errors.Is(err, context.Canceled) {
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

	shutErr := orderlyShutdown(shutdownHooks{
		drain:     hub.Drain,
		stopHTTP:  stopHTTP,
		httpDone:  httpDone,
		stopSim:   stopSim,
		wait:      wg.Wait,
		closeBro:  b.Close,
		closePool: pg.Close,
	})
	for {
		select {
		case err := <-errCh:
			runErr = errors.Join(runErr, err)
		default:
			return errors.Join(runErr, shutErr)
		}
	}
}

type shutdownHooks struct {
	drain     func()
	stopHTTP  func()
	httpDone  <-chan struct{}
	stopSim   func()
	wait      func()
	closeBro  func() error
	closePool func()
}

func orderlyShutdown(h shutdownHooks) error {
	if h.drain != nil {
		h.drain()
	}
	if h.stopHTTP != nil {
		h.stopHTTP()
	}
	if h.httpDone != nil {
		<-h.httpDone
	}
	if h.stopSim != nil {
		h.stopSim()
	}
	if h.wait != nil {
		h.wait()
	}
	var broErr error
	if h.closeBro != nil {
		if err := h.closeBro(); err != nil {
			broErr = fmt.Errorf("close broker: %w", err)
		}
	}
	if h.closePool != nil {
		h.closePool()
	}
	return broErr
}

type readySub struct {
	ingest.Subscriber
	done chan error
	left int
	mu   sync.Mutex
}

func newReadySub(sub ingest.Subscriber, n int) *readySub {
	return &readySub{
		Subscriber: sub,
		done:       make(chan error, 1),
		left:       n,
	}
}

func (s *readySub) Subscribe(ctx context.Context, filter string, handler ingest.MessageHandler) error {
	err := s.Subscriber.Subscribe(ctx, filter, handler)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		select {
		case s.done <- err:
		default:
		}
		return err
	}
	s.left--
	if s.left == 0 {
		s.done <- nil
	}
	return nil
}

type fleetApply interface {
	Apply(store.Vehicle) (store.Vehicle, bool)
}

type telemetrySink struct {
	mem fleetApply
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
	exit, crossed := s.mem.Apply(v)
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	s.hub.Publish(httpapi.Event{Name: "telemetry", Data: data})
	if !crossed {
		return
	}
	payload, err := json.Marshal(struct {
		VIN       string  `json:"vin"`
		DisplayID string  `json:"displayId"`
		Lat       float64 `json:"lat"`
		Lng       float64 `json:"lng"`
	}{
		VIN:       exit.VIN,
		DisplayID: exit.DisplayID,
		Lat:       exit.Lat,
		Lng:       exit.Lng,
	})
	if err != nil {
		return
	}
	s.hub.Publish(httpapi.Event{Name: "area-exit", Data: payload})
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
