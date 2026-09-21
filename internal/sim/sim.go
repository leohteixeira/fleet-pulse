// Package sim builds the demo fleet and publishes telemetry through real MQTT clients.
package sim

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/eclipse/paho.golang/paho"

	"github.com/leohteixeira/fleet-pulse/internal/roads"
	"github.com/leohteixeira/fleet-pulse/internal/routes"
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
	// WanderVIN walks west across Centro's western edge over telemetry ticks.
	WanderVIN      = "FPULSESAO00000002"
	wanderStep     = 0.012
	wanderFloor    = -46.70
	kmPerDegLat    = 111.0
	visualScale    = 1.2
	envSimSeed     = "SIM_SEED"
	defaultSimSeed = uint64(20260921)
)

type incidentKind int

const (
	incidentNone incidentKind = iota
	incidentOffline
	incidentLowBattery
	incidentSignalLoss
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
	VIN              string
	DisplayID        string
	Plate            string
	Model            string
	Lat              float64
	Lng              float64
	Battery          int
	Speed            int
	Heading          int
	Ignition         bool
	Locked           bool
	Odometer         float64
	Trip             float64
	IsOffline        bool
	Prefix           string
	RouteID          string
	refuse           func() bool
	edge             int
	along            float64
	onGraph          bool
	westPick         func(from int, outgoing []int) int
	routeIdx         int
	routeAlong       float64
	rng              *rand.Rand
	ticks            int
	incident         incidentState
	nextIncidentAt   int
	nextIncidentKind incidentKind
	blocked          bool
	unlockPending    bool
}

type incidentState struct {
	kind      incidentKind
	remaining int
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

type commandSubscriber interface {
	Subscribe(ctx context.Context, topic string, handler func([]byte)) error
}

type mqttClient interface {
	publisher
	commandSubscriber
	Disconnect(ctx context.Context) error
}

type dialFunc func(ctx context.Context, addr, clientID string) (mqttClient, error)

// NewFleet returns 20 vehicles around Centro, including exactly one offline VIN.
func NewFleet() []Vehicle {
	g := roads.Default()
	ids := routes.Default().RouteIDs()
	src := newRNG(1)
	fleet := make([]Vehicle, 0, FleetSize)
	for i := range FleetSize {
		n := i + 1
		vin := fmt.Sprintf("FPULSESAO%08d", n)
		v := Vehicle{
			VIN:              vin,
			DisplayID:        fmt.Sprintf("V%02d", n),
			Plate:            plate(i),
			Model:            models[i%len(models)],
			Lat:              CentroLat + (float64(i%5)-2)*0.004,
			Lng:              CentroLng + (float64(i/5)-1.5)*0.006,
			Battery:          22 + (i*4)%76,
			Speed:            parkedSpeed(i),
			Heading:          (i * 18) % 360,
			Ignition:         i%3 != 0,
			Locked:           i%3 == 0,
			Odometer:         4200 + float64(i)*1000,
			Trip:             float64((i%7)+1) * 0.4,
			IsOffline:        vin == OfflineVIN,
			Prefix:           PrefixFleet,
			RouteID:          ids[src.IntN(len(ids))],
			routeIdx:         -1,
			rng:              newRNG(uint64(n)),
			nextIncidentAt:   40 + src.IntN(40),
			nextIncidentKind: incidentKind(1 + src.IntN(3)),
		}
		applyCursor(&v, g.Snap(v.Lat, v.Lng))
		fleet = append(fleet, v)
	}
	return fleet
}

// Seed is the process RNG seed from SIM_SEED (invalid or empty → default).
func Seed() uint64 {
	return processSeed()
}

func processSeed() uint64 {
	raw := strings.TrimSpace(os.Getenv(envSimSeed))
	if raw == "" {
		return defaultSimSeed
	}
	n, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return defaultSimSeed
	}
	return n
}

func newRNG(stream uint64) *rand.Rand {
	return rand.New(rand.NewPCG(processSeed(), stream))
}

// TelemetryTopic is the device publish topic for vin.
func TelemetryTopic(vin string) string {
	return "fleet/" + vin + "/telemetry"
}

// CommandTopic is the device subscribe topic for door commands.
func CommandTopic(vin string) string {
	return "fleet/" + vin + "/commands"
}

// AckTopic is the device publish topic for command acknowledgements.
func AckTopic(vin string) string {
	return "fleet/" + vin + "/ack"
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

// Run connects each online rental and leasing vehicle as a real MQTT client
// and publishes every 2s. Leasing clients use leasing/{vin}/ topics.
func Run(ctx context.Context, addr string) error {
	rental := NewFleet()
	leasing := NewLeasingFleet()
	fleet := make([]Vehicle, 0, len(rental)+len(leasing))
	fleet = append(fleet, rental...)
	fleet = append(fleet, leasing...)
	return runFleet(ctx, addr, fleet, PublishInterval, dialPaho)
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

	cmds := make(chan []byte, 8)
	if err := client.Subscribe(ctx, v.commandTopic(), func(payload []byte) {
		select {
		case cmds <- payload:
		case <-ctx.Done():
		}
	}); err != nil {
		return fmt.Errorf("subscribe commands: %w", err)
	}

	err = publishLoop(ctx, v, interval, client, cmds)
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

func publishLoop(ctx context.Context, v Vehicle, interval time.Duration, pub publisher, cmds <-chan []byte) error {
	if !skipPublish(&v) {
		if err := publishTelemetry(ctx, v, pub); err != nil {
			return err
		}
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			stepVehicle(&v)
			if skipPublish(&v) {
				continue
			}
			if err := publishTelemetry(ctx, v, pub); err != nil {
				return err
			}
		case cmd := <-cmds:
			if err := ackCommand(ctx, &v, cmd, pub); err != nil {
				return err
			}
		}
	}
}

func skipPublish(v *Vehicle) bool {
	if v.rng == nil {
		return false
	}
	v.ticks++
	if v.incident.remaining > 0 {
		v.incident.remaining--
		skip := false
		switch v.incident.kind {
		case incidentOffline:
			skip = true
		case incidentSignalLoss:
			skip = v.rng.IntN(2) == 0
		case incidentLowBattery:
			if v.Battery > 15 {
				v.Battery = 8 + v.rng.IntN(8)
			}
		}
		if v.incident.remaining == 0 {
			v.incident.kind = incidentNone
			v.nextIncidentAt = v.ticks + 2 + v.rng.IntN(8)
			v.nextIncidentKind = incidentKind(1 + v.rng.IntN(3))
		}
		return skip
	}
	if v.nextIncidentAt > 0 && v.ticks >= v.nextIncidentAt {
		v.incident.kind = v.nextIncidentKind
		v.incident.remaining = 3 + v.rng.IntN(4)
		v.nextIncidentAt = 0
		switch v.incident.kind {
		case incidentLowBattery:
			if v.Battery > 15 {
				v.Battery = 8 + v.rng.IntN(8)
			}
		case incidentOffline, incidentSignalLoss:
			return true
		}
	}
	return false
}

func parkedSpeed(i int) int {
	if i%3 == 0 {
		return 0
	}
	return 18 + (i*3)%40
}

func stepVehicle(v *Vehicle) {
	if v.IsOffline {
		return
	}
	if v.blocked || v.unlockPending {
		v.Ignition = false
		return
	}
	if v.VIN == WanderVIN {
		if v.Lng > wanderFloor {
			g := ensureOnGraph(v)
			if v.westPick == nil {
				v.westPick = g.NewWestPicker()
			}
			startLat, startLng := v.Lat, v.Lng
			meters := wanderMeters(v.Lat)
			applyCursor(v, g.Advance(cursorOf(v), meters, v.westPick))
			v.Heading = displacementHeading(startLat, startLng, v.Lat, v.Lng)
			v.Odometer += meters / 1000
			v.Trip += meters / 1000
		}
		if v.Speed <= 0 {
			v.Speed = 22
		}
		v.Ignition = true
		v.Locked = false
		return
	}
	if !v.Ignition || v.Speed <= 0 {
		return
	}
	roll := rand.IntN
	if v.rng != nil {
		roll = v.rng.IntN
	}
	if roll(100) < 8 {
		v.Speed = max(8, min(70, v.Speed+roll(25)-12))
	}
	stepAlongRoute(v, routes.Default())
}

func stepAlongRoute(v *Vehicle, lib *routes.Library) {
	r, ok := lib.Lookup(v.RouteID)
	if !ok || len(r.Points) < 2 {
		ids := lib.RouteIDs()
		if len(ids) == 0 {
			return
		}
		pick := 0
		if v.rng != nil {
			pick = v.rng.IntN(len(ids))
		}
		v.RouteID = ids[pick]
		r, ok = lib.Lookup(v.RouteID)
		if !ok || len(r.Points) < 2 {
			return
		}
		v.routeIdx = -1
	}
	km := float64(v.Speed) * PublishInterval.Hours() * visualScale
	if km <= 0 {
		return
	}
	meters := km * 1000
	if v.routeIdx < 0 || v.routeIdx >= len(r.Points)-1 {
		v.routeIdx, v.routeAlong = nearestOnRoute(r.Points, v.Lat, v.Lng)
	}
	for meters > 1e-6 && v.routeIdx < len(r.Points)-1 {
		a, b := r.Points[v.routeIdx], r.Points[v.routeIdx+1]
		seg := roads.Distance(a.Lat, a.Lng, b.Lat, b.Lng)
		if seg <= 1e-6 {
			v.routeIdx++
			v.routeAlong = 0
			continue
		}
		remain := seg - v.routeAlong
		if remain <= 1e-6 {
			v.routeIdx++
			v.routeAlong = 0
			continue
		}
		if meters <= remain {
			t := (v.routeAlong + meters) / seg
			v.Lat = a.Lat + t*(b.Lat-a.Lat)
			v.Lng = a.Lng + t*(b.Lng-a.Lng)
			v.Heading = displacementHeading(a.Lat, a.Lng, b.Lat, b.Lng)
			v.routeAlong += meters
			meters = 0
			break
		}
		meters -= remain
		v.routeIdx++
		v.routeAlong = 0
		v.Lat, v.Lng = b.Lat, b.Lng
		v.Heading = displacementHeading(a.Lat, a.Lng, b.Lat, b.Lng)
	}
	if v.routeIdx >= len(r.Points)-1 {
		ids := lib.RouteIDs()
		if len(ids) > 0 {
			if v.rng != nil {
				v.RouteID = ids[v.rng.IntN(len(ids))]
			} else {
				v.RouteID = ids[0]
			}
			if next, ok := lib.Lookup(v.RouteID); ok && len(next.Points) >= 2 {
				v.routeIdx, v.routeAlong = nearestOnRoute(next.Points, v.Lat, v.Lng)
			} else {
				v.routeIdx = -1
				v.routeAlong = 0
			}
		} else {
			v.routeIdx = 0
			v.routeAlong = 0
		}
	}
	v.Odometer += km
	v.Trip += km
}

func nearestOnRoute(pts []routes.Point, lat, lng float64) (idx int, along float64) {
	bestDist := math.MaxFloat64
	for i := 0; i < len(pts)-1; i++ {
		alongSeg, dist := projectRoute(pts[i], pts[i+1], lat, lng)
		if dist < bestDist {
			bestDist = dist
			idx = i
			along = alongSeg
		}
	}
	return idx, along
}

func projectRoute(a, b routes.Point, lat, lng float64) (along, dist float64) {
	seg := roads.Distance(a.Lat, a.Lng, b.Lat, b.Lng)
	if seg <= 1e-6 {
		return 0, roads.Distance(lat, lng, a.Lat, a.Lng)
	}
	midLat := (a.Lat + b.Lat) / 2
	cosLat := math.Cos(midLat * math.Pi / 180)
	if cosLat == 0 {
		cosLat = 1
	}
	bx := (b.Lng - a.Lng) * kmPerDegLat * cosLat * 1000
	by := (b.Lat - a.Lat) * kmPerDegLat * 1000
	px := (lng - a.Lng) * kmPerDegLat * cosLat * 1000
	py := (lat - a.Lat) * kmPerDegLat * 1000
	ab2 := bx*bx + by*by
	t := 0.0
	if ab2 > 0 {
		t = (px*bx + py*by) / ab2
	}
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	qx := t * bx
	qy := t * by
	return t * seg, math.Hypot(px-qx, py-qy)
}

func applyCursor(v *Vehicle, c roads.Cursor) {
	v.Lat = c.Lat
	v.Lng = c.Lng
	v.Heading = c.Heading
	v.edge = c.Edge
	v.along = c.Along
	v.onGraph = true
}

func cursorOf(v *Vehicle) roads.Cursor {
	return roads.Cursor{
		Edge:    v.edge,
		Along:   v.along,
		Lat:     v.Lat,
		Lng:     v.Lng,
		Heading: v.Heading,
	}
}

func ensureOnGraph(v *Vehicle) *roads.Graph {
	g := roads.Default()
	if !v.onGraph {
		applyCursor(v, g.Snap(v.Lat, v.Lng))
	}
	return g
}

func displacementHeading(fromLat, fromLng, toLat, toLng float64) int {
	h := int(math.Round(math.Atan2(toLng-fromLng, toLat-fromLat) * 180 / math.Pi))
	h %= 360
	if h < 0 {
		h += 360
	}
	return h
}

func wanderMeters(lat float64) float64 {
	cosLat := math.Cos(lat * math.Pi / 180)
	if cosLat == 0 {
		cosLat = 1
	}
	return wanderStep * kmPerDegLat * cosLat * 1000
}

func publishTelemetry(ctx context.Context, v Vehicle, pub publisher) error {
	payload, err := EncodeTelemetry(v)
	if err != nil {
		return err
	}
	return pub.Publish(ctx, v.telemetryTopic(), payload)
}

func (v Vehicle) rollRefuse() bool {
	if v.refuse != nil {
		return v.refuse()
	}
	return rand.IntN(10) == 0
}

func ackCommand(ctx context.Context, v *Vehicle, payload []byte, pub publisher) error {
	var cmd struct {
		ID     string `json:"id"`
		Action string `json:"action"`
	}
	if err := json.Unmarshal(payload, &cmd); err != nil || cmd.ID == "" {
		return nil
	}
	ok := commandAckOK(v, cmd.Action)
	if ok {
		switch cmd.Action {
		case "lock":
			v.Locked = true
		case "unlock":
			v.Locked = false
			v.blocked = false
			v.unlockPending = false
			if v.topicPrefix() == PrefixLeasing {
				v.Ignition = true
				if v.Speed <= 0 {
					v.Speed = 18
				}
			}
		case "block":
			v.blocked = true
			v.unlockPending = false
			v.Ignition = false
			v.Speed = 0
		}
		offline := v.incident.kind == incidentOffline || v.incident.kind == incidentSignalLoss
		if !offline {
			_ = publishTelemetry(ctx, *v, pub)
		}
	}
	raw, err := json.Marshal(struct {
		CommandID string `json:"commandId"`
		OK        bool   `json:"ok"`
	}{CommandID: cmd.ID, OK: ok})
	if err != nil {
		return fmt.Errorf("encode ack: %w", err)
	}
	return pub.Publish(ctx, v.ackTopic(), raw)
}

func commandAckOK(v *Vehicle, action string) bool {
	switch action {
	case "block":
		return !v.RefuseBlock()
	case "unlock":
		if v.topicPrefix() == PrefixLeasing {
			return true
		}
		return !v.rollRefuse()
	default:
		return !v.rollRefuse()
	}
}

func plate(i int) string {
	vowels := []byte("AEIOU")
	return fmt.Sprintf("B%cL%dA%02d", vowels[i%len(vowels)], i%10, i+1)
}

type pahoClient struct {
	c      *paho.Client
	router *paho.StandardRouter
}

func dialPaho(ctx context.Context, addr, clientID string) (mqttClient, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("dial mqtt: %w", err)
	}

	router := paho.NewStandardRouter()
	c := paho.NewClient(paho.ClientConfig{
		ClientID: clientID,
		Conn:     conn,
		Router:   router,
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
	return &pahoClient{c: c, router: router}, nil
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

func (p *pahoClient) Subscribe(ctx context.Context, topic string, handler func([]byte)) error {
	p.router.RegisterHandler(topic, func(pub *paho.Publish) {
		handler(bytes.Clone(pub.Payload))
	})
	if _, err := p.c.Subscribe(ctx, &paho.Subscribe{
		Subscriptions: []paho.SubscribeOptions{
			{Topic: topic, QoS: 0},
		},
	}); err != nil {
		return fmt.Errorf("subscribe %s: %w", topic, err)
	}
	return nil
}

func (p *pahoClient) Disconnect(context.Context) error {
	if err := p.c.Disconnect(&paho.Disconnect{ReasonCode: 0}); err != nil {
		return fmt.Errorf("mqtt disconnect: %w", err)
	}
	return nil
}
