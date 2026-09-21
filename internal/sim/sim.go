// Package sim builds the demo fleet and publishes telemetry through real MQTT clients.
package sim

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/eclipse/paho.golang/paho"
)

const (
	// FleetSize sits inside the 15–25 vehicle range required by the product.
	FleetSize = 20
	// PublishInterval is the device telemetry period.
	PublishInterval = 2 * time.Second
	// CentroLat is the spawn latitude near the Centro rectangle.
	CentroLat = -23.55
	// CentroLng is the spawn longitude near the Centro rectangle.
	CentroLng = -46.63
	// OfflineVIN never connects and never publishes.
	OfflineVIN = "FPULSESAO00000020"
)

var models = []string{
	"Fiat Argo",
	"VW Polo",
	"Chevrolet Onix",
	"Hyundai HB20",
	"Renault Kwid",
	"Fiat Mobi",
	"Toyota Corolla",
	"VW Nivus",
	"Jeep Renegade",
	"Peugeot 208",
}

// Vehicle is one simulated fleet member and the telemetry it will publish.
type Vehicle struct {
	VIN       string
	DisplayID string
	Plate     string
	Model     string
	Lat       float64
	Lng       float64
	Battery   int
	Speed     int
	Heading   int
	Ignition  bool
	Locked    bool
	Odometer  float64
	Trip      float64
	IsOffline bool
}

type telemetry struct {
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

type publisher interface {
	Publish(ctx context.Context, topic string, payload []byte) error
}

type mqttClient interface {
	publisher
	Disconnect(ctx context.Context) error
}

type dialFunc func(ctx context.Context, addr, clientID string) (mqttClient, error)

// NewFleet returns 20 vehicles around Centro, including exactly one offline VIN.
func NewFleet() []Vehicle {
	fleet := make([]Vehicle, 0, FleetSize)
	for i := range FleetSize {
		n := i + 1
		vin := fmt.Sprintf("FPULSESAO%08d", n)
		fleet = append(fleet, Vehicle{
			VIN:       vin,
			DisplayID: fmt.Sprintf("V%02d", n),
			Plate:     plate(i),
			Model:     models[i%len(models)],
			Lat:       CentroLat + (float64(i%5)-2)*0.004,
			Lng:       CentroLng + (float64(i/5)-1.5)*0.006,
			Battery:   22 + (i*4)%76,
			Speed:     18 + (i*3)%40,
			Heading:   (i * 18) % 360,
			Ignition:  i%3 != 0,
			Locked:    i%3 == 0,
			Odometer:  4200 + float64(i)*1000,
			Trip:      float64((i%7)+1) * 0.4,
			IsOffline: vin == OfflineVIN,
		})
	}
	return fleet
}

// TelemetryTopic is the device publish topic for vin.
func TelemetryTopic(vin string) string {
	return "fleet/" + vin + "/telemetry"
}

// EncodeTelemetry renders the wire payload later stories must keep stable.
func EncodeTelemetry(v Vehicle) ([]byte, error) {
	raw, err := json.Marshal(telemetry{
		VIN:       v.VIN,
		Lat:       v.Lat,
		Lng:       v.Lng,
		Plate:     v.Plate,
		Model:     v.Model,
		Battery:   v.Battery,
		Speed:     v.Speed,
		Heading:   v.Heading,
		Ignition:  v.Ignition,
		Locked:    v.Locked,
		Odometer:  v.Odometer,
		Trip:      v.Trip,
		DisplayID: v.DisplayID,
	})
	if err != nil {
		return nil, fmt.Errorf("encode telemetry: %w", err)
	}
	return raw, nil
}

// Run connects each online vehicle as a real MQTT client and publishes every 2s.
func Run(ctx context.Context, addr string) error {
	return runFleet(ctx, addr, NewFleet(), PublishInterval, dialPaho)
}

func runFleet(ctx context.Context, addr string, fleet []Vehicle, interval time.Duration, dial dialFunc) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		mu    sync.Mutex
		first error
	)
	record := func(err error) {
		if err == nil || errors.Is(err, context.Canceled) {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if first == nil {
			first = err
			cancel()
		}
	}

	var wg sync.WaitGroup
	for _, v := range fleet {
		if v.IsOffline {
			continue
		}
		wg.Go(func() {
			if err := runVehicle(ctx, addr, v, interval, dial); err != nil {
				record(fmt.Errorf("vehicle %s: %w", v.VIN, err))
			}
		})
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	return first
}

func runVehicle(ctx context.Context, addr string, v Vehicle, interval time.Duration, dial dialFunc) (err error) {
	client, err := dial(ctx, addr, v.VIN)
	if err != nil {
		return err
	}
	defer func() {
		dErr := client.Disconnect(context.WithoutCancel(ctx))
		if dErr != nil && err == nil {
			err = fmt.Errorf("disconnect: %w", dErr)
		}
	}()
	err = publishLoop(ctx, v, interval, client)
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

func publishLoop(ctx context.Context, v Vehicle, interval time.Duration, pub publisher) error {
	payload, err := EncodeTelemetry(v)
	if err != nil {
		return err
	}
	topic := TelemetryTopic(v.VIN)

	if err := pub.Publish(ctx, topic, payload); err != nil {
		return err
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := pub.Publish(ctx, topic, payload); err != nil {
				return err
			}
		}
	}
}

func plate(i int) string {
	vowels := []byte("AEIOU")
	return fmt.Sprintf("B%cL%dA%02d", vowels[i%len(vowels)], i%10, i+1)
}

type pahoClient struct {
	c *paho.Client
}

func dialPaho(ctx context.Context, addr, clientID string) (mqttClient, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("dial mqtt: %w", err)
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
		return nil, fmt.Errorf("mqtt connect: %w", err)
	}
	if ca.ReasonCode != 0 {
		_ = conn.Close()
		return nil, fmt.Errorf("mqtt connect refused: reason %d", ca.ReasonCode)
	}
	return &pahoClient{c: c}, nil
}

func (p *pahoClient) Publish(ctx context.Context, topic string, payload []byte) error {
	if _, err := p.c.Publish(ctx, &paho.Publish{
		Topic:   topic,
		QoS:     0,
		Payload: payload,
	}); err != nil {
		return fmt.Errorf("publish %s: %w", topic, err)
	}
	return nil
}

func (p *pahoClient) Disconnect(context.Context) error {
	if err := p.c.Disconnect(&paho.Disconnect{ReasonCode: 0}); err != nil {
		return fmt.Errorf("mqtt disconnect: %w", err)
	}
	return nil
}
