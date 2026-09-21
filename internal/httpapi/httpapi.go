// Package httpapi serves the fleet snapshot, simulated clock, leasing book,
// door commands, health check, and SSE stream.
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

	"github.com/leohteixeira/fleet-pulse/internal/block"
	"github.com/leohteixeira/fleet-pulse/internal/book"
	"github.com/leohteixeira/fleet-pulse/internal/clock"
	"github.com/leohteixeira/fleet-pulse/internal/command"
	"github.com/leohteixeira/fleet-pulse/internal/store"
	"github.com/leohteixeira/fleet-pulse/internal/webui"
)

// ListenAddr is the process bind address for the HTTP API.
const ListenAddr = "0.0.0.0:8300"

// Store is the snapshot port declared by HTTP and implemented by the fleet store.
type Store interface {
	Snapshot() store.Snapshot
}

// Ready is the readiness port declared by HTTP so healthz can ping the pool
// without the store declaring HTTP types.
type Ready interface {
	Ping(ctx context.Context) error
}

// HubPort is the publish/subscribe port the stream handler uses.
type HubPort interface {
	Subscribe(fleet string) (events <-chan Event, unsubscribe func(), err error)
	Publish(Event)
}

// Unlocker is the door-command port declared by HTTP and implemented by the command machine.
type Unlocker interface {
	Unlock(ctx context.Context, vin, key string) (command.Record, error)
	Lock(ctx context.Context, vin, key string) (command.Record, error)
	Get(id string) (command.Record, bool)
}

// Clock is the calendar port declared by HTTP. The clock package implements it.
type Clock interface {
	Snapshot() clock.Snapshot
}

// Contracts is the book port declared by HTTP. The book package implements it.
type Contracts interface {
	List(ctx context.Context) ([]book.Contract, error)
	Get(ctx context.Context, id string) (book.Detail, error)
	Notify(ctx context.Context, in book.NotifyInput) (book.WriteResult, error)
	Pay(ctx context.Context, in book.PayInput) (book.PayResult, error)
	AppendAudit(ctx context.Context, in book.AuditInput) (string, error)
}

// LeasingCommands looks up a leasing command by id. Same JSON keys as rental.
type LeasingCommands interface {
	Get(id string) (block.Record, bool)
}

// Option configures optional HTTP ports (clock, contracts).
type Option func(*Server)

// WithClock wires GET /api/clock.
func WithClock(c Clock) Option {
	return func(s *Server) {
		s.clock = c
	}
}

// WithContracts wires GET /api/contracts.
func WithContracts(c Contracts) Option {
	return func(s *Server) {
		s.contracts = c
	}
}

// WithLeasingCommands lets GET /api/commands/{id} resolve leasing records.
func WithLeasingCommands(c LeasingCommands) Option {
	return func(s *Server) {
		s.leasing = c
	}
}

// Server is the stdlib HTTP surface for snapshot, lock/unlock, health, and SSE.
type Server struct {
	store          Store
	hub            HubPort
	unlocker       Unlocker
	ready          Ready
	clock          Clock
	contracts      Contracts
	leasing        LeasingCommands
	blocks         Blocker
	roster         Roster
	keys           Idempotency
	auditSecret    string
	trustForwarded bool
	guards         *writeGuards
	files          fs.FS
}

// New wires consumer-owned store, hub, command, and ready ports.
func New(store Store, hub HubPort, unlocker Unlocker, ready Ready, opts ...Option) *Server {
	if hub == nil {
		hub = NewHub()
	}
	s := &Server{
		store:          store,
		hub:            hub,
		unlocker:       unlocker,
		ready:          ready,
		files:          webui.FS(),
		auditSecret:    auditSecretFromEnv(),
		trustForwarded: trustForwardedFromEnv(),
		guards:         newWriteGuards(),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(s)
		}
	}
	return s
}

// Handler registers snapshot, lock, unlock, command lookup, stream, health, and the SPA.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/clock", s.clockHandler)
	mux.HandleFunc("GET /api/leasing/vehicles", s.leasingVehicles)
	mux.HandleFunc("GET /api/contracts", s.contractsHandler)
	mux.HandleFunc("GET /api/contracts/{id}", s.contractDetail)
	mux.HandleFunc("POST /api/contracts/{id}/notify", s.notifyContract)
	mux.HandleFunc("POST /api/contracts/{id}/block", s.blockContract)
	mux.HandleFunc("POST /api/contracts/{id}/block/cancel", s.cancelBlock)
	mux.HandleFunc("POST /api/contracts/{id}/payments", s.payContract)
	mux.HandleFunc("GET /api/vehicles", s.vehicles)
	mux.HandleFunc("POST /api/vehicles/{vin}/unlock", s.unlock)
	mux.HandleFunc("POST /api/vehicles/{vin}/lock", s.lock)
	mux.HandleFunc("GET /api/commands/{id}", s.command)
	mux.HandleFunc("GET /api/stream", s.stream)
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /{$}", s.spa)
	mux.HandleFunc("GET /{path...}", s.spa)
	return withSameOriginCORS(mux)
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

func (s *Server) clockHandler(w http.ResponseWriter, _ *http.Request) {
	if s.clock == nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	snap := s.clock.Snapshot()
	body := clockBody{
		Simulated:  snap.Simulated.UTC().Format(time.RFC3339),
		Real:       snap.Real.UTC().Format(time.RFC3339),
		Rate:       snap.Rate,
		Multiplier: snap.Multiplier,
	}
	if err := writeJSON(w, http.StatusOK, body); err != nil {
		slog.Error("clock", "err", err)
	}
}

func (s *Server) contractsHandler(w http.ResponseWriter, r *http.Request) {
	if s.contracts == nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	list, err := s.contracts.List(r.Context())
	if err != nil {
		slog.Error("contracts", "err", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if list == nil {
		list = []book.Contract{}
	}
	list = book.FilterByBand(list, r.URL.Query().Get("overdueBand"))
	if state := r.URL.Query().Get("vehicleState"); state != "" && s.blocks != nil {
		filtered := make([]book.Contract, 0, len(list))
		for _, c := range list {
			if matchesVehicleState(s.blocks, c.VIN, state) {
				filtered = append(filtered, c)
			}
		}
		list = filtered
	}
	if err := writeJSON(w, http.StatusOK, contractsBody{Contracts: list}); err != nil {
		slog.Error("contracts", "err", err)
	}
}

func (s *Server) vehicles(w http.ResponseWriter, _ *http.Request) {
	if err := writeSnapshot(w, s.store.Snapshot()); err != nil {
		slog.Error("snapshot", "err", err)
	}
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	if s.ready == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.ready.Ping(ctx); err != nil {
		slog.Error("healthz ping failed")
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
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
	id := r.PathValue("id")
	if s.unlocker != nil {
		if rec, ok := s.unlocker.Get(id); ok {
			if err := writeJSON(w, http.StatusOK, rec); err != nil {
				slog.Error("command response", "err", err)
			}
			return
		}
	}
	if s.blocks != nil {
		if rec, ok := s.blocks.Get(id); ok {
			if err := writeJSON(w, http.StatusOK, rec); err != nil {
				slog.Error("command response", "err", err)
			}
			return
		}
	}
	if s.leasing != nil {
		if rec, ok := s.leasing.Get(id); ok {
			if err := writeJSON(w, http.StatusOK, rec); err != nil {
				slog.Error("command response", "err", err)
			}
			return
		}
	}
	http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
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

type clockBody struct {
	Simulated  string `json:"simulated"`
	Real       string `json:"real"`
	Rate       string `json:"rate"`
	Multiplier int    `json:"multiplier"`
}

type contractsBody struct {
	Contracts []book.Contract `json:"contracts"`
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
