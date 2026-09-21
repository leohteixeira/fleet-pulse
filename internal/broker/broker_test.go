package broker_test

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eclipse/paho.golang/paho"
	"github.com/leohteixeira/fleet-pulse/internal/broker"
	"github.com/leohteixeira/fleet-pulse/internal/ingest"
)

var _ ingest.Subscriber = (*broker.Broker)(nil)

func TestBroker_SubscribeReceivesClientPublish(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	b := broker.New("127.0.0.1:0", nil)
	if err := b.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() {
		if err := b.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})

	got := make(chan struct {
		topic   string
		payload []byte
	}, 1)
	if err := b.Subscribe(ctx, ingest.TelemetryFilter, func(topic string, payload []byte) {
		got <- struct {
			topic   string
			payload []byte
		}{topic: topic, payload: payload}
	}); err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}

	payload := []byte(`{"vin":"TESTVIN000000001","lat":-23.55,"lng":-46.63}`)
	topic := "fleet/TESTVIN000000001/telemetry"
	if err := publishMQTT(ctx, b.DialAddr(), "test-publisher", topic, payload); err != nil {
		t.Fatalf("publishMQTT() error = %v", err)
	}

	select {
	case msg := <-got:
		if msg.topic != topic {
			t.Fatalf("topic = %q, want %q", msg.topic, topic)
		}
		if !bytes.Equal(msg.payload, payload) {
			t.Fatalf("payload = %s, want %s", msg.payload, payload)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("subscriber handler did not fire")
	}
}

func TestBroker_IngestLogsPublishedTelemetry(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	b := broker.New("127.0.0.1:0", nil)
	if err := b.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = b.Close() })

	var buf syncBuffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	errCh := make(chan error, 1)
	go func() {
		errCh <- ingest.Run(
			ctx,
			b,
			nopSink{},
			log,
		)
	}()

	payload := []byte(`{"vin":"TESTVIN000000001","lat":-23.55,"lng":-46.63}`)
	topic := "fleet/TESTVIN000000001/telemetry"
	deadline := time.Now().Add(2 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		lastErr = publishMQTT(ctx, b.DialAddr(), "ingest-roundtrip", topic, payload)
		if lastErr == nil && strings.Contains(buf.String(), `"msg":"telemetry"`) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	got := buf.String()
	for _, want := range []string{
		`"msg":"telemetry"`,
		`"vin":"TESTVIN000000001"`,
		`"lat":-23.55`,
		`"lng":-46.63`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("log missing %q in %s (publish err %v)", want, got, lastErr)
		}
	}

	cancel()
	if err := <-errCh; err != nil {
		t.Fatalf("ingest.Run() error = %v", err)
	}
}

func TestBroker_StartListensOnTCP(t *testing.T) {
	t.Parallel()

	b := broker.New("127.0.0.1:0", nil)
	if err := b.Start(t.Context()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = b.Close() })

	conn, err := net.DialTimeout("tcp", b.DialAddr(), time.Second)
	if err != nil {
		t.Fatalf("dial broker: %v", err)
	}
	_ = conn.Close()
}

type nopSink struct{}

func (nopSink) Apply(ingest.Telemetry) {}

type syncBuffer struct {
	mu sync.Mutex
	bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.String()
}

func publishMQTT(ctx context.Context, addr, clientID, topic string, payload []byte) error {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}

	c := paho.NewClient(paho.ClientConfig{
		ClientID: clientID,
		Conn:     conn,
		OnClientError: func(error) {
		},
	})
	ca, err := c.Connect(ctx, &paho.Connect{
		ClientID:   clientID,
		KeepAlive:  30,
		CleanStart: true,
	})
	if err != nil {
		_ = conn.Close()
		return err
	}
	if ca.ReasonCode != 0 {
		_ = conn.Close()
		return fmt.Errorf("mqtt connect refused: reason %d", ca.ReasonCode)
	}
	defer func() { _ = c.Disconnect(&paho.Disconnect{ReasonCode: 0}) }()

	_, err = c.Publish(ctx, &paho.Publish{
		Topic:   topic,
		QoS:     0,
		Payload: payload,
	})
	return err
}
