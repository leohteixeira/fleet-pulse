package sim

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/leohteixeira/fleet-pulse/internal/broker"
)

func TestNewFleet(t *testing.T) {
	t.Parallel()

	fleet := NewFleet()
	if len(fleet) != FleetSize {
		t.Fatalf("len(fleet) = %d, want %d", len(fleet), FleetSize)
	}
	if FleetSize < 15 || FleetSize > 25 {
		t.Fatalf("FleetSize = %d, want in 15–25", FleetSize)
	}

	var offline int
	seenVIN := make(map[string]struct{}, len(fleet))
	seenID := make(map[string]struct{}, len(fleet))
	for i, v := range fleet {
		if _, ok := seenVIN[v.VIN]; ok {
			t.Fatalf("duplicate vin %q", v.VIN)
		}
		seenVIN[v.VIN] = struct{}{}
		if _, ok := seenID[v.DisplayID]; ok {
			t.Fatalf("duplicate displayId %q", v.DisplayID)
		}
		seenID[v.DisplayID] = struct{}{}

		if v.IsOffline {
			offline++
			if v.VIN != OfflineVIN {
				t.Fatalf("offline vin = %q, want %q", v.VIN, OfflineVIN)
			}
		}
		if v.DisplayID != fmt.Sprintf("V%02d", i+1) {
			t.Fatalf("displayId = %q, want V%02d", v.DisplayID, i+1)
		}
		if abs(v.Lat-CentroLat) > 0.03 || abs(v.Lng-CentroLng) > 0.04 {
			t.Fatalf("vehicle %s spawn (%v,%v) is not around Centro", v.VIN, v.Lat, v.Lng)
		}

		raw, err := EncodeTelemetry(v)
		if err != nil {
			t.Fatalf("EncodeTelemetry() error = %v", err)
		}
		var payload map[string]any
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatalf("payload json: %v", err)
		}
		for _, key := range []string{
			"vin", "lat", "lng", "plate", "model", "battery", "speed",
			"heading", "ignition", "locked", "odometer", "trip", "displayId",
		} {
			if _, ok := payload[key]; !ok {
				t.Fatalf("payload missing %q: %s", key, raw)
			}
		}
		if payload["vin"] != v.VIN {
			t.Fatalf("payload vin = %v, want %q", payload["vin"], v.VIN)
		}
		if payload["displayId"] != v.DisplayID {
			t.Fatalf("payload displayId = %v, want %q", payload["displayId"], v.DisplayID)
		}
	}
	if offline != 1 {
		t.Fatalf("offline count = %d, want 1", offline)
	}
}

func TestRunFleet_SkipsOfflineVIN(t *testing.T) {
	t.Parallel()

	var (
		mu     sync.Mutex
		dialed []string
	)
	dial := func(_ context.Context, _, clientID string) (mqttClient, error) {
		mu.Lock()
		dialed = append(dialed, clientID)
		mu.Unlock()
		return &stubClient{}, nil
	}

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- runFleet(ctx, "127.0.0.1:1883", NewFleet(), time.Hour, dial)
	}()

	waitFor(t, time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(dialed) == FleetSize-1
	})
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("runFleet() error = %v", err)
	}

	if len(dialed) != FleetSize-1 {
		t.Fatalf("dialed %d vehicles, want %d", len(dialed), FleetSize-1)
	}
	for _, id := range dialed {
		if id == OfflineVIN {
			t.Fatal("offline vin connected")
		}
	}
}

func TestPublishLoop_Period(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		vehicle := NewFleet()[0]
		var (
			mu       sync.Mutex
			times    []time.Time
			topics   []string
			payloads [][]byte
		)
		pub := publisherFunc(func(_ context.Context, topic string, payload []byte) error {
			mu.Lock()
			times = append(times, time.Now())
			topics = append(topics, topic)
			payloads = append(payloads, payload)
			mu.Unlock()
			return nil
		})

		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() {
			done <- publishLoop(ctx, vehicle, PublishInterval, pub, nil)
		}()

		time.Sleep(4 * time.Second)
		synctest.Wait()
		cancel()
		if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("publishLoop() error = %v", err)
		}

		if len(times) < 2 {
			t.Fatalf("publishes = %d, want at least 2 after 4s", len(times))
		}
		gap := times[1].Sub(times[0])
		if gap != PublishInterval {
			t.Fatalf("publish gap = %s, want %s", gap, PublishInterval)
		}
		wantTopic := TelemetryTopic(vehicle.VIN)
		for i, topic := range topics {
			if topic != wantTopic {
				t.Fatalf("publish %d topic = %q, want %q", i, topic, wantTopic)
			}
			var point struct {
				VIN string  `json:"vin"`
				Lat float64 `json:"lat"`
				Lng float64 `json:"lng"`
			}
			if err := json.Unmarshal(payloads[i], &point); err != nil {
				t.Fatalf("publish %d payload: %v", i, err)
			}
			if point.VIN != vehicle.VIN || point.Lat != vehicle.Lat || point.Lng != vehicle.Lng {
				t.Fatalf("publish %d fields = %+v, want vin=%s lat=%v lng=%v", i, point, vehicle.VIN, vehicle.Lat, vehicle.Lng)
			}
		}
	})
}

func TestRunVehicle_AcksOverMQTT(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	b := broker.New("127.0.0.1:0", nil)
	if err := b.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = b.Close() })

	acks := make(chan []byte, 1)
	if err := b.Subscribe(ctx, "fleet/+/ack", func(_ string, payload []byte) {
		acks <- append([]byte(nil), payload...)
	}); err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}

	vehicle := NewFleet()[0]
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		done <- runFleet(runCtx, b.DialAddr(), []Vehicle{vehicle}, time.Hour, dialPaho)
	}()

	payload := []byte(`{"id":"cmd-mqtt","action":"unlock","correlationId":"corr-mqtt"}`)
	deadline := time.Now().Add(2 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		lastErr = b.Publish(ctx, CommandTopic(vehicle.VIN), payload)
		select {
		case got := <-acks:
			var body struct {
				CommandID string `json:"commandId"`
				OK        bool   `json:"ok"`
			}
			if err := json.Unmarshal(got, &body); err != nil {
				t.Fatalf("ack json: %v", err)
			}
			if body.CommandID != "cmd-mqtt" || !body.OK {
				t.Fatalf("ack = %+v, want commandId=cmd-mqtt ok=true", body)
			}
			cancel()
			if err := <-done; err != nil {
				t.Fatalf("runFleet() error = %v", err)
			}
			return
		case <-time.After(20 * time.Millisecond):
		}
	}

	cancel()
	<-done
	t.Fatalf("did not receive ack (last publish err %v)", lastErr)
}

func TestRunVehicle_SubscribesAndAcks(t *testing.T) {
	t.Parallel()

	client := &recordingClient{}
	dial := func(_ context.Context, _, _ string) (mqttClient, error) {
		return client, nil
	}

	vehicle := NewFleet()[0]
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- runFleet(ctx, "127.0.0.1:1883", []Vehicle{vehicle}, time.Hour, dial)
	}()

	waitFor(t, time.Second, func() bool {
		return client.subTopic() == CommandTopic(vehicle.VIN)
	})

	client.deliver([]byte(`{"id":"cmd-1","action":"unlock","correlationId":"corr-1"}`))
	waitFor(t, time.Second, func() bool {
		return client.hasAck("cmd-1")
	})

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("runFleet() error = %v", err)
	}
}

type stubClient struct{}

func (stubClient) Publish(context.Context, string, []byte) error { return nil }
func (stubClient) Subscribe(context.Context, string, func([]byte)) error {
	return nil
}
func (stubClient) Disconnect(context.Context) error { return nil }

type recordingClient struct {
	mu       sync.Mutex
	topic    string
	handler  func([]byte)
	payloads [][]byte
	topics   []string
}

func (c *recordingClient) Subscribe(_ context.Context, topic string, handler func([]byte)) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.topic = topic
	c.handler = handler
	return nil
}

func (c *recordingClient) Publish(_ context.Context, topic string, payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.topics = append(c.topics, topic)
	c.payloads = append(c.payloads, append([]byte(nil), payload...))
	return nil
}

func (c *recordingClient) Disconnect(context.Context) error { return nil }

func (c *recordingClient) deliver(payload []byte) {
	c.mu.Lock()
	h := c.handler
	c.mu.Unlock()
	if h != nil {
		h(payload)
	}
}

func (c *recordingClient) subTopic() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.topic
}

func (c *recordingClient) hasAck(commandID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, topic := range c.topics {
		if !strings.HasSuffix(topic, "/ack") {
			continue
		}
		var body struct {
			CommandID string `json:"commandId"`
			OK        bool   `json:"ok"`
		}
		if json.Unmarshal(c.payloads[i], &body) == nil && body.CommandID == commandID && body.OK {
			return true
		}
	}
	return false
}

type publisherFunc func(ctx context.Context, topic string, payload []byte) error

func (f publisherFunc) Publish(ctx context.Context, topic string, payload []byte) error {
	return f(ctx, topic, payload)
}

func waitFor(t *testing.T, timeout time.Duration, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out after %s", timeout)
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
