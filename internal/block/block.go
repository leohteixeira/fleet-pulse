// Package block owns the leasing immobilizer machine and last-known telem.
package block

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"
)

const (
	// ActionBlock immobilizes a leasing vehicle.
	ActionBlock = "block"
	// ActionUnlock releases a blocked leasing vehicle.
	ActionUnlock = "unlock"

	// StateRequested is HTTP/package accepted and not yet armed.
	StateRequested = "REQUESTED"
	// StateArmed waits for speed 0, ignition off, and online.
	StateArmed = "ARMED"
	// StateSent is published via the outbox and waiting for ack.
	StateSent = "SENT"
	// StateAcked is a successful device ack. Terminal.
	StateAcked = "ACKED"
	// StateFailed is a refused device ack. Terminal.
	StateFailed = "FAILED"
	// StateTimeout is ARMED offline more than ArmedTimeout. Terminal.
	StateTimeout = "TIMEOUT"
	// StateCancelled is a visitor or payment cancel. Terminal.
	StateCancelled = "CANCELLED"

	// Fleet is the commands.fleet discriminator for leasing rows.
	Fleet = "leasing"
	// AuditOrigin is the audit origin until visitor hash exists.
	AuditOrigin = "SISTEMA"
	// ArmedTimeout is how long ARMED may stay offline.
	ArmedTimeout = 30 * time.Second

	tickInterval   = 100 * time.Millisecond
	persistTimeout = 2 * time.Second
)

var (
	// ErrInTransit is returned when Cancel is refused for a SENT command.
	ErrInTransit = errors.New("block: in transit")
	// ErrUnknownVIN is returned when the VIN is not a leasing vehicle.
	ErrUnknownVIN = errors.New("block: unknown vin")
	// ErrUnknownCommand is returned when Cancel/Get cannot find the id.
	ErrUnknownCommand = errors.New("block: unknown command")
	// ErrNotBlocked is returned when Unlock is called on a free vehicle.
	ErrNotBlocked = errors.New("block: vehicle is not blocked")
)

// Record is one leasing block or unlock command.
type Record struct {
	ID            string    `json:"id"`
	VIN           string    `json:"vin"`
	Action        string    `json:"action"`
	State         string    `json:"state"`
	CorrelationID string    `json:"correlationId"`
	CreatedAt     time.Time `json:"-"`
	SentAt        time.Time `json:"-"`
	UpdatedAt     time.Time `json:"-"`
}

// LastKnown is leasing telemetry used by the machine (not the rental snapshot).
type LastKnown struct {
	VIN       string
	Speed     int
	Ignition  bool
	Lat       float64
	Lng       float64
	Battery   int
	Heading   int
	Locked    bool
	Odometer  float64
	Trip      float64
	Plate     string
	Model     string
	DisplayID string
}

// PersistWrite is command + audit + optional outbox for one transaction.
type PersistWrite struct {
	Command Record
	Insert  bool
	SentAt  time.Time
	Audit   AuditEntry
	Outbox  *OutboxEntry
}

// AuditEntry is one append-only audit row.
type AuditEntry struct {
	Action  string
	Payload []byte
}

// OutboxEntry is an unpublished MQTT payload.
type OutboxEntry struct {
	Topic   string
	Payload []byte
}

// Clock is the time port used so timeout tests can lock the clock.
type Clock interface {
	Now() time.Time
}

// Store is the persist port declared by block.
type Store interface {
	Persist(ctx context.Context, w PersistWrite) error
	GetCommand(ctx context.Context, id string) (Record, bool, error)
	ListActive(ctx context.Context) ([]Record, error)
	ListLatestAcked(ctx context.Context) ([]Record, error)
	HasVIN(ctx context.Context, vin string) (bool, error)
	UpsertTelem(ctx context.Context, t LastKnown) error
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

type telem struct {
	Speed    int
	Ignition bool
	SeenAt   time.Time
}

// Service is the leasing block machine: request, arm, send, ack, timeout, unlock.
type Service struct {
	mu      sync.Mutex
	records map[string]*Record
	telem   map[string]telem
	blocked map[string]bool
	store   Store
	clock   Clock
	live    func() bool
	log     *slog.Logger
	newID   func() string
}

// Option configures optional clock and live-stream ports.
type Option func(*Service)

// WithClock injects the clock used for ARMED timeout and telem freshness.
func WithClock(c Clock) Option {
	return func(s *Service) {
		if c != nil {
			s.clock = c
		}
	}
}

// WithLive gates ARMED→SENT. Default is always live.
func WithLive(fn func() bool) Option {
	return func(s *Service) {
		if fn != nil {
			s.live = fn
		}
	}
}

// New wires persist, a JSON logger, and optional clock/live ports.
func New(store Store, log *slog.Logger, opts ...Option) *Service {
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	s := &Service{
		records: make(map[string]*Record),
		telem:   make(map[string]telem),
		blocked: make(map[string]bool),
		store:   store,
		clock:   realClock{},
		live:    func() bool { return true },
		log:     log,
		newID:   randomID,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(s)
		}
	}
	return s
}

// Load hydrates in-flight commands and ACKED block flags from the store.
func (s *Service) Load(ctx context.Context) error {
	if s.store == nil {
		return nil
	}
	active, err := s.store.ListActive(ctx)
	if err != nil {
		return fmt.Errorf("list active commands: %w", err)
	}
	acked, err := s.store.ListLatestAcked(ctx)
	if err != nil {
		return fmt.Errorf("list acked commands: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range active {
		rec := active[i]
		copied := rec
		s.records[rec.ID] = &copied
	}
	for _, rec := range acked {
		s.blocked[rec.VIN] = rec.Action == ActionBlock
	}
	return nil
}

// Request accepts a block command in REQUESTED. The first Tick moves it to ARMED.
func (s *Service) Request(ctx context.Context, vin string) (Record, error) {
	if vin == "" {
		return Record{}, ErrUnknownVIN
	}
	if err := s.requireVIN(ctx, vin); err != nil {
		return Record{}, err
	}

	s.mu.Lock()
	busy := s.blocked[vin] || s.hasActive(vin, ActionBlock)
	s.mu.Unlock()
	if busy {
		return Record{}, ErrInTransit
	}

	rec := Record{
		ID:            s.newID(),
		VIN:           vin,
		Action:        ActionBlock,
		State:         StateRequested,
		CorrelationID: s.newID(),
		CreatedAt:     s.clock.Now(),
		UpdatedAt:     s.clock.Now(),
	}
	if err := s.persist(ctx, PersistWrite{
		Command: rec,
		Insert:  true,
		Audit:   auditOf(rec),
	}); err != nil {
		return Record{}, err
	}

	s.mu.Lock()
	s.records[rec.ID] = &rec
	s.mu.Unlock()
	s.logChange("leasing command accepted", rec)
	return rec, nil
}

// Cancel moves REQUESTED or ARMED to CANCELLED. SENT is refused.
func (s *Service) Cancel(ctx context.Context, id string) (Record, error) {
	if id == "" {
		return Record{}, ErrUnknownCommand
	}

	s.mu.Lock()
	cur, ok := s.records[id]
	if !ok {
		s.mu.Unlock()
		return Record{}, ErrUnknownCommand
	}
	switch cur.State {
	case StateRequested, StateArmed:
		prev := *cur
		cur.State = StateCancelled
		cur.UpdatedAt = s.clock.Now()
		rec := *cur
		s.mu.Unlock()
		if err := s.persist(ctx, PersistWrite{
			Command: rec,
			Audit:   auditOf(rec),
		}); err != nil {
			s.mu.Lock()
			if live, found := s.records[id]; found && live.State == StateCancelled {
				*live = prev
			}
			s.mu.Unlock()
			return Record{}, err
		}
		s.logChange("leasing command cancelled", rec)
		return rec, nil
	case StateSent:
		s.mu.Unlock()
		return Record{}, ErrInTransit
	default:
		s.mu.Unlock()
		return Record{}, ErrUnknownCommand
	}
}

// Unlock persists an unlock for a blocked VIN. Outbox waits for online telem.
func (s *Service) Unlock(ctx context.Context, vin string) (Record, error) {
	if vin == "" {
		return Record{}, ErrUnknownVIN
	}
	if err := s.requireVIN(ctx, vin); err != nil {
		return Record{}, err
	}

	s.mu.Lock()
	blocked := s.blocked[vin]
	pending := s.hasActive(vin, ActionUnlock)
	s.mu.Unlock()
	if !blocked {
		return Record{}, ErrNotBlocked
	}
	if pending {
		return Record{}, ErrInTransit
	}

	rec := Record{
		ID:            s.newID(),
		VIN:           vin,
		Action:        ActionUnlock,
		State:         StateRequested,
		CorrelationID: s.newID(),
		CreatedAt:     s.clock.Now(),
		UpdatedAt:     s.clock.Now(),
	}
	if err := s.persist(ctx, PersistWrite{
		Command: rec,
		Insert:  true,
		Audit:   auditOf(rec),
	}); err != nil {
		return Record{}, err
	}

	s.mu.Lock()
	s.records[rec.ID] = &rec
	s.mu.Unlock()
	s.logChange("leasing unlock accepted", rec)
	return rec, nil
}

// Get returns a copy of the leasing command. Unknown ids are not found.
func (s *Service) Get(id string) (Record, bool) {
	if id == "" {
		return Record{}, false
	}
	s.mu.Lock()
	rec, ok := s.clone(id)
	s.mu.Unlock()
	if ok {
		return rec, true
	}
	if s.store == nil {
		return Record{}, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), persistTimeout)
	defer cancel()
	rec, ok, err := s.store.GetCommand(ctx, id)
	if err != nil || !ok {
		return Record{}, false
	}
	return rec, true
}

// ApplyAck records a device ack. Unknown or non-SENT ids are ignored.
func (s *Service) ApplyAck(commandID string, ok bool) {
	if commandID == "" {
		return
	}
	s.mu.Lock()
	rec, found := s.clone(commandID)
	s.mu.Unlock()
	if !found || rec.State != StateSent {
		return
	}
	if ok {
		rec.State = StateAcked
	} else {
		rec.State = StateFailed
	}
	rec.UpdatedAt = s.clock.Now()

	ctx, cancel := context.WithTimeout(context.Background(), persistTimeout)
	defer cancel()
	if err := s.persist(ctx, PersistWrite{
		Command: rec,
		Audit:   auditOf(rec),
	}); err != nil {
		s.log.Error("persist leasing ack failed", "commandId", rec.ID, "err", err)
		return
	}

	s.mu.Lock()
	s.records[rec.ID] = &rec
	if rec.Action == ActionBlock && rec.State == StateAcked {
		s.blocked[rec.VIN] = true
	}
	if rec.Action == ActionUnlock && rec.State == StateAcked {
		s.blocked[rec.VIN] = false
	}
	s.mu.Unlock()
	s.logChange("leasing command ack", rec)
}

// NoteTelem records last-known leasing telem for the machine and persists it.
func (s *Service) NoteTelem(t LastKnown) {
	if t.VIN == "" {
		return
	}
	now := s.clock.Now()
	s.mu.Lock()
	s.telem[t.VIN] = telem{Speed: t.Speed, Ignition: t.Ignition, SeenAt: now}
	s.mu.Unlock()

	if s.store == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), persistTimeout)
	defer cancel()
	if err := s.store.UpsertTelem(ctx, t); err != nil {
		s.log.Error("persist leasing telem failed", "vin", t.VIN, "err", err)
	}
}

// Blocked reports whether vin has an ACKED block that is not yet unlocked.
func (s *Service) Blocked(vin string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.blocked[vin]
}

// Topic is the server publish topic for a leasing VIN.
func Topic(vin string) string {
	return "leasing/" + vin + "/commands"
}

func (s *Service) requireVIN(ctx context.Context, vin string) error {
	if s.store == nil {
		return nil
	}
	ok, err := s.store.HasVIN(ctx, vin)
	if err != nil {
		return fmt.Errorf("lookup leasing vin: %w", err)
	}
	if !ok {
		return ErrUnknownVIN
	}
	return nil
}

func (s *Service) persist(ctx context.Context, w PersistWrite) error {
	if s.store == nil {
		return nil
	}
	if err := s.store.Persist(ctx, w); err != nil {
		return fmt.Errorf("persist leasing command: %w", err)
	}
	return nil
}

func (s *Service) hasActive(vin, action string) bool {
	for _, rec := range s.records {
		if rec.VIN != vin || rec.Action != action {
			continue
		}
		switch rec.State {
		case StateRequested, StateArmed, StateSent:
			return true
		}
	}
	return false
}

func (s *Service) clone(id string) (Record, bool) {
	rec, ok := s.records[id]
	if !ok {
		return Record{}, false
	}
	return *rec, true
}

func (s *Service) replace(rec Record) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records[rec.ID] = &rec
}

func (s *Service) logChange(msg string, rec Record) {
	s.log.Info(msg,
		"correlationId", rec.CorrelationID,
		"commandId", rec.ID,
		"vin", rec.VIN,
		"action", rec.Action,
		"state", rec.State,
	)
}

func auditOf(rec Record) AuditEntry {
	payload, err := json.Marshal(map[string]string{
		"origin":        AuditOrigin,
		"commandId":     rec.ID,
		"vin":           rec.VIN,
		"action":        rec.Action,
		"state":         rec.State,
		"correlationId": rec.CorrelationID,
	})
	if err != nil {
		payload = []byte(`{"origin":"SISTEMA"}`)
	}
	return AuditEntry{
		Action:  rec.Action + "." + rec.State,
		Payload: payload,
	}
}

func encodeCommand(rec Record) ([]byte, error) {
	raw, err := json.Marshal(struct {
		ID            string `json:"id"`
		Action        string `json:"action"`
		CorrelationID string `json:"correlationId"`
	}{
		ID:            rec.ID,
		Action:        rec.Action,
		CorrelationID: rec.CorrelationID,
	})
	if err != nil {
		return nil, fmt.Errorf("encode leasing command: %w", err)
	}
	return raw, nil
}

func randomID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}
