// Package httpapi serves the fleet snapshot, unlock commands, health check, and SSE stream.
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

	"github.com/leohteixeira/fleet-pulse/internal/command"
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

// Unlocker is the unlock port declared by HTTP and implemented by the command machine.
type Unlocker interface {
	Unlock(ctx context.Context, vin, key string) (command.Record, error)
	Get(id string) (command.Record, bool)
}

// Server is the stdlib HTTP surface for snapshot, unlock, health, and SSE.
type Server struct {
	store    Store
	hub      HubPort
	unlocker Unlocker
}

// New wires consumer-owned store, hub, and unlock ports.
func New(store Store, hub HubPort, unlocker Unlocker) *Server {
	if hub == nil {
		hub = NewHub()
	}
	return &Server{store: store, hub: hub, unlocker: unlocker}
}

// Handler registers snapshot, unlock, command lookup, stream, and health routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/vehicles", s.vehicles)
	mux.HandleFunc("POST /api/vehicles/{vin}/unlock", s.unlock)
	mux.HandleFunc("GET /api/commands/{id}", s.command)
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

func (s *Server) unlock(w http.ResponseWriter, r *http.Request) {
	if s.unlocker == nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	vin := r.PathValue("vin")
	key := r.Header.Get("Idempotency-Key")
	rec, err := s.unlocker.Unlock(r.Context(), vin, key)
	if err != nil && rec.ID == "" {
		s.writeUnlockError(w, err)
		return
	}
	if err := writeJSON(w, http.StatusAccepted, unlockBody{ID: rec.ID, State: rec.State}); err != nil {
		slog.Error("unlock response", "err", err)
	}
}

func (s *Server) command(w http.ResponseWriter, r *http.Request) {
	if s.unlocker == nil {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}
	rec, ok := s.unlocker.Get(r.PathValue("id"))
	if !ok {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}
	if err := writeJSON(w, http.StatusOK, rec); err != nil {
		slog.Error("command response", "err", err)
	}
}

func (s *Server) writeUnlockError(w http.ResponseWriter, err error) {
	var conflict *command.ConflictError
	switch {
	case errors.Is(err, command.ErrMissingKey):
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
	case errors.Is(err, command.ErrUnknownVIN):
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
	case errors.As(err, &conflict):
		if writeErr := writeJSON(w, http.StatusConflict, unlockBody{
			ID:    conflict.Current.ID,
			State: conflict.Current.State,
		}); writeErr != nil {
			slog.Error("unlock conflict", "err", writeErr)
		}
	default:
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}
}

type unlockBody struct {
	ID    string `json:"id"`
	State string `json:"state"`
}

func writeJSON(w http.ResponseWriter, status int, v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return fmt.Errorf("encode json: %w", err)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err := w.Write(body); err != nil {
		return fmt.Errorf("write json: %w", err)
	}
	return nil
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
