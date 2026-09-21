// Command server starts the embedded MQTT broker, vehicle simulator, and telemetry ingest.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/leohteixeira/fleet-pulse/internal/broker"
	"github.com/leohteixeira/fleet-pulse/internal/ingest"
	"github.com/leohteixeira/fleet-pulse/internal/sim"
)

const defaultBindAddr = "0.0.0.0:1883"

var _ ingest.Subscriber = (*broker.Broker)(nil)

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

	var wg sync.WaitGroup
	errCh := make(chan error, 2)
	subscribed := make(chan error, 1)

	wg.Go(func() {
		if err := ingest.Run(ctx, readySub{Subscriber: b, ready: subscribed}, log); err != nil && !errors.Is(err, context.Canceled) {
			errCh <- fmt.Errorf("ingest: %w", err)
		}
	})
	if err := <-subscribed; err != nil {
		stop()
		wg.Wait()
		return errors.Join(fmt.Errorf("subscribe telemetry: %w", err), b.Close())
	}

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
