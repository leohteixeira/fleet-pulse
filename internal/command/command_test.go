package command

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestService_UnlockPublishAndAck(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	pub := &fakePublisher{}
	svc := newTestService(t, pub, vehicles{"FPULSESAO00000001": {}}, &buf)

	rec, err := svc.Unlock(t.Context(), "FPULSESAO00000001", "key-1")
	if err != nil {
		t.Fatalf("Unlock() error = %v", err)
	}
	if rec.ID == "" || rec.State != StateSent {
		t.Fatalf("record = %+v, want id set and state SENT", rec)
	}
	if rec.Action != ActionUnlock || rec.CorrelationID == "" {
		t.Fatalf("record = %+v, want action unlock and correlation id", rec)
	}
	if pub.calls() != 1 {
		t.Fatalf("publishes = %d, want 1", pub.calls())
	}
	if pub.lastTopic() != Topic("FPULSESAO00000001") {
		t.Fatalf("topic = %q, want %q", pub.lastTopic(), Topic("FPULSESAO00000001"))
	}
	var payload map[string]any
	if err := json.Unmarshal(pub.lastPayload(), &payload); err != nil {
		t.Fatalf("payload json: %v", err)
	}
	if payload["id"] != rec.ID || payload["action"] != ActionUnlock || payload["correlationId"] != rec.CorrelationID {
		t.Fatalf("payload = %s, want id/action/correlationId", pub.lastPayload())
	}

	svc.Apply(rec.ID, true)
	got, ok := svc.Get(rec.ID)
	if !ok || got.State != StateAcked {
		t.Fatalf("after ack = %+v ok=%v, want ACKED", got, ok)
	}

	logs := buf.String()
	for _, want := range []string{`"msg":"command accepted"`, `"msg":"command published"`, `"msg":"command ack"`} {
		if !strings.Contains(logs, want) {
			t.Fatalf("log missing %s in %s", want, logs)
		}
	}
	corr := `"correlationId":"` + rec.CorrelationID + `"`
	if strings.Count(logs, corr) < 3 {
		t.Fatalf("correlation id not shared across accept/publish/ack in %s", logs)
	}
}

func TestService_ReplaySameKey(t *testing.T) {
	t.Parallel()

	pub := &fakePublisher{}
	svc := newTestService(t, pub, vehicles{"FPULSESAO00000001": {}}, nil)

	first, err := svc.Unlock(t.Context(), "FPULSESAO00000001", "same")
	if err != nil {
		t.Fatalf("first Unlock() error = %v", err)
	}
	second, err := svc.Unlock(t.Context(), "FPULSESAO00000001", "same")
	if err != nil {
		t.Fatalf("replay Unlock() error = %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("replay id = %q, want %q", second.ID, first.ID)
	}
	if second.State != first.State {
		t.Fatalf("replay state = %q, want %q", second.State, first.State)
	}
	if pub.calls() != 1 {
		t.Fatalf("publishes = %d, want 1 (no second publish)", pub.calls())
	}
}

func TestService_TimeoutFromSent(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	clock := &stubClock{now: time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)}
	pub := &fakePublisher{}
	svc := newTestService(t, pub, vehicles{"FPULSESAO00000020": {}}, &buf)
	svc.clock = clock

	rec, err := svc.Unlock(t.Context(), "FPULSESAO00000020", "offline")
	if err != nil {
		t.Fatalf("Unlock() error = %v", err)
	}
	if rec.State != StateSent {
		t.Fatalf("state = %q, want SENT", rec.State)
	}

	clock.now = clock.now.Add(Expiry - time.Nanosecond)
	svc.Expire()
	got, _ := svc.Get(rec.ID)
	if got.State != StateSent {
		t.Fatalf("before expiry state = %q, want SENT", got.State)
	}

	clock.now = clock.now.Add(time.Nanosecond)
	svc.Expire()
	got, _ = svc.Get(rec.ID)
	if got.State != StateTimeout {
		t.Fatalf("after expiry state = %q, want TIMEOUT", got.State)
	}

	logs := buf.String()
	if !strings.Contains(logs, `"msg":"command timeout"`) {
		t.Fatalf("log missing timeout in %s", logs)
	}
	if !strings.Contains(logs, `"correlationId":"`+rec.CorrelationID+`"`) {
		t.Fatalf("timeout log missing correlation id in %s", logs)
	}
}

func TestService_RefuseAck(t *testing.T) {
	t.Parallel()

	svc := newTestService(t, &fakePublisher{}, vehicles{"FPULSESAO00000001": {}}, nil)
	rec, err := svc.Unlock(t.Context(), "FPULSESAO00000001", "refuse")
	if err != nil {
		t.Fatalf("Unlock() error = %v", err)
	}

	svc.Apply(rec.ID, false)
	got, ok := svc.Get(rec.ID)
	if !ok || got.State != StateFailed {
		t.Fatalf("after refuse = %+v ok=%v, want FAILED", got, ok)
	}

	svc.Apply(rec.ID, true)
	got, _ = svc.Get(rec.ID)
	if got.State != StateFailed {
		t.Fatalf("terminal apply changed state to %q", got.State)
	}
}

func TestService_Lock(t *testing.T) {
	t.Parallel()

	pub := &fakePublisher{}
	svc := newTestService(t, pub, vehicles{"FPULSESAO00000001": {}}, nil)

	rec, err := svc.Lock(t.Context(), "FPULSESAO00000001", "lock-1")
	if err != nil {
		t.Fatalf("Lock() error = %v", err)
	}
	if rec.Action != ActionLock || rec.State != StateSent {
		t.Fatalf("record = %+v, want action lock and state SENT", rec)
	}
	var payload map[string]any
	if err := json.Unmarshal(pub.lastPayload(), &payload); err != nil {
		t.Fatalf("payload json: %v", err)
	}
	if payload["action"] != ActionLock || payload["id"] != rec.ID {
		t.Fatalf("payload = %s, want lock id", pub.lastPayload())
	}

	svc.Apply(rec.ID, true)
	got, ok := svc.Get(rec.ID)
	if !ok || got.State != StateAcked {
		t.Fatalf("after ack = %+v ok=%v, want ACKED", got, ok)
	}
}

func TestService_QueueAndThirdConflict(t *testing.T) {
	t.Parallel()

	pub := &fakePublisher{}
	svc := newTestService(t, pub, vehicles{"FPULSESAO00000001": {}}, nil)

	first, err := svc.Lock(t.Context(), "FPULSESAO00000001", "one")
	if err != nil {
		t.Fatalf("Lock() error = %v", err)
	}
	if first.State != StateSent {
		t.Fatalf("first state = %q, want SENT", first.State)
	}

	queued, err := svc.Unlock(t.Context(), "FPULSESAO00000001", "two")
	if err != nil {
		t.Fatalf("queued Unlock() error = %v, want 202 PENDING", err)
	}
	if queued.State != StatePending || queued.Action != ActionUnlock {
		t.Fatalf("queued = %+v, want PENDING unlock", queued)
	}
	if pub.calls() != 1 {
		t.Fatalf("publishes = %d, want 1 (successor stays unpublished)", pub.calls())
	}

	replay, err := svc.Unlock(t.Context(), "FPULSESAO00000001", "two")
	if err != nil {
		t.Fatalf("replay queued Unlock() error = %v", err)
	}
	if replay.ID != queued.ID {
		t.Fatalf("replay id = %q, want queued %q", replay.ID, queued.ID)
	}

	_, err = svc.Unlock(t.Context(), "FPULSESAO00000001", "three")
	var conflict *ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("third command error = %v, want ConflictError", err)
	}
	if conflict.Current.ID != first.ID {
		t.Fatalf("conflict id = %q, want in-flight %q", conflict.Current.ID, first.ID)
	}

	svc.Apply(first.ID, true)
	got, ok := svc.Get(queued.ID)
	if !ok || got.State != StateSent {
		t.Fatalf("successor after terminal = %+v ok=%v, want SENT", got, ok)
	}
	if pub.calls() != 2 {
		t.Fatalf("publishes = %d, want 2 after predecessor terminals", pub.calls())
	}

	var payload map[string]any
	if err := json.Unmarshal(pub.lastPayload(), &payload); err != nil {
		t.Fatalf("successor payload json: %v", err)
	}
	if payload["id"] != queued.ID || payload["action"] != ActionUnlock {
		t.Fatalf("successor payload = %s, want queued unlock", pub.lastPayload())
	}
}

func TestService_ReplayPerAction(t *testing.T) {
	t.Parallel()

	pub := &fakePublisher{}
	svc := newTestService(t, pub, vehicles{"FPULSESAO00000001": {}}, nil)

	unlock, err := svc.Unlock(t.Context(), "FPULSESAO00000001", "same")
	if err != nil {
		t.Fatalf("Unlock() error = %v", err)
	}
	lock, err := svc.Lock(t.Context(), "FPULSESAO00000001", "same")
	if err != nil {
		t.Fatalf("Lock() error = %v, want queued successor", err)
	}
	if lock.ID == unlock.ID {
		t.Fatal("same key reused across actions")
	}
	if lock.State != StatePending {
		t.Fatalf("lock state = %q, want PENDING", lock.State)
	}

	replayUnlock, err := svc.Unlock(t.Context(), "FPULSESAO00000001", "same")
	if err != nil {
		t.Fatalf("replay Unlock() error = %v", err)
	}
	if replayUnlock.ID != unlock.ID {
		t.Fatalf("replay unlock id = %q, want %q", replayUnlock.ID, unlock.ID)
	}
	replayLock, err := svc.Lock(t.Context(), "FPULSESAO00000001", "same")
	if err != nil {
		t.Fatalf("replay Lock() error = %v", err)
	}
	if replayLock.ID != lock.ID {
		t.Fatalf("replay lock id = %q, want %q", replayLock.ID, lock.ID)
	}
	if pub.calls() != 1 {
		t.Fatalf("publishes = %d, want 1 until successor is promoted", pub.calls())
	}
}

func TestService_QueuePromotesOnRefuse(t *testing.T) {
	t.Parallel()

	pub := &fakePublisher{}
	svc := newTestService(t, pub, vehicles{"FPULSESAO00000001": {}}, nil)

	first, err := svc.Unlock(t.Context(), "FPULSESAO00000001", "one")
	if err != nil {
		t.Fatalf("Unlock() error = %v", err)
	}
	queued, err := svc.Lock(t.Context(), "FPULSESAO00000001", "two")
	if err != nil {
		t.Fatalf("Lock() error = %v", err)
	}
	if pub.calls() != 1 {
		t.Fatalf("publishes = %d, want 1 before refuse", pub.calls())
	}

	svc.Apply(first.ID, false)
	got, ok := svc.Get(first.ID)
	if !ok || got.State != StateFailed {
		t.Fatalf("predecessor = %+v ok=%v, want FAILED", got, ok)
	}
	promoted, ok := svc.Get(queued.ID)
	if !ok || promoted.State != StateSent {
		t.Fatalf("successor = %+v ok=%v, want SENT", promoted, ok)
	}
	if pub.calls() != 2 {
		t.Fatalf("publishes = %d, want 2 after refuse promotion", pub.calls())
	}
}

func TestService_QueuePromotesOnTimeout(t *testing.T) {
	t.Parallel()

	clock := &stubClock{now: time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)}
	pub := &fakePublisher{}
	svc := newTestService(t, pub, vehicles{"FPULSESAO00000020": {}}, nil)
	svc.clock = clock

	first, err := svc.Unlock(t.Context(), "FPULSESAO00000020", "offline")
	if err != nil {
		t.Fatalf("Unlock() error = %v", err)
	}
	queued, err := svc.Lock(t.Context(), "FPULSESAO00000020", "next")
	if err != nil {
		t.Fatalf("Lock() error = %v", err)
	}

	clock.now = clock.now.Add(Expiry)
	svc.Expire()
	got, _ := svc.Get(first.ID)
	if got.State != StateTimeout {
		t.Fatalf("first state = %q, want TIMEOUT", got.State)
	}
	promoted, _ := svc.Get(queued.ID)
	if promoted.State != StateSent {
		t.Fatalf("successor state = %q, want SENT", promoted.State)
	}
	if pub.calls() != 2 {
		t.Fatalf("publishes = %d, want 2 after timeout promotion", pub.calls())
	}
}

func TestService_NewCommandAfterTerminal(t *testing.T) {
	t.Parallel()

	svc := newTestService(t, &fakePublisher{}, vehicles{"FPULSESAO00000001": {}}, nil)
	first, err := svc.Unlock(t.Context(), "FPULSESAO00000001", "one")
	if err != nil {
		t.Fatalf("Unlock() error = %v", err)
	}
	svc.Apply(first.ID, true)

	second, err := svc.Unlock(t.Context(), "FPULSESAO00000001", "two")
	if err != nil {
		t.Fatalf("Unlock after terminal error = %v", err)
	}
	if second.ID == first.ID {
		t.Fatal("new key after terminal reused the old id")
	}
	if second.State != StateSent {
		t.Fatalf("state = %q, want SENT", second.State)
	}
}

func TestService_MissingKey(t *testing.T) {
	t.Parallel()

	pub := &fakePublisher{}
	svc := newTestService(t, pub, vehicles{"FPULSESAO00000001": {}}, nil)
	_, err := svc.Unlock(t.Context(), "FPULSESAO00000001", "")
	if !errors.Is(err, ErrMissingKey) {
		t.Fatalf("Unlock() error = %v, want ErrMissingKey", err)
	}
	if pub.calls() != 0 {
		t.Fatalf("publishes = %d, want 0", pub.calls())
	}
}

func TestService_UnknownVIN(t *testing.T) {
	t.Parallel()

	svc := newTestService(t, &fakePublisher{}, vehicles{"FPULSESAO00000001": {}}, nil)
	_, err := svc.Unlock(t.Context(), "FPULSESAO99999999", "key")
	if !errors.Is(err, ErrUnknownVIN) {
		t.Fatalf("Unlock() error = %v, want ErrUnknownVIN", err)
	}
}

func TestService_PublishErrorKeepsID(t *testing.T) {
	t.Parallel()

	pub := &fakePublisher{err: errors.New("broker down")}
	svc := newTestService(t, pub, vehicles{"FPULSESAO00000001": {}}, nil)

	first, err := svc.Unlock(t.Context(), "FPULSESAO00000001", "retry")
	if err == nil {
		t.Fatal("Unlock() error = nil, want publish error")
	}
	if first.ID == "" || first.State != StateFailed {
		t.Fatalf("after publish error = %+v, want FAILED with id", first)
	}

	pub.err = nil
	replay, err := svc.Unlock(t.Context(), "FPULSESAO00000001", "retry")
	if err != nil {
		t.Fatalf("replay Unlock() error = %v", err)
	}
	if replay.ID != first.ID {
		t.Fatalf("replay id = %q, want %q", replay.ID, first.ID)
	}
	if replay.State != StateFailed {
		t.Fatalf("replay state = %q, want FAILED", replay.State)
	}
	if pub.calls() != 1 {
		t.Fatalf("publishes = %d, want 1 (replay does not publish)", pub.calls())
	}

	second, err := svc.Unlock(t.Context(), "FPULSESAO00000001", "other")
	if err != nil {
		t.Fatalf("new key after failure error = %v", err)
	}
	if second.ID == first.ID {
		t.Fatal("new key after failure reused the old id")
	}
	if second.State != StateSent {
		t.Fatalf("new key state = %q, want SENT", second.State)
	}
	if pub.calls() != 2 {
		t.Fatalf("publishes = %d, want 2 after a distinct new key", pub.calls())
	}
}

func TestService_RunExpiry(t *testing.T) {
	t.Parallel()

	clock := &stubClock{now: time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)}
	svc := newTestService(t, &fakePublisher{}, vehicles{"FPULSESAO00000020": {}}, nil)
	svc.clock = clock

	rec, err := svc.Unlock(t.Context(), "FPULSESAO00000020", "expiry-loop")
	if err != nil {
		t.Fatalf("Unlock() error = %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	done := make(chan struct{})
	go func() {
		defer close(done)
		svc.RunExpiry(ctx)
	}()

	clock.advance(Expiry)
	deadline := time.Now().Add(time.Second)
	var got Record
	var ok bool
	for time.Now().Before(deadline) {
		got, ok = svc.Get(rec.ID)
		if ok && got.State == StateTimeout {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !ok || got.State != StateTimeout {
		t.Fatalf("after RunExpiry tick = %+v ok=%v, want TIMEOUT", got, ok)
	}
	cancel()
	<-done
}

func TestService_ApplyIgnoresUnknownAndEmpty(t *testing.T) {
	t.Parallel()

	svc := newTestService(t, &fakePublisher{}, vehicles{"FPULSESAO00000001": {}}, nil)
	svc.Apply("", true)
	svc.Apply("missing", false)
	if _, ok := svc.Get("missing"); ok {
		t.Fatal("unknown ack created a record")
	}
}

func newTestService(t *testing.T, pub Publisher, fleet vehicles, buf *bytes.Buffer) *Service {
	t.Helper()
	var log *slog.Logger
	if buf != nil {
		log = slog.New(slog.NewJSONHandler(buf, nil))
	}
	ids := 0
	svc := New(pub, fleet, log)
	svc.newID = func() string {
		ids++
		return "id-" + strconv.Itoa(ids)
	}
	return svc
}

type vehicles map[string]struct{}

func (v vehicles) Has(vin string) bool {
	_, ok := v[vin]
	return ok
}

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

type fakePublisher struct {
	mu      sync.Mutex
	err     error
	n       int
	topic   string
	payload []byte
}

func (f *fakePublisher) Publish(_ context.Context, topic string, payload []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.n++
	f.topic = topic
	f.payload = append(f.payload[:0], payload...)
	return f.err
}

func (f *fakePublisher) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.n
}

func (f *fakePublisher) lastTopic() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.topic
}

func (f *fakePublisher) lastPayload() []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]byte(nil), f.payload...)
}
