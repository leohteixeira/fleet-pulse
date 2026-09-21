// Package command owns the asynchronous unlock state machine.
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
	// ActionUnlock is the only action this story implements.
	ActionUnlock = "unlock"

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
)

// ConflictError is a distinct new unlock while one is PENDING or SENT.
type ConflictError struct {
	Current Record
}

func (e *ConflictError) Error() string {
	return "command: unlock already in flight"
}

// Record is one unlock command.
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

// Service is the unlock machine: create, publish, ack, expire, replay.
type Service struct {
	mu       sync.Mutex
	records  map[string]*Record
	byKey    map[string]string
	inFlight map[string]string
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

// Unlock creates or replays an unlock for vin+key. Same key does not republish.
func (s *Service) Unlock(ctx context.Context, vin, key string) (Record, error) {
	if key == "" {
		return Record{}, ErrMissingKey
	}
	if vin == "" || !s.vehicles.Has(vin) {
		return Record{}, ErrUnknownVIN
	}

	s.mu.Lock()
	if id, ok := s.byKey[idempotencyKey(vin, ActionUnlock, key)]; ok {
		rec := *s.records[id]
		s.mu.Unlock()
		return rec, nil
	}
	if id, ok := s.inFlight[flightKey(vin, ActionUnlock)]; ok {
		rec := *s.records[id]
		s.mu.Unlock()
		return Record{}, &ConflictError{Current: rec}
	}

	rec := &Record{
		ID:            s.newID(),
		VIN:           vin,
		Action:        ActionUnlock,
		State:         StatePending,
		CorrelationID: s.newID(),
		Key:           key,
	}
	s.records[rec.ID] = rec
	s.byKey[idempotencyKey(vin, ActionUnlock, key)] = rec.ID
	s.inFlight[flightKey(vin, ActionUnlock)] = rec.ID
	accepted := *rec
	s.log.Info("unlock accepted",
		"correlationId", rec.CorrelationID,
		"commandId", rec.ID,
		"vin", vin,
		"state", rec.State,
	)
	s.mu.Unlock()
	s.notify(accepted)

	payload, err := json.Marshal(wireCommand{
		ID:            accepted.ID,
		Action:        ActionUnlock,
		CorrelationID: accepted.CorrelationID,
	})
	if err != nil {
		return s.failPublish(rec, fmt.Errorf("encode command: %w", err))
	}

	pubCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), publishTimeout)
	defer cancel()
	if err := s.pub.Publish(pubCtx, Topic(vin), payload); err != nil {
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
		"vin", vin,
		"state", rec.State,
	)
	s.mu.Unlock()
	s.notify(sent)
	return sent, nil
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

// Apply records a device ack. Unknown or malformed ids are ignored.
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
	delete(s.inFlight, flightKey(rec.VIN, rec.Action))
	updated := *rec
	s.log.Info("command ack",
		"correlationId", rec.CorrelationID,
		"commandId", rec.ID,
		"vin", rec.VIN,
		"state", rec.State,
	)
	s.mu.Unlock()
	s.notify(updated)
}

// Expire moves SENT commands past Expiry to TIMEOUT.
func (s *Service) Expire() {
	s.mu.Lock()
	now := s.clock.Now()
	changed := make([]Record, 0)
	for _, rec := range s.records {
		if rec.State != StateSent || rec.SentAt.IsZero() {
			continue
		}
		if now.Sub(rec.SentAt) < Expiry {
			continue
		}
		rec.State = StateTimeout
		delete(s.inFlight, flightKey(rec.VIN, rec.Action))
		changed = append(changed, *rec)
		s.log.Info("command timeout",
			"correlationId", rec.CorrelationID,
			"commandId", rec.ID,
			"vin", rec.VIN,
			"state", rec.State,
		)
	}
	s.mu.Unlock()
	for _, rec := range changed {
		s.notify(rec)
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
	if rec.State == StatePending {
		rec.State = StateFailed
		delete(s.inFlight, flightKey(rec.VIN, rec.Action))
	}
	failed := *rec
	s.mu.Unlock()
	s.notify(failed)
	return failed, err
}

func (s *Service) notify(rec Record) {
	s.mu.Lock()
	fn := s.listener
	s.mu.Unlock()
	if fn != nil {
		fn(rec)
	}
}

func idempotencyKey(vin, action, key string) string {
	return vin + "\x00" + action + "\x00" + key
}

func flightKey(vin, action string) string {
	return vin + "\x00" + action
}

func randomID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}
