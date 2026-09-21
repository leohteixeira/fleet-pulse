package block

import (
	"context"
	"time"
)

// Tick advances REQUESTED→ARMED, ARMED→SENT|TIMEOUT, and pending unlocks.
func (s *Service) Tick(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	now := s.clock.Now()

	s.mu.Lock()
	ids := make([]string, 0, len(s.records))
	for id := range s.records {
		ids = append(ids, id)
	}
	s.mu.Unlock()

	var first error
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.tickOne(ctx, id, now); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// Run ticks until ctx is cancelled.
func (s *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.Tick(ctx); err != nil {
				s.log.Error("leasing block tick failed", "err", err)
			}
		}
	}
}

func (s *Service) tickOne(ctx context.Context, id string, now time.Time) error {
	s.mu.Lock()
	rec, ok := s.clone(id)
	last, hasTelem := s.telem[rec.VIN]
	live := s.live()
	s.mu.Unlock()
	if !ok {
		return nil
	}

	switch rec.Action {
	case ActionBlock:
		return s.tickBlock(ctx, rec, last, hasTelem, live, now)
	case ActionUnlock:
		return s.tickUnlock(ctx, rec, last, hasTelem, now)
	default:
		return nil
	}
}

func (s *Service) tickBlock(ctx context.Context, rec Record, last telem, hasTelem, live bool, now time.Time) error {
	switch rec.State {
	case StateRequested:
		rec.State = StateArmed
		rec.UpdatedAt = now
		if err := s.commit(ctx, rec, nil); err != nil {
			return err
		}
		s.logChange("leasing command armed", rec)
		return nil
	case StateArmed:
		online := isOnline(last, hasTelem, now)
		if !online && armedOffline(rec, last, hasTelem, now) {
			rec.State = StateTimeout
			rec.UpdatedAt = now
			if err := s.commit(ctx, rec, nil); err != nil {
				return err
			}
			s.logChange("leasing command timeout", rec)
			return nil
		}
		stopped := last.Speed == 0 && !last.Ignition
		if !online || !stopped || !live {
			return nil
		}
		return s.send(ctx, rec, now)
	default:
		return nil
	}
}

func (s *Service) tickUnlock(ctx context.Context, rec Record, last telem, hasTelem bool, now time.Time) error {
	if rec.State != StateRequested {
		return nil
	}
	if !isOnline(last, hasTelem, now) {
		return nil
	}
	return s.send(ctx, rec, now)
}

func (s *Service) send(ctx context.Context, rec Record, now time.Time) error {
	payload, err := encodeCommand(rec)
	if err != nil {
		return err
	}
	rec.State = StateSent
	rec.SentAt = now
	rec.UpdatedAt = now
	outbox := &OutboxEntry{Topic: Topic(rec.VIN), Payload: payload}
	if err := s.commit(ctx, rec, outbox); err != nil {
		return err
	}
	s.logChange("leasing command sent", rec)
	return nil
}

func (s *Service) commit(ctx context.Context, rec Record, outbox *OutboxEntry) error {
	if err := s.persist(ctx, PersistWrite{
		Command: rec,
		SentAt:  rec.SentAt,
		Audit:   auditOf(rec),
		Outbox:  outbox,
	}); err != nil {
		return err
	}
	s.replace(rec)
	return nil
}

func isOnline(last telem, hasTelem bool, now time.Time) bool {
	if !hasTelem || last.SeenAt.IsZero() {
		return false
	}
	return now.Sub(last.SeenAt) <= ArmedTimeout
}

func armedOffline(rec Record, last telem, hasTelem bool, now time.Time) bool {
	if isOnline(last, hasTelem, now) {
		return false
	}
	lastSeen := rec.UpdatedAt
	if lastSeen.IsZero() {
		lastSeen = rec.CreatedAt
	}
	if hasTelem && last.SeenAt.After(lastSeen) {
		lastSeen = last.SeenAt
	}
	if lastSeen.IsZero() {
		return false
	}
	return now.Sub(lastSeen) > ArmedTimeout
}
