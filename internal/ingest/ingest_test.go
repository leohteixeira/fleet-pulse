package ingest_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/leohteixeira/fleet-pulse/internal/ingest"
)

type fakeSub struct {
	mu         sync.Mutex
	handler    ingest.MessageHandler
	subscribed chan struct{}
	filter     string
}

func newFakeSub() *fakeSub {
	return &fakeSub{subscribed: make(chan struct{})}
}

func (f *fakeSub) Subscribe(_ context.Context, filter string, handler ingest.MessageHandler) error {
	f.mu.Lock()
	f.filter = filter
	f.handler = handler
	f.mu.Unlock()
	close(f.subscribed)
	return nil
}

func (f *fakeSub) Unsubscribe(context.Context, string) error {
	return nil
}

func (f *fakeSub) deliver(topic string, payload []byte) {
	f.mu.Lock()
	h := f.handler
	f.mu.Unlock()
	h(topic, payload)
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
			payload: []byte(`{"vin":"FPULSESAO00000001","lat":-23.55,"lng":-46.63}`),
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

			ctx, cancel := context.WithCancel(t.Context())
			errCh := make(chan error, 1)
			go func() {
				errCh <- ingest.Run(ctx, sub, log)
			}()

			select {
			case <-sub.subscribed:
			case err := <-errCh:
				t.Fatalf("run ended before subscribe: %v", err)
			}
			if sub.filter != ingest.TelemetryFilter {
				t.Fatalf("filter = %q, want %q", sub.filter, ingest.TelemetryFilter)
			}

			sub.deliver(tt.topic, tt.payload)
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
		})
	}
}
