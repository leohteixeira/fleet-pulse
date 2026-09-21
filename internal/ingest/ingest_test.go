package ingest_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/leohteixeira/fleet-pulse/internal/ingest"
)

type recordingSink struct {
	mu  sync.Mutex
	got []ingest.Telemetry
}

type recordingAcks struct {
	mu  sync.Mutex
	ids []string
	oks []bool
}

func (r *recordingAcks) Apply(commandID string, ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ids = append(r.ids, commandID)
	r.oks = append(r.oks, ok)
}

func (r *recordingAcks) applied() (ids []string, oks []bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.ids...), append([]bool(nil), r.oks...)
}

func (r *recordingSink) Apply(t ingest.Telemetry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.got = append(r.got, t)
}

func (r *recordingSink) applied() []ingest.Telemetry {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]ingest.Telemetry, len(r.got))
	copy(out, r.got)
	return out
}

type fakeSub struct {
	mu         sync.Mutex
	handlers   map[string]ingest.MessageHandler
	subscribed chan struct{}
	filters    []string
}

func newFakeSub() *fakeSub {
	return &fakeSub{
		subscribed: make(chan struct{}),
		handlers:   make(map[string]ingest.MessageHandler),
	}
}

func (f *fakeSub) Subscribe(_ context.Context, filter string, handler ingest.MessageHandler) error {
	f.mu.Lock()
	f.filters = append(f.filters, filter)
	f.handlers[filter] = handler
	first := len(f.filters) == 1
	f.mu.Unlock()
	if first {
		close(f.subscribed)
	}
	return nil
}

func (f *fakeSub) Unsubscribe(context.Context, string) error {
	return nil
}

func (f *fakeSub) deliver(filter, topic string, payload []byte) {
	f.mu.Lock()
	h := f.handlers[filter]
	f.mu.Unlock()
	h(topic, payload)
}

func (f *fakeSub) hasFilter(want string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, filter := range f.filters {
		if filter == want {
			return true
		}
	}
	return false
}

func TestRun(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		topic        string
		payload      []byte
		wantContains []string
		wantAbsent   []string
		wantLevel    string
	}{
		{
			name:    "happy path",
			topic:   "fleet/FPULSESAO00000001/telemetry",
			payload: []byte(`{"vin":"FPULSESAO00000001","lat":-23.55,"lng":-46.63,"plate":"BAL0A01","model":"Fiat Argo","battery":22,"speed":18,"heading":0,"ignition":false,"locked":true,"odometer":4200,"trip":0.4,"displayId":"V01"}`),
			wantContains: []string{
				`"msg":"telemetry"`,
				`"vin":"FPULSESAO00000001"`,
				`"lat":-23.55`,
				`"lng":-46.63`,
			},
			wantLevel: "INFO",
		},
		{
			name:         "bad payload",
			topic:        "fleet/FPULSESAO00000002/telemetry",
			payload:      []byte(`not-json`),
			wantContains: []string{`"msg":"skipping telemetry"`, `"topic":"fleet/FPULSESAO00000002/telemetry"`},
			wantAbsent:   []string{`"msg":"telemetry"`},
			wantLevel:    "WARN",
		},
		{
			name:         "missing vin",
			topic:        "fleet/unknown/telemetry",
			payload:      []byte(`{"lat":-23.55,"lng":-46.63}`),
			wantContains: []string{`"msg":"skipping telemetry"`, `"topic":"fleet/unknown/telemetry"`},
			wantAbsent:   []string{`"vin":`},
			wantLevel:    "WARN",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			log := slog.New(slog.NewJSONHandler(&buf, nil))
			sub := newFakeSub()
			sink := &recordingSink{}
			acks := &recordingAcks{}

			ctx, cancel := context.WithCancel(t.Context())
			errCh := make(chan error, 1)
			go func() {
				errCh <- ingest.Run(ctx, sub, sink, acks, log)
			}()

			select {
			case <-sub.subscribed:
			case err := <-errCh:
				t.Fatalf("run ended before subscribe: %v", err)
			}
			waitFor(t, func() bool {
				return sub.hasFilter(ingest.TelemetryFilter) && sub.hasFilter(ingest.AckFilter)
			})

			sub.deliver(ingest.TelemetryFilter, tt.topic, tt.payload)
			cancel()

			if err := <-errCh; err != nil {
				t.Fatalf("Run() error = %v", err)
			}

			got := buf.String()
			if !strings.Contains(got, `"level":"`+tt.wantLevel+`"`) {
				t.Fatalf("log level %q not found in %s", tt.wantLevel, got)
			}
			for _, want := range tt.wantContains {
				if !strings.Contains(got, want) {
					t.Fatalf("log missing %q in %s", want, got)
				}
			}
			for _, absent := range tt.wantAbsent {
				if strings.Contains(got, absent) {
					t.Fatalf("log unexpectedly contains %q in %s", absent, got)
				}
			}

			applied := sink.applied()
			if tt.wantLevel == "INFO" {
				if len(applied) != 1 {
					t.Fatalf("applied = %d, want 1", len(applied))
				}
				want := ingest.Telemetry{
					VIN:       "FPULSESAO00000001",
					Lat:       -23.55,
					Lng:       -46.63,
					Plate:     "BAL0A01",
					Model:     "Fiat Argo",
					Battery:   22,
					Speed:     18,
					Heading:   0,
					Ignition:  false,
					Locked:    true,
					Odometer:  4200,
					Trip:      0.4,
					DisplayID: "V01",
				}
				if applied[0] != want {
					t.Fatalf("applied = %+v, want %+v", applied[0], want)
				}
				return
			}
			if len(applied) != 0 {
				t.Fatalf("applied = %+v, want none on skip", applied)
			}
		})
	}
}

func TestRun_Ack(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		payload      []byte
		wantIDs      []string
		wantOKs      []bool
		wantContains []string
		wantAbsent   []string
	}{
		{
			name:         "accepted",
			payload:      []byte(`{"commandId":"cmd-1","ok":true}`),
			wantIDs:      []string{"cmd-1"},
			wantOKs:      []bool{true},
			wantContains: []string{},
		},
		{
			name:         "refused",
			payload:      []byte(`{"commandId":"cmd-2","ok":false}`),
			wantIDs:      []string{"cmd-2"},
			wantOKs:      []bool{false},
			wantContains: []string{},
		},
		{
			name:         "bad json skipped",
			payload:      []byte(`not-json`),
			wantContains: []string{`"msg":"skipping ack"`},
			wantAbsent:   []string{`"msg":"telemetry"`},
		},
		{
			name:         "missing command id skipped",
			payload:      []byte(`{"ok":true}`),
			wantContains: []string{`"msg":"skipping ack"`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			log := slog.New(slog.NewJSONHandler(&buf, nil))
			sub := newFakeSub()
			sink := &recordingSink{}
			acks := &recordingAcks{}

			ctx, cancel := context.WithCancel(t.Context())
			errCh := make(chan error, 1)
			go func() {
				errCh <- ingest.Run(ctx, sub, sink, acks, log)
			}()

			select {
			case <-sub.subscribed:
			case err := <-errCh:
				t.Fatalf("run ended before subscribe: %v", err)
			}
			waitFor(t, func() bool {
				return sub.hasFilter(ingest.AckFilter)
			})

			sub.deliver(ingest.AckFilter, "fleet/FPULSESAO00000001/ack", tt.payload)
			cancel()
			if err := <-errCh; err != nil {
				t.Fatalf("Run() error = %v", err)
			}

			ids, oks := acks.applied()
			if len(ids) != len(tt.wantIDs) {
				t.Fatalf("applied ids = %v, want %v", ids, tt.wantIDs)
			}
			for i, id := range tt.wantIDs {
				if ids[i] != id || oks[i] != tt.wantOKs[i] {
					t.Fatalf("applied[%d] = %s/%v, want %s/%v", i, ids[i], oks[i], id, tt.wantOKs[i])
				}
			}

			got := buf.String()
			for _, want := range tt.wantContains {
				if !strings.Contains(got, want) {
					t.Fatalf("log missing %q in %s", want, got)
				}
			}
			for _, absent := range tt.wantAbsent {
				if strings.Contains(got, absent) {
					t.Fatalf("log unexpectedly contains %q in %s", absent, got)
				}
			}
			if len(sink.applied()) != 0 {
				t.Fatalf("telemetry sink applied = %+v, want none", sink.applied())
			}
		})
	}
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for subscribe")
}
