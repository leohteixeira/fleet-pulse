// Package httpapi serves the fleet snapshot, health check, and SSE stream.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/leohteixeira/fleet-pulse/internal/store"
)

// ListenAddr is the process bind address for the HTTP API.
const ListenAddr = "0.0.0.0:8300"

// Store is the snapshot port declared by HTTP and implemented by the in-memory store.
type Store interface {
	Snapshot() store.Snapshot
}

// HubPort is the publish/subscribe port the stream handler uses.
type HubPort interface {
	Subscribe() (events <-chan Event, unsubscribe func())
	Publish(Event)
}

// Server is the stdlib HTTP surface for snapshot, health, and SSE.
type Server struct {
	store Store
	hub   HubPort
}

// New wires consumer-owned store and hub ports.
func New(store Store, hub HubPort) *Server {
	if hub == nil {
		hub = NewHub()
	}
	return &Server{store: store, hub: hub}
}

// Handler registers GET /api/vehicles, GET /api/stream, and GET /healthz.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/vehicles", s.vehicles)
	mux.HandleFunc("GET /api/stream", s.stream)
	mux.HandleFunc("GET /healthz", s.healthz)
	return mux
}

// Listen binds addr (default 0.0.0.0:8300) and shuts the listener down when ctx is cancelled.
func Listen(ctx context.Context, addr string, handler http.Handler) error {
	if addr == "" {
		addr = ListenAddr
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		BaseContext: func(net.Listener) context.Context {
			return ctx
		},
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutCtx); err != nil {
			return fmt.Errorf("shutdown http: %w", err)
		}
		if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func (s *Server) vehicles(w http.ResponseWriter, _ *http.Request) {
	if err := writeSnapshot(w, s.store.Snapshot()); err != nil {
		slog.Error("snapshot", "err", err)
	}
}

func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func writeSnapshot(w http.ResponseWriter, snap store.Snapshot) error {
	body, err := json.Marshal(snap)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return fmt.Errorf("encode snapshot: %w", err)
	}
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(body); err != nil {
		return fmt.Errorf("write snapshot: %w", err)
	}
	return nil
}
