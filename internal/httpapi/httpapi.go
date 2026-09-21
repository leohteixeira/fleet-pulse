// Package httpapi serves the fleet snapshot, door commands, health check, and SSE stream.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/leohteixeira/fleet-pulse/internal/command"
	"github.com/leohteixeira/fleet-pulse/internal/store"
	"github.com/leohteixeira/fleet-pulse/internal/webui"
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

// Unlocker is the door-command port declared by HTTP and implemented by the command machine.
type Unlocker interface {
	Unlock(ctx context.Context, vin, key string) (command.Record, error)
	Lock(ctx context.Context, vin, key string) (command.Record, error)
	Get(id string) (command.Record, bool)
}

// Server is the stdlib HTTP surface for snapshot, lock/unlock, health, and SSE.
type Server struct {
	store    Store
	hub      HubPort
	unlocker Unlocker
	files    fs.FS
}

// New wires consumer-owned store, hub, and command ports.
func New(store Store, hub HubPort, unlocker Unlocker) *Server {
	if hub == nil {
		hub = NewHub()
	}
	return &Server{store: store, hub: hub, unlocker: unlocker, files: webui.FS()}
}

// Handler registers snapshot, lock, unlock, command lookup, stream, health, and the SPA.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/vehicles", s.vehicles)
	mux.HandleFunc("POST /api/vehicles/{vin}/unlock", s.unlock)
	mux.HandleFunc("POST /api/vehicles/{vin}/lock", s.lock)
	mux.HandleFunc("GET /api/commands/{id}", s.command)
	mux.HandleFunc("GET /api/stream", s.stream)
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /{$}", s.spa)
	mux.HandleFunc("GET /{path...}", s.spa)
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

func (s *Server) spa(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		http.NotFound(w, r)
		return
	}
	files := s.files
	if files == nil {
		http.NotFound(w, r)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}
	if f, err := files.Open(path); err == nil {
		_ = f.Close()
		info, err := fs.Stat(files, path)
		if err == nil && !info.IsDir() {
			http.FileServer(http.FS(files)).ServeHTTP(w, r)
			return
		}
	}
	body, err := fs.ReadFile(files, "index.html")
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(body); err != nil {
		slog.Error("spa fallback", "err", err)
	}
}

func (s *Server) unlock(w http.ResponseWriter, r *http.Request) {
	if s.unlocker == nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	s.doorCommand(w, r, s.unlocker.Unlock)
}

func (s *Server) lock(w http.ResponseWriter, r *http.Request) {
	if s.unlocker == nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	s.doorCommand(w, r, s.unlocker.Lock)
}

func (s *Server) doorCommand(w http.ResponseWriter, r *http.Request, submit func(context.Context, string, string) (command.Record, error)) {
	if submit == nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	vin := r.PathValue("vin")
	key := r.Header.Get("Idempotency-Key")
	rec, err := submit(r.Context(), vin, key)
	if err != nil && rec.ID == "" {
		s.writeCommandError(w, err)
		return
	}
	if err := writeJSON(w, http.StatusAccepted, commandBody{ID: rec.ID, State: rec.State}); err != nil {
		slog.Error("command response", "err", err)
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

func (s *Server) writeCommandError(w http.ResponseWriter, err error) {
	var conflict *command.ConflictError
	switch {
	case errors.Is(err, command.ErrMissingKey), errors.Is(err, command.ErrUnknownAction):
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
	case errors.Is(err, command.ErrUnknownVIN):
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
	case errors.As(err, &conflict):
		if writeErr := writeJSON(w, http.StatusConflict, commandBody{
			ID:    conflict.Current.ID,
			State: conflict.Current.State,
		}); writeErr != nil {
			slog.Error("command conflict", "err", writeErr)
		}
	default:
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}
}

type commandBody struct {
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
