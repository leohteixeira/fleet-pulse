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

// Sink is the apply port declared by ingest and implemented by the process store wiring.
type Sink interface {
	Apply(Telemetry)
}

// Telemetry is the last-known vehicle fields taken from a parsed MQTT payload.
type Telemetry struct {
	VIN       string  `json:"vin"`
	Lat       float64 `json:"lat"`
	Lng       float64 `json:"lng"`
	Plate     string  `json:"plate"`
	Model     string  `json:"model"`
	Battery   int     `json:"battery"`
	Speed     int     `json:"speed"`
	Heading   int     `json:"heading"`
	Ignition  bool    `json:"ignition"`
	Locked    bool    `json:"locked"`
	Odometer  float64 `json:"odometer"`
	Trip      float64 `json:"trip"`
	DisplayID string  `json:"displayId"`
}

// Run subscribes to telemetry, logs vin/lat/lng, applies a successful parse, and unsubscribes when ctx is cancelled.
func Run(ctx context.Context, sub Subscriber, sink Sink, log *slog.Logger) error {
	if log == nil {
		return errors.New("ingest: logger is required")
	}
	if sub == nil {
		return errors.New("ingest: subscriber is required")
	}
	if sink == nil {
		return errors.New("ingest: sink is required")
	}

	if err := sub.Subscribe(ctx, TelemetryFilter, func(topic string, payload []byte) {
		handle(log, sink, topic, payload)
	}); err != nil {
		return fmt.Errorf("subscribe telemetry: %w", err)
	}

	<-ctx.Done()

	if err := sub.Unsubscribe(context.WithoutCancel(ctx), TelemetryFilter); err != nil {
		return fmt.Errorf("unsubscribe telemetry: %w", err)
	}
	return nil
}

func handle(log *slog.Logger, sink Sink, topic string, payload []byte) {
	p, err := parse(payload)
	if err != nil {
		log.Warn("skipping telemetry", "topic", topic, "err", err)
		return
	}
	log.Info("telemetry", "vin", p.VIN, "lat", p.Lat, "lng", p.Lng)
	sink.Apply(p)
}

func parse(payload []byte) (Telemetry, error) {
	var p Telemetry
	if err := json.Unmarshal(payload, &p); err != nil {
		return Telemetry{}, fmt.Errorf("decode telemetry: %w", err)
	}
	if p.VIN == "" {
		return Telemetry{}, errMissingVIN
	}
	return p, nil
}
