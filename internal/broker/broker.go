// Package broker embeds a mochi-mqtt v2 TCP broker and adapts it to ingest.Subscriber.
package broker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"

	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"
	"github.com/mochi-mqtt/server/v2/packets"
)

// Broker is an embedded MQTT server that vehicles reach over TCP.
type Broker struct {
	bindAddr  string
	addr      string
	log       *slog.Logger
	srv       *mqtt.Server
	closeOnce sync.Once
	closeErr  error
	mu        sync.Mutex
	nextSub   int
	subs      map[string]int
}

// New prepares a broker that will bind addr (for example 0.0.0.0:1883).
func New(addr string, log *slog.Logger) *Broker {
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Broker{
		bindAddr: addr,
		log:      log,
		subs:     make(map[string]int),
	}
}

// Addr is the actual listen address after Start.
func (b *Broker) Addr() string {
	return b.addr
}

// DialAddr is the TCP address vehicle clients should use.
func (b *Broker) DialAddr() string {
	host, port, err := net.SplitHostPort(b.addr)
	if err != nil {
		return b.addr
	}
	switch host {
	case "", "0.0.0.0", "::":
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
}

// Start binds the TCP listener and begins accepting MQTT clients.
func (b *Broker) Start(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	srv := mqtt.New(&mqtt.Options{
		InlineClient: true,
		Logger:       b.log,
	})
	if err := srv.AddHook(new(auth.AllowHook), nil); err != nil {
		return fmt.Errorf("add auth hook: %w", err)
	}

	tcp := listeners.NewTCP(listeners.Config{
		ID:      "tcp",
		Address: b.bindAddr,
	})
	if err := srv.AddListener(tcp); err != nil {
		return fmt.Errorf("add tcp listener: %w", err)
	}
	if err := srv.Serve(); err != nil {
		return fmt.Errorf("serve broker: %w", err)
	}

	b.srv = srv
	b.addr = tcp.Address()
	if err := waitReady(ctx, b.DialAddr()); err != nil {
		_ = b.Close()
		return err
	}
	return nil
}

// Subscribe implements ingest.Subscriber without exposing mochi types.
func (b *Broker) Subscribe(ctx context.Context, filter string, handler func(topic string, payload []byte)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if b.srv == nil {
		return errors.New("broker: not started")
	}
	if handler == nil {
		return errors.New("broker: handler is required")
	}

	id := b.reserveSub(filter)
	err := b.srv.Subscribe(filter, id, func(_ *mqtt.Client, _ packets.Subscription, pk packets.Packet) {
		handler(pk.TopicName, pk.Payload)
	})
	if err != nil {
		b.releaseSub(filter)
		return fmt.Errorf("subscribe %q: %w", filter, err)
	}
	return nil
}

// Unsubscribe implements ingest.Subscriber.
func (b *Broker) Unsubscribe(ctx context.Context, filter string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if b.srv == nil {
		return errors.New("broker: not started")
	}
	id, ok := b.subID(filter)
	if !ok {
		return fmt.Errorf("unsubscribe %q: unknown subscription", filter)
	}
	if err := b.srv.Unsubscribe(filter, id); err != nil {
		return fmt.Errorf("unsubscribe %q: %w", filter, err)
	}
	b.releaseSub(filter)
	return nil
}

// Publish sends payload on topic through the inline client without exposing mochi types.
func (b *Broker) Publish(ctx context.Context, topic string, payload []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if b.srv == nil {
		return errors.New("broker: not started")
	}
	if err := b.srv.Publish(topic, payload, false, 0); err != nil {
		return fmt.Errorf("publish %q: %w", topic, err)
	}
	return nil
}

func (b *Broker) reserveSub(filter string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.subs == nil {
		b.subs = make(map[string]int)
	}
	b.nextSub++
	b.subs[filter] = b.nextSub
	return b.nextSub
}

func (b *Broker) subID(filter string) (int, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	id, ok := b.subs[filter]
	return id, ok
}

func (b *Broker) releaseSub(filter string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.subs, filter)
}

// Close stops listeners and the embedded broker.
func (b *Broker) Close() error {
	b.closeOnce.Do(func() {
		if b.srv == nil {
			return
		}
		if err := b.srv.Close(); err != nil {
			b.closeErr = fmt.Errorf("close broker: %w", err)
		}
	})
	return b.closeErr
}

func waitReady(ctx context.Context, addr string) error {
	readyCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	var d net.Dialer
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	var last error
	for {
		conn, err := d.DialContext(readyCtx, "tcp", addr)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		last = err
		select {
		case <-readyCtx.Done():
			if last != nil {
				return fmt.Errorf("broker not accepting connections: %w", last)
			}
			return fmt.Errorf("broker not accepting connections: %w", readyCtx.Err())
		case <-ticker.C:
		}
	}
}
