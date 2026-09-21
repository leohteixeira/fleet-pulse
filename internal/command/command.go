// Package command owns the asynchronous lock/unlock state machine.
package command

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
	// ActionUnlock is a door-unlock command.
	ActionUnlock = "unlock"
	// ActionLock is a door-lock command.
	ActionLock = "lock"

	// StatePending is HTTP-accepted and not yet published.
	StatePending = "PENDING"
	// StateSent is published and waiting for ack.
	StateSent = "SENT"
	// StateAcked is a successful device ack. Terminal.
	StateAcked = "ACKED"
	// StateFailed is a refused device ack. Terminal.
	StateFailed = "FAILED"
	// StateTimeout is no ack before Expiry after SENT. Terminal.
	StateTimeout = "TIMEOUT"

	// Expiry is measured from SENT (publish), not HTTP accept.
	Expiry = 5 * time.Second

	publishTimeout = 2 * time.Second
	expiryTick     = 100 * time.Millisecond
)

var (
	// ErrMissingKey is returned when Idempotency-Key is empty.
	ErrMissingKey = errors.New("command: missing idempotency key")
	// ErrUnknownVIN is returned when the VIN is not in the fleet.
	ErrUnknownVIN = errors.New("command: unknown vin")
	// ErrUnknownAction is returned when action is not lock or unlock.
	ErrUnknownAction = errors.New("command: unknown action")
)

// ConflictError is a distinct new command while the one-deep queue is full.
type ConflictError struct {
	Current Record
}

func (e *ConflictError) Error() string {
	return "command: queue full"
}

// Record is one lock or unlock command.
type Record struct {
	ID            string    `json:"id"`
	VIN           string    `json:"vin"`
	Action        string    `json:"action"`
	State         string    `json:"state"`
	CorrelationID string    `json:"correlationId"`
	Key           string    `json:"-"`
	SentAt        time.Time `json:"-"`
}

// Clock is the time port used so timeout tests can lock the clock.
type Clock interface {
	Now() time.Time
}

// Publisher is the MQTT publish port declared by command.
type Publisher interface {
	Publish(ctx context.Context, topic string, payload []byte) error
}

// Vehicles is the VIN lookup port declared by command.
type Vehicles interface {
	Has(vin string) bool
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// Service is the lock/unlock machine: create, queue, publish, ack, expire, replay.
type Service struct {
	mu       sync.Mutex
	records  map[string]*Record
	byKey    map[string]string
	inFlight map[string]string
	queued   map[string]string
	pub      Publisher
	vehicles Vehicles
	clock    Clock
	log      *slog.Logger
	newID    func() string
	listener func(Record)
}

// New wires a publisher, VIN lookup, and JSON logger.
func New(pub Publisher, vehicles Vehicles, log *slog.Logger) *Service {
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if vehicles == nil {
		vehicles = nopVehicles{}
	}
	return &Service{
		records:  make(map[string]*Record),
		byKey:    make(map[string]string),
		inFlight: make(map[string]string),
		queued:   make(map[string]string),
		pub:      pub,
		vehicles: vehicles,
		clock:    realClock{},
		log:      log,
		newID:    randomID,
	}
}

type nopVehicles struct{}

func (nopVehicles) Has(string) bool { return false }

// SetListener receives a copy after each state change. Optional SSE hook.
func (s *Service) SetListener(fn func(Record)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listener = fn
}

// Unlock creates or replays an unlock for vin+key.
func (s *Service) Unlock(ctx context.Context, vin, key string) (Record, error) {
	return s.Submit(ctx, vin, ActionUnlock, key)
}

// Lock creates or replays a lock for vin+key.
func (s *Service) Lock(ctx context.Context, vin, key string) (Record, error) {
	return s.Submit(ctx, vin, ActionLock, key)
}

// Submit creates or replays a door command. Same vin+action+key does not republish.
// A distinct command while one is PENDING or SENT is accepted as unpublished PENDING.
// A third distinct command while that slot is full returns ConflictError with the in-flight record.
func (s *Service) Submit(ctx context.Context, vin, action, key string) (Record, error) {
	if key == "" {
		return Record{}, ErrMissingKey
	}
	if !validAction(action) {
		return Record{}, ErrUnknownAction
	}
	if vin == "" || !s.vehicles.Has(vin) {
		return Record{}, ErrUnknownVIN
	}

	s.mu.Lock()
	if id, ok := s.byKey[idempotencyKey(vin, action, key)]; ok {
		rec := *s.records[id]
		s.mu.Unlock()
		return rec, nil
	}
	if flightID, ok := s.inFlight[vin]; ok {
		if _, full := s.queued[vin]; full {
			rec := *s.records[flightID]
			s.mu.Unlock()
			return Record{}, &ConflictError{Current: rec}
		}
		rec := s.accept(vin, action, key)
		s.queued[vin] = rec.ID
		accepted := *rec
		s.logAccept(rec)
		s.mu.Unlock()
		s.notify(accepted)
		return accepted, nil
	}

	rec := s.accept(vin, action, key)
	s.inFlight[vin] = rec.ID
	accepted := *rec
	s.logAccept(rec)
	s.mu.Unlock()
	s.notify(accepted)
	return s.publish(ctx, rec)
}

func (s *Service) accept(vin, action, key string) *Record {
	rec := &Record{
		ID:            s.newID(),
		VIN:           vin,
		Action:        action,
		State:         StatePending,
		CorrelationID: s.newID(),
		Key:           key,
	}
	s.records[rec.ID] = rec
	s.byKey[idempotencyKey(vin, action, key)] = rec.ID
	return rec
}

func (s *Service) logAccept(rec *Record) {
	s.log.Info("command accepted",
		"correlationId", rec.CorrelationID,
		"commandId", rec.ID,
		"vin", rec.VIN,
		"action", rec.Action,
		"state", rec.State,
	)
}

func (s *Service) publish(ctx context.Context, rec *Record) (Record, error) {
	payload, err := json.Marshal(wireCommand{
		ID:            rec.ID,
		Action:        rec.Action,
		CorrelationID: rec.CorrelationID,
	})
	if err != nil {
		return s.failPublish(rec, fmt.Errorf("encode command: %w", err))
	}

	pubCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), publishTimeout)
	defer cancel()
	if err := s.pub.Publish(pubCtx, Topic(rec.VIN), payload); err != nil {
		return s.failPublish(rec, fmt.Errorf("publish command: %w", err))
	}

	s.mu.Lock()
	if rec.State == StatePending {
		rec.State = StateSent
		rec.SentAt = s.clock.Now()
	}
	sent := *rec
	s.log.Info("command published",
		"correlationId", rec.CorrelationID,
		"commandId", rec.ID,
		"vin", rec.VIN,
		"action", rec.Action,
		"state", rec.State,
	)
	s.mu.Unlock()
	s.notify(sent)
	return sent, nil
}

func (s *Service) publishSuccessor(next *Record) {
	if next == nil {
		return
	}
	_, _ = s.publish(context.Background(), next)
}

// Get returns a copy of the command record.
func (s *Service) Get(id string) (Record, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.records[id]
	if !ok {
		return Record{}, false
	}
	return *rec, true
}

// Apply records a device ack. Unknown, queued, or terminal ids are ignored.
func (s *Service) Apply(commandID string, ok bool) {
	if commandID == "" {
		return
	}

	s.mu.Lock()
	rec, exists := s.records[commandID]
	if !exists {
		s.mu.Unlock()
		return
	}
	if s.inFlight[rec.VIN] != rec.ID {
		s.mu.Unlock()
		return
	}
	inFlight := rec.State == StatePending || rec.State == StateSent
	if !inFlight {
		s.mu.Unlock()
		return
	}
	if ok {
		rec.State = StateAcked
	} else {
		rec.State = StateFailed
	}
	next := s.promote(rec.VIN)
	updated := *rec
	s.log.Info("command ack",
		"correlationId", rec.CorrelationID,
		"commandId", rec.ID,
		"vin", rec.VIN,
		"action", rec.Action,
		"state", rec.State,
	)
	s.mu.Unlock()
	s.notify(updated)
	s.publishSuccessor(next)
}

// Expire moves SENT commands past Expiry to TIMEOUT.
func (s *Service) Expire() {
	s.mu.Lock()
	now := s.clock.Now()
	changed := make([]Record, 0)
	successors := make([]*Record, 0)
	for _, rec := range s.records {
		if rec.State != StateSent || rec.SentAt.IsZero() {
			continue
		}
		if now.Sub(rec.SentAt) < Expiry {
			continue
		}
		rec.State = StateTimeout
		if next := s.promote(rec.VIN); next != nil {
			successors = append(successors, next)
		}
		changed = append(changed, *rec)
		s.log.Info("command timeout",
			"correlationId", rec.CorrelationID,
			"commandId", rec.ID,
			"vin", rec.VIN,
			"action", rec.Action,
			"state", rec.State,
		)
	}
	s.mu.Unlock()
	for _, rec := range changed {
		s.notify(rec)
	}
	for _, next := range successors {
		s.publishSuccessor(next)
	}
}

// RunExpiry ticks until ctx is cancelled.
func (s *Service) RunExpiry(ctx context.Context) {
	ticker := time.NewTicker(expiryTick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.Expire()
		}
	}
}

// Topic is the server publish topic for vin.
func Topic(vin string) string {
	return "fleet/" + vin + "/commands"
}

type wireCommand struct {
	ID            string `json:"id"`
	Action        string `json:"action"`
	CorrelationID string `json:"correlationId"`
}

func (s *Service) failPublish(rec *Record, err error) (Record, error) {
	s.mu.Lock()
	var next *Record
	if rec.State == StatePending {
		rec.State = StateFailed
		if s.inFlight[rec.VIN] == rec.ID {
			next = s.promote(rec.VIN)
		}
	}
	failed := *rec
	s.mu.Unlock()
	s.notify(failed)
	s.publishSuccessor(next)
	return failed, err
}

// promote moves the queued successor into in-flight. Caller holds s.mu.
func (s *Service) promote(vin string) *Record {
	delete(s.inFlight, vin)
	queuedID, ok := s.queued[vin]
	if !ok {
		return nil
	}
	next := s.records[queuedID]
	delete(s.queued, vin)
	if next == nil {
		return nil
	}
	s.inFlight[vin] = next.ID
	return next
}

func (s *Service) notify(rec Record) {
	s.mu.Lock()
	fn := s.listener
	s.mu.Unlock()
	if fn != nil {
		fn(rec)
	}
}

func validAction(action string) bool {
	return action == ActionUnlock || action == ActionLock
}

func idempotencyKey(vin, action, key string) string {
	return vin + "\x00" + action + "\x00" + key
}

func randomID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}
