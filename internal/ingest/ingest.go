// Package ingest observes telemetry after it has traversed the MQTT broker.
package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
)

// TelemetryFilter matches device telemetry topics.
const TelemetryFilter = "fleet/+/telemetry"

var errMissingVIN = errors.New("ingest: missing vin")

// MessageHandler receives a topic and payload that already traversed the broker.
type MessageHandler = func(topic string, payload []byte)

// Subscriber is the subscribe port declared by ingest and implemented by the broker adapter.
type Subscriber interface {
	Subscribe(ctx context.Context, filter string, handler MessageHandler) error
	Unsubscribe(ctx context.Context, filter string) error
}

type point struct {
	VIN string  `json:"vin"`
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

// Run subscribes to telemetry, logs vin/lat/lng, and unsubscribes when ctx is cancelled.
func Run(ctx context.Context, sub Subscriber, log *slog.Logger) error {
	if log == nil {
		return errors.New("ingest: logger is required")
	}
	if sub == nil {
		return errors.New("ingest: subscriber is required")
	}

	if err := sub.Subscribe(ctx, TelemetryFilter, func(topic string, payload []byte) {
		handle(log, topic, payload)
	}); err != nil {
		return fmt.Errorf("subscribe telemetry: %w", err)
	}

	<-ctx.Done()

	if err := sub.Unsubscribe(context.WithoutCancel(ctx), TelemetryFilter); err != nil {
		return fmt.Errorf("unsubscribe telemetry: %w", err)
	}
	return nil
}

func handle(log *slog.Logger, topic string, payload []byte) {
	p, err := parse(payload)
	if err != nil {
		log.Warn("skipping telemetry", "topic", topic, "err", err)
		return
	}
	log.Info("telemetry", "vin", p.VIN, "lat", p.Lat, "lng", p.Lng)
}

func parse(payload []byte) (point, error) {
	var p point
	if err := json.Unmarshal(payload, &p); err != nil {
		return point{}, fmt.Errorf("decode telemetry: %w", err)
	}
	if p.VIN == "" {
		return point{}, errMissingVIN
	}
	return p, nil
}
