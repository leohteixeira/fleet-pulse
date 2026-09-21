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

// AckFilter matches device command acknowledgements.
const AckFilter = "fleet/+/ack"

var (
	errMissingVIN       = errors.New("ingest: missing vin")
	errMissingCommandID = errors.New("ingest: missing command id")
)

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

// AckSink is the ack apply port declared by ingest and implemented by the command machine.
type AckSink interface {
	Apply(commandID string, ok bool)
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

// Run subscribes to telemetry and ack, applies successful parses, and unsubscribes when ctx is cancelled.
func Run(ctx context.Context, sub Subscriber, sink Sink, acks AckSink, log *slog.Logger) error {
	if log == nil {
		return errors.New("ingest: logger is required")
	}
	if sub == nil {
		return errors.New("ingest: subscriber is required")
	}
	if sink == nil {
		return errors.New("ingest: sink is required")
	}
	if acks == nil {
		return errors.New("ingest: ack sink is required")
	}

	if err := sub.Subscribe(ctx, TelemetryFilter, func(topic string, payload []byte) {
		handle(log, sink, topic, payload)
	}); err != nil {
		return fmt.Errorf("subscribe telemetry: %w", err)
	}
	if err := sub.Subscribe(ctx, AckFilter, func(topic string, payload []byte) {
		handleAck(log, acks, topic, payload)
	}); err != nil {
		_ = sub.Unsubscribe(context.WithoutCancel(ctx), TelemetryFilter)
		return fmt.Errorf("subscribe ack: %w", err)
	}

	<-ctx.Done()

	unsubCtx := context.WithoutCancel(ctx)
	var unsubErr error
	if err := sub.Unsubscribe(unsubCtx, TelemetryFilter); err != nil {
		unsubErr = errors.Join(unsubErr, fmt.Errorf("unsubscribe telemetry: %w", err))
	}
	if err := sub.Unsubscribe(unsubCtx, AckFilter); err != nil {
		unsubErr = errors.Join(unsubErr, fmt.Errorf("unsubscribe ack: %w", err))
	}
	return unsubErr
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

func handleAck(log *slog.Logger, acks AckSink, topic string, payload []byte) {
	a, err := parseAck(payload)
	if err != nil {
		log.Warn("skipping ack", "topic", topic, "err", err)
		return
	}
	acks.Apply(a.CommandID, a.OK)
}

type ackPayload struct {
	CommandID string `json:"commandId"`
	OK        bool   `json:"ok"`
}

func parseAck(payload []byte) (ackPayload, error) {
	var a ackPayload
	if err := json.Unmarshal(payload, &a); err != nil {
		return ackPayload{}, fmt.Errorf("decode ack: %w", err)
	}
	if a.CommandID == "" {
		return ackPayload{}, errMissingCommandID
	}
	return a, nil
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
