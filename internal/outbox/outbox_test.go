package outbox

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type memOutbox struct {
	mu   sync.Mutex
	rows []Row
	sent map[int64]time.Time
	fail bool
}

func (m *memOutbox) ListUnsent(_ context.Context, limit int32) ([]Row, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Row, 0, len(m.rows))
	for _, row := range m.rows {
		if _, ok := m.sent[row.ID]; ok {
			continue
		}
		out = append(out, row)
		if limit > 0 && int32(len(out)) >= limit {
			break
		}
	}
	return out, nil
}

func (m *memOutbox) MarkSent(_ context.Context, id int64, sentAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sent == nil {
		m.sent = make(map[int64]time.Time)
	}
	m.sent[id] = sentAt
	return nil
}

type recPub struct {
	mu       sync.Mutex
	topics   []string
	payloads [][]byte
	err      error
}

func (p *recPub) Publish(_ context.Context, topic string, payload []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.err != nil {
		return p.err
	}
	p.topics = append(p.topics, topic)
	p.payloads = append(p.payloads, append([]byte(nil), payload...))
	return nil
}

func TestRelay_FlushPublishesAndMarksSent(t *testing.T) {
	t.Parallel()

	st := &memOutbox{rows: []Row{{
		ID:      7,
		Topic:   "leasing/FPULSELSG00000001/commands",
		Payload: []byte(`{"id":"c1","action":"block"}`),
	}}}
	pub := &recPub{}
	r := New(st, pub, nil)
	if err := r.Flush(t.Context()); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	if len(pub.topics) != 1 || pub.topics[0] != st.rows[0].Topic {
		t.Fatalf("published topics = %v, want [%q]", pub.topics, st.rows[0].Topic)
	}
	if _, ok := st.sent[7]; !ok {
		t.Fatal("row 7 was not marked sent")
	}

	if err := r.Flush(t.Context()); err != nil {
		t.Fatalf("second Flush() error = %v", err)
	}
	if len(pub.topics) != 1 {
		t.Fatalf("republished already-sent row: %v", pub.topics)
	}
}

func TestRelay_FlushPagesUntilEmpty(t *testing.T) {
	t.Parallel()

	const leftover = 7
	const total = int(flushLimit) + leftover
	rows := make([]Row, total)
	for i := range rows {
		rows[i] = Row{
			ID:      int64(i + 1),
			Topic:   "leasing/x/commands",
			Payload: []byte(`{}`),
		}
	}
	st := &memOutbox{rows: rows}
	pub := &recPub{}
	r := New(st, pub, nil)
	if err := r.Flush(t.Context()); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	if len(pub.topics) != total {
		t.Fatalf("published %d rows, want %d", len(pub.topics), total)
	}
	if len(st.sent) != total {
		t.Fatalf("marked sent %d rows, want %d", len(st.sent), total)
	}
}

func TestRelay_PublishErrorDoesNotMarkSent(t *testing.T) {
	t.Parallel()

	st := &memOutbox{rows: []Row{{ID: 1, Topic: "leasing/x/commands", Payload: []byte(`{}`)}}}
	pub := &recPub{err: errors.New("mqtt down")}
	r := New(st, pub, nil)
	if err := r.Flush(t.Context()); err == nil {
		t.Fatal("Flush() expected publish error")
	}
	if len(st.sent) != 0 {
		t.Fatalf("marked sent on publish failure: %v", st.sent)
	}
}
