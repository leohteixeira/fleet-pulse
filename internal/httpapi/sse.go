package httpapi

import (
	"bytes"
	"fmt"
	"net/http"
	"sync"
)

const clientBufferSize = 32

// Event is one SSE frame. Telemetry patches use Name "telemetry".
type Event struct {
	Name string
	Data []byte
}

type client struct {
	mu       sync.Mutex
	ch       chan Event
	isClosed bool
}

// Hub fans telemetry events to each SSE client through a bounded ring.
type Hub struct {
	mu      sync.Mutex
	clients map[*client]struct{}
}

// NewHub starts with no subscribers.
func NewHub() *Hub {
	return &Hub{clients: make(map[*client]struct{})}
}

var _ HubPort = (*Hub)(nil)

// Subscribe registers a per-client buffer. The caller must unsubscribe.
func (h *Hub) Subscribe() (<-chan Event, func()) {
	c := &client{ch: make(chan Event, clientBufferSize)}
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
	return c.ch, func() { h.unsubscribe(c) }
}

// Publish enqueues ev for every client, dropping the oldest queued event under pressure.
func (h *Hub) Publish(ev Event) {
	h.mu.Lock()
	clients := make([]*client, 0, len(h.clients))
	for c := range h.clients {
		clients = append(clients, c)
	}
	h.mu.Unlock()

	for _, c := range clients {
		c.enqueue(ev)
	}
}

func (h *Hub) unsubscribe(c *client) {
	h.mu.Lock()
	delete(h.clients, c)
	h.mu.Unlock()
	c.close()
}

func (c *client) enqueue(ev Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.isClosed {
		return
	}
	ev.Data = bytes.Clone(ev.Data)
	if len(c.ch) == cap(c.ch) {
		<-c.ch
	}
	c.ch <- ev
}

func (c *client) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.isClosed {
		return
	}
	c.isClosed = true
	close(c.ch)
}

func (s *Server) stream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	events, unsubscribe := s.hub.Subscribe()
	defer unsubscribe()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Del("Content-Encoding")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			if err := writeSSE(w, ev); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func writeSSE(w http.ResponseWriter, ev Event) error {
	if ev.Name == "" {
		ev.Name = "telemetry"
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Name, ev.Data); err != nil {
		return fmt.Errorf("write sse event: %w", err)
	}
	return nil
}
