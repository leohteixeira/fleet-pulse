package block

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

const testVIN = "FPULSELSG00000001"

type stubClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *stubClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *stubClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

type memStore struct {
	mu          sync.Mutex
	vins        map[string]bool
	cmds        map[string]Record
	acked       []Record
	outbox      []OutboxEntry
	telem       map[string]LastKnown
	failPersist bool
}

func newMemStore(vins ...string) *memStore {
	m := &memStore{
		vins:  make(map[string]bool, len(vins)),
		cmds:  make(map[string]Record),
		telem: make(map[string]LastKnown),
	}
	for _, vin := range vins {
		m.vins[vin] = true
	}
	return m
}

func (m *memStore) Persist(_ context.Context, w PersistWrite) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failPersist {
		return errors.New("persist failed")
	}
	m.cmds[w.Command.ID] = w.Command
	if w.Outbox != nil {
		m.outbox = append(m.outbox, *w.Outbox)
	}
	return nil
}

func (m *memStore) GetCommand(_ context.Context, id string) (Record, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.cmds[id]
	return rec, ok, nil
}

func (m *memStore) ListActive(context.Context) ([]Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Record, 0, len(m.cmds))
	for _, rec := range m.cmds {
		switch rec.State {
		case StateRequested, StateArmed, StateSent:
			out = append(out, rec)
		}
	}
	return out, nil
}

func (m *memStore) ListLatestAcked(context.Context) ([]Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Record(nil), m.acked...), nil
}

func (m *memStore) HasVIN(_ context.Context, vin string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.vins[vin], nil
}

func (m *memStore) UpsertTelem(_ context.Context, t LastKnown) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.telem[t.VIN] = t
	return nil
}

func (m *memStore) topics() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.outbox))
	for _, row := range m.outbox {
		out = append(out, row.Topic)
	}
	return out
}

func parked(vin string) LastKnown {
	return LastKnown{VIN: vin, Speed: 0, Ignition: false}
}

func moving(vin string) LastKnown {
	return LastKnown{VIN: vin, Speed: 32, Ignition: true}
}

func newTestService(t *testing.T, store *memStore, clk *stubClock) *Service {
	t.Helper()
	return New(store, nil, WithClock(clk))
}

func TestService_ArmThenSend(t *testing.T) {
	t.Parallel()

	st := newMemStore(testVIN)
	clk := &stubClock{now: time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)}
	svc := newTestService(t, st, clk)

	rec, err := svc.Request(t.Context(), testVIN)
	if err != nil {
		t.Fatalf("Request() error = %v", err)
	}
	if rec.State != StateRequested {
		t.Fatalf("after request state = %q, want REQUESTED", rec.State)
	}

	svc.NoteTelem(parked(testVIN))
	if err := svc.Tick(t.Context()); err != nil {
		t.Fatalf("first Tick() error = %v", err)
	}
	rec, ok := svc.Get(rec.ID)
	if !ok || rec.State != StateArmed {
		t.Fatalf("after first tick state = %+v, want ARMED", rec)
	}
	if got := st.topics(); len(got) != 0 {
		t.Fatalf("outbox after arm = %v, want empty", got)
	}

	if err := svc.Tick(t.Context()); err != nil {
		t.Fatalf("second Tick() error = %v", err)
	}
	rec, _ = svc.Get(rec.ID)
	if rec.State != StateSent {
		t.Fatalf("after second tick state = %q, want SENT", rec.State)
	}
	topics := st.topics()
	wantTopic := Topic(testVIN)
	if len(topics) != 1 || topics[0] != wantTopic {
		t.Fatalf("outbox topics = %v, want [%q]", topics, wantTopic)
	}
	if !strings.HasPrefix(wantTopic, "leasing/") {
		t.Fatalf("topic %q is not leasing", wantTopic)
	}

	svc.ApplyAck(rec.ID, true)
	rec, _ = svc.Get(rec.ID)
	if rec.State != StateAcked {
		t.Fatalf("after ack state = %q, want ACKED", rec.State)
	}
	if !svc.Blocked(testVIN) {
		t.Fatal("vehicle not blocked after ACKED block")
	}
}

func TestService_MovingStaysArmed(t *testing.T) {
	t.Parallel()

	st := newMemStore(testVIN)
	clk := &stubClock{now: time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)}
	svc := newTestService(t, st, clk)

	rec, err := svc.Request(t.Context(), testVIN)
	if err != nil {
		t.Fatalf("Request() error = %v", err)
	}
	svc.NoteTelem(moving(testVIN))
	if err := svc.Tick(t.Context()); err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if err := svc.Tick(t.Context()); err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	rec, _ = svc.Get(rec.ID)
	if rec.State != StateArmed {
		t.Fatalf("state = %q, want ARMED while moving", rec.State)
	}
	if got := st.topics(); len(got) != 0 {
		t.Fatalf("outbox = %v, want empty while moving", got)
	}
}

func TestService_ArmedTimeout(t *testing.T) {
	t.Parallel()

	st := newMemStore(testVIN)
	clk := &stubClock{now: time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)}
	svc := newTestService(t, st, clk)

	rec, err := svc.Request(t.Context(), testVIN)
	if err != nil {
		t.Fatalf("Request() error = %v", err)
	}
	if err := svc.Tick(t.Context()); err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	clk.advance(ArmedTimeout + time.Second)
	if err := svc.Tick(t.Context()); err != nil {
		t.Fatalf("timeout Tick() error = %v", err)
	}
	rec, _ = svc.Get(rec.ID)
	if rec.State != StateTimeout {
		t.Fatalf("state = %q, want TIMEOUT", rec.State)
	}
	if got := st.topics(); len(got) != 0 {
		t.Fatalf("outbox after timeout = %v, want empty", got)
	}
	if svc.Blocked(testVIN) {
		t.Fatal("vehicle blocked after TIMEOUT")
	}
}

func TestService_CancelArmedAndRefuseSent(t *testing.T) {
	t.Parallel()

	st := newMemStore(testVIN)
	clk := &stubClock{now: time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)}
	svc := newTestService(t, st, clk)

	rec, err := svc.Request(t.Context(), testVIN)
	if err != nil {
		t.Fatalf("Request() error = %v", err)
	}
	cancelled, err := svc.Cancel(t.Context(), rec.ID)
	if err != nil {
		t.Fatalf("Cancel(REQUESTED) error = %v", err)
	}
	if cancelled.State != StateCancelled {
		t.Fatalf("cancel requested state = %q, want CANCELLED", cancelled.State)
	}
	if svc.Blocked(testVIN) {
		t.Fatal("vehicle blocked after cancel")
	}

	rec, err = svc.Request(t.Context(), testVIN)
	if err != nil {
		t.Fatalf("second Request() error = %v", err)
	}
	svc.NoteTelem(parked(testVIN))
	_ = svc.Tick(t.Context())
	armed, _ := svc.Get(rec.ID)
	if armed.State != StateArmed {
		t.Fatalf("state = %q, want ARMED", armed.State)
	}
	cancelled, err = svc.Cancel(t.Context(), rec.ID)
	if err != nil || cancelled.State != StateCancelled {
		t.Fatalf("Cancel(ARMED) = %+v err=%v, want CANCELLED", cancelled, err)
	}

	rec, err = svc.Request(t.Context(), testVIN)
	if err != nil {
		t.Fatalf("third Request() error = %v", err)
	}
	svc.NoteTelem(parked(testVIN))
	_ = svc.Tick(t.Context())
	_ = svc.Tick(t.Context())
	sent, _ := svc.Get(rec.ID)
	if sent.State != StateSent {
		t.Fatalf("state = %q, want SENT", sent.State)
	}
	_, err = svc.Cancel(t.Context(), rec.ID)
	if !errors.Is(err, ErrInTransit) {
		t.Fatalf("Cancel(SENT) error = %v, want ErrInTransit", err)
	}
}

func TestService_DeviceRefuse(t *testing.T) {
	t.Parallel()

	st := newMemStore(testVIN)
	clk := &stubClock{now: time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)}
	svc := newTestService(t, st, clk)

	rec, err := svc.Request(t.Context(), testVIN)
	if err != nil {
		t.Fatalf("Request() error = %v", err)
	}
	svc.NoteTelem(parked(testVIN))
	_ = svc.Tick(t.Context())
	_ = svc.Tick(t.Context())
	svc.ApplyAck(rec.ID, false)
	rec, _ = svc.Get(rec.ID)
	if rec.State != StateFailed {
		t.Fatalf("state = %q, want FAILED", rec.State)
	}
	if svc.Blocked(testVIN) {
		t.Fatal("vehicle blocked after refused ack")
	}
}

func TestService_OfflineUnlockThenOnline(t *testing.T) {
	t.Parallel()

	st := newMemStore(testVIN)
	clk := &stubClock{now: time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)}
	svc := newTestService(t, st, clk)

	blockRec, err := svc.Request(t.Context(), testVIN)
	if err != nil {
		t.Fatalf("Request() error = %v", err)
	}
	svc.NoteTelem(parked(testVIN))
	_ = svc.Tick(t.Context())
	_ = svc.Tick(t.Context())
	svc.ApplyAck(blockRec.ID, true)
	if !svc.Blocked(testVIN) {
		t.Fatal("want blocked before unlock")
	}
	st.mu.Lock()
	st.outbox = nil
	st.mu.Unlock()

	clk.advance(ArmedTimeout + time.Second)
	unlockRec, err := svc.Unlock(t.Context(), testVIN)
	if err != nil {
		t.Fatalf("Unlock() error = %v", err)
	}
	if unlockRec.State != StateRequested {
		t.Fatalf("unlock state = %q, want REQUESTED", unlockRec.State)
	}
	if err := svc.Tick(t.Context()); err != nil {
		t.Fatalf("offline Tick() error = %v", err)
	}
	unlockRec, _ = svc.Get(unlockRec.ID)
	if unlockRec.State != StateRequested {
		t.Fatalf("offline unlock state = %q, want REQUESTED", unlockRec.State)
	}
	if got := st.topics(); len(got) != 0 {
		t.Fatalf("outbox while offline = %v, want empty", got)
	}
	if !svc.Blocked(testVIN) {
		t.Fatal("persist-pending unlock cleared blocked")
	}

	svc.NoteTelem(parked(testVIN))
	if err := svc.Tick(t.Context()); err != nil {
		t.Fatalf("online Tick() error = %v", err)
	}
	unlockRec, _ = svc.Get(unlockRec.ID)
	if unlockRec.State != StateSent {
		t.Fatalf("online unlock state = %q, want SENT", unlockRec.State)
	}
	if got := st.topics(); len(got) != 1 || got[0] != Topic(testVIN) {
		t.Fatalf("unlock outbox = %v, want [%q]", got, Topic(testVIN))
	}

	svc.ApplyAck(unlockRec.ID, true)
	if svc.Blocked(testVIN) {
		t.Fatal("vehicle still blocked after unlock ack")
	}
}

func TestService_PersistFailDoesNotPublish(t *testing.T) {
	t.Parallel()

	st := newMemStore(testVIN)
	st.failPersist = true
	clk := &stubClock{now: time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)}
	svc := newTestService(t, st, clk)

	rec, err := svc.Request(t.Context(), testVIN)
	if err == nil {
		t.Fatalf("Request() succeeded with %+v, want persist error", rec)
	}
	if rec.ID != "" {
		t.Fatalf("failed request returned id %q", rec.ID)
	}
	if got := st.topics(); len(got) != 0 {
		t.Fatalf("outbox after persist fail = %v, want empty", got)
	}
}

func TestService_LiveGatesSend(t *testing.T) {
	t.Parallel()

	st := newMemStore(testVIN)
	clk := &stubClock{now: time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)}
	svc := New(st, nil, WithClock(clk), WithLive(func() bool { return false }))

	rec, err := svc.Request(t.Context(), testVIN)
	if err != nil {
		t.Fatalf("Request() error = %v", err)
	}
	svc.NoteTelem(parked(testVIN))
	_ = svc.Tick(t.Context())
	_ = svc.Tick(t.Context())
	rec, _ = svc.Get(rec.ID)
	if rec.State != StateArmed {
		t.Fatalf("degraded live state = %q, want ARMED", rec.State)
	}
	if got := st.topics(); len(got) != 0 {
		t.Fatalf("outbox while not live = %v, want empty", got)
	}
}

func TestService_GetUnknown(t *testing.T) {
	t.Parallel()

	svc := New(newMemStore(), nil)
	if _, ok := svc.Get("missing"); ok {
		t.Fatal("Get(missing) found a record")
	}
}

func TestService_LoadHydratesInFlightAndAcked(t *testing.T) {
	t.Parallel()

	st := newMemStore(testVIN)
	ok, err := st.HasVIN(t.Context(), testVIN)
	if err != nil || !ok {
		t.Fatalf("HasVIN(%q) = %v, %v, want true", testVIN, ok, err)
	}
	ok, err = st.HasVIN(t.Context(), "UNKNOWN")
	if err != nil || ok {
		t.Fatalf("HasVIN(UNKNOWN) = %v, %v, want false", ok, err)
	}

	st.cmds["armed-1"] = Record{
		ID:            "armed-1",
		VIN:           testVIN,
		Action:        ActionBlock,
		State:         StateArmed,
		CorrelationID: "corr-armed",
	}
	st.acked = []Record{{
		ID:     "acked-1",
		VIN:    testVIN,
		Action: ActionBlock,
		State:  StateAcked,
	}}

	svc := New(st, nil)
	if err := svc.Load(t.Context()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	got, found := svc.Get("armed-1")
	if !found || got.State != StateArmed {
		t.Fatalf("in-flight after Load = %+v found=%v, want ARMED", got, found)
	}
	if !svc.Blocked(testVIN) {
		t.Fatal("ACKED block not hydrated")
	}
}

func TestService_UnknownVIN(t *testing.T) {
	t.Parallel()

	svc := New(newMemStore(), nil)
	if _, err := svc.Request(t.Context(), testVIN); !errors.Is(err, ErrUnknownVIN) {
		t.Fatalf("Request(unknown) error = %v, want ErrUnknownVIN", err)
	}
	if _, err := svc.Unlock(t.Context(), testVIN); !errors.Is(err, ErrUnknownVIN) {
		t.Fatalf("Unlock(unknown) error = %v, want ErrUnknownVIN", err)
	}
}

func TestService_SendPersistFailDoesNotPublish(t *testing.T) {
	t.Parallel()

	st := newMemStore(testVIN)
	clk := &stubClock{now: time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)}
	svc := newTestService(t, st, clk)

	rec, err := svc.Request(t.Context(), testVIN)
	if err != nil {
		t.Fatalf("Request() error = %v", err)
	}
	svc.NoteTelem(parked(testVIN))
	if err := svc.Tick(t.Context()); err != nil {
		t.Fatalf("arm Tick() error = %v", err)
	}
	armed, _ := svc.Get(rec.ID)
	if armed.State != StateArmed {
		t.Fatalf("after arm state = %q, want ARMED", armed.State)
	}

	st.failPersist = true
	if err := svc.Tick(t.Context()); err == nil {
		t.Fatal("SENT persist Tick() succeeded, want error")
	}
	rec, _ = svc.Get(rec.ID)
	if rec.State != StateArmed {
		t.Fatalf("after send persist fail state = %q, want ARMED", rec.State)
	}
	if got := st.topics(); len(got) != 0 {
		t.Fatalf("outbox after send persist fail = %v, want empty", got)
	}
}

func TestService_RefuseSecondRequest(t *testing.T) {
	t.Parallel()

	st := newMemStore(testVIN)
	clk := &stubClock{now: time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)}
	svc := newTestService(t, st, clk)

	rec, err := svc.Request(t.Context(), testVIN)
	if err != nil {
		t.Fatalf("Request() error = %v", err)
	}
	if second, err := svc.Request(t.Context(), testVIN); !errors.Is(err, ErrInTransit) {
		t.Fatalf("second in-flight Request() = %+v err=%v, want ErrInTransit", second, err)
	}
	if got := st.topics(); len(got) != 0 {
		t.Fatalf("outbox after refused request = %v, want empty", got)
	}

	svc.NoteTelem(parked(testVIN))
	_ = svc.Tick(t.Context())
	_ = svc.Tick(t.Context())
	svc.ApplyAck(rec.ID, true)
	if !svc.Blocked(testVIN) {
		t.Fatal("want blocked before second Request")
	}
	if second, err := svc.Request(t.Context(), testVIN); !errors.Is(err, ErrInTransit) {
		t.Fatalf("Request while blocked = %+v err=%v, want ErrInTransit", second, err)
	}
}

func TestService_FailedAckDoesNotUnblock(t *testing.T) {
	t.Parallel()

	st := newMemStore(testVIN)
	clk := &stubClock{now: time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)}
	svc := newTestService(t, st, clk)

	rec, err := svc.Request(t.Context(), testVIN)
	if err != nil {
		t.Fatalf("Request() error = %v", err)
	}
	svc.NoteTelem(parked(testVIN))
	_ = svc.Tick(t.Context())
	_ = svc.Tick(t.Context())
	svc.ApplyAck(rec.ID, true)
	if !svc.Blocked(testVIN) {
		t.Fatal("want blocked after ACKED")
	}

	const laterID = "later-fail"
	svc.mu.Lock()
	svc.records[laterID] = &Record{
		ID:     laterID,
		VIN:    testVIN,
		Action: ActionBlock,
		State:  StateSent,
	}
	svc.mu.Unlock()
	svc.ApplyAck(laterID, false)

	later, ok := svc.Get(laterID)
	if !ok || later.State != StateFailed {
		t.Fatalf("later ack = %+v found=%v, want FAILED", later, ok)
	}
	if !svc.Blocked(testVIN) {
		t.Fatal("FAILED ack unblocked a prior ACKED VIN")
	}
}

func TestService_RefuseSecondUnlock(t *testing.T) {
	t.Parallel()

	st := newMemStore(testVIN)
	clk := &stubClock{now: time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)}
	svc := newTestService(t, st, clk)

	blockRec, err := svc.Request(t.Context(), testVIN)
	if err != nil {
		t.Fatalf("Request() error = %v", err)
	}
	svc.NoteTelem(parked(testVIN))
	_ = svc.Tick(t.Context())
	_ = svc.Tick(t.Context())
	svc.ApplyAck(blockRec.ID, true)

	if _, err := svc.Unlock(t.Context(), testVIN); err != nil {
		t.Fatalf("Unlock() error = %v", err)
	}
	if rec, err := svc.Unlock(t.Context(), testVIN); !errors.Is(err, ErrInTransit) {
		t.Fatalf("second Unlock() = %+v err=%v, want ErrInTransit", rec, err)
	}
}

func TestService_UnlockStaleAfterThirtySimDays(t *testing.T) {
	t.Parallel()

	st := newMemStore(testVIN)
	real0 := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	clk := &stubClock{now: real0}
	cal := calendarClock{
		sim:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		real: real0,
		mult: 14400,
	}
	svc := New(st, nil, WithClock(clk), WithCalendar(&cal))

	rec, err := svc.Request(t.Context(), testVIN)
	if err != nil {
		t.Fatalf("Request() error = %v", err)
	}
	svc.NoteTelem(parked(testVIN))
	if err := svc.Tick(t.Context()); err != nil {
		t.Fatalf("arm: %v", err)
	}
	if err := svc.Tick(t.Context()); err != nil {
		t.Fatalf("send: %v", err)
	}
	svc.ApplyAck(rec.ID, true)
	if !svc.Blocked(testVIN) {
		t.Fatal("want blocked after ack")
	}

	cal.real = real0.Add(181 * time.Second)
	cal.sim = cal.sim.Add(181 * 14400 * time.Second)
	cutoff := cal.Simulated().Add(-30 * 24 * time.Hour)
	if err := svc.UnlockStale(t.Context(), cutoff); err != nil {
		t.Fatalf("UnlockStale: %v", err)
	}
	if svc.VehicleState(testVIN) != VehicleUnlockPending && svc.Blocked(testVIN) {
		t.Fatalf("state = %s blocked=%v, want unlock pending or unblocked", svc.VehicleState(testVIN), svc.Blocked(testVIN))
	}
}

func TestService_UnlockStaleKeepsRecentBlock(t *testing.T) {
	t.Parallel()

	st := newMemStore(testVIN)
	real0 := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	clk := &stubClock{now: real0}
	cal := calendarClock{
		sim:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		real: real0,
		mult: 14400,
	}
	svc := New(st, nil, WithClock(clk), WithCalendar(&cal))
	rec, err := svc.Request(t.Context(), testVIN)
	if err != nil {
		t.Fatalf("Request() error = %v", err)
	}
	svc.NoteTelem(parked(testVIN))
	if err := svc.Tick(t.Context()); err != nil {
		t.Fatalf("arm: %v", err)
	}
	if err := svc.Tick(t.Context()); err != nil {
		t.Fatalf("send: %v", err)
	}
	svc.ApplyAck(rec.ID, true)

	cal.real = real0.Add(174 * time.Second)
	cal.sim = cal.sim.Add(174 * 14400 * time.Second)
	cutoff := cal.Simulated().Add(-30 * 24 * time.Hour)
	if err := svc.UnlockStale(t.Context(), cutoff); err != nil {
		t.Fatalf("UnlockStale: %v", err)
	}
	if !svc.Blocked(testVIN) {
		t.Fatal("29 simulated days must stay blocked")
	}
}

func TestService_UnlockStaleWithoutCalendarIsNoop(t *testing.T) {
	t.Parallel()

	st := newMemStore(testVIN)
	real0 := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	svc := New(st, nil, WithClock(&stubClock{now: real0}))
	rec, err := svc.Request(t.Context(), testVIN)
	if err != nil {
		t.Fatalf("Request() error = %v", err)
	}
	svc.NoteTelem(parked(testVIN))
	if err := svc.Tick(t.Context()); err != nil {
		t.Fatalf("arm: %v", err)
	}
	if err := svc.Tick(t.Context()); err != nil {
		t.Fatalf("send: %v", err)
	}
	svc.ApplyAck(rec.ID, true)
	cutoff := time.Date(1999, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := svc.UnlockStale(t.Context(), cutoff); err != nil {
		t.Fatalf("UnlockStale: %v", err)
	}
	if !svc.Blocked(testVIN) {
		t.Fatal("without calendar, UnlockStale must not unlock")
	}
}

type calendarClock struct {
	sim  time.Time
	real time.Time
	mult int
}

func (c *calendarClock) Simulated() time.Time { return c.sim }
func (c *calendarClock) Real() time.Time      { return c.real }
func (c *calendarClock) Multiplier() int      { return c.mult }

func TestService_TickAndTelemRace(t *testing.T) {
	t.Parallel()

	st := newMemStore(testVIN)
	clk := &stubClock{now: time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)}
	svc := newTestService(t, st, clk)
	if _, err := svc.Request(t.Context(), testVIN); err != nil {
		t.Fatalf("Request() error = %v", err)
	}

	var wg sync.WaitGroup
	ctx, cancel := context.WithCancel(t.Context())
	wg.Go(func() {
		for ctx.Err() == nil {
			_ = svc.Tick(ctx)
		}
	})
	wg.Go(func() {
		for ctx.Err() == nil {
			svc.NoteTelem(parked(testVIN))
		}
	})
	time.Sleep(20 * time.Millisecond)
	cancel()
	wg.Wait()
}
