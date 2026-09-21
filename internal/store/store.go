// Package store keeps last-known fleet state in memory.
package store

import (
	"cmp"
	"slices"
	"sync"
)

// Centro is the allowed rectangle published on the snapshot.
var Centro = Polygon{
	South: -23.585,
	North: -23.525,
	West:  -46.685,
	East:  -46.60,
}

// Polygon is the allowed area the snapshot exposes for the map.
type Polygon struct {
	South float64 `json:"south"`
	North float64 `json:"north"`
	West  float64 `json:"west"`
	East  float64 `json:"east"`
}

// Vehicle is last-known state for one VIN.
type Vehicle struct {
	VIN       string  `json:"vin"`
	DisplayID string  `json:"displayId"`
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
}

// Snapshot is the fleet view served by GET /api/vehicles.
type Snapshot struct {
	Polygon  Polygon   `json:"polygon"`
	Vehicles []Vehicle `json:"vehicles"`
}

// Memory is a concurrent in-memory fleet keyed by VIN.
type Memory struct {
	mu       sync.RWMutex
	vehicles map[string]Vehicle
	polygon  Polygon
}

// New returns an empty store that already owns the Centro polygon.
func New() *Memory {
	return &Memory{
		vehicles: make(map[string]Vehicle),
		polygon:  Centro,
	}
}

// Seed records the roster so every VIN, including the silent one, exists before telemetry.
func (m *Memory) Seed(vehicles []Vehicle) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.vehicles == nil {
		m.vehicles = make(map[string]Vehicle, len(vehicles))
	}
	for _, v := range vehicles {
		if v.VIN == "" {
			continue
		}
		m.vehicles[v.VIN] = v
	}
}

// Apply writes last-known fields for a VIN after a successful ingest parse.
func (m *Memory) Apply(update Vehicle) {
	if update.VIN == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.vehicles == nil {
		m.vehicles = make(map[string]Vehicle)
	}
	cur := m.vehicles[update.VIN]
	cur.VIN = update.VIN
	if update.DisplayID != "" {
		cur.DisplayID = update.DisplayID
	}
	cur.Lat = update.Lat
	cur.Lng = update.Lng
	cur.Plate = update.Plate
	cur.Model = update.Model
	cur.Battery = update.Battery
	cur.Speed = update.Speed
	cur.Heading = update.Heading
	cur.Ignition = update.Ignition
	cur.Locked = update.Locked
	cur.Odometer = update.Odometer
	cur.Trip = update.Trip
	m.vehicles[update.VIN] = cur
}

// Snapshot copies current last-known state and the Centro polygon.
func (m *Memory) Snapshot() Snapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()

	vehicles := make([]Vehicle, 0, len(m.vehicles))
	for _, v := range m.vehicles {
		vehicles = append(vehicles, v)
	}
	slices.SortFunc(vehicles, func(a, b Vehicle) int {
		return cmp.Compare(a.VIN, b.VIN)
	})
	return Snapshot{
		Polygon:  m.polygon,
		Vehicles: vehicles,
	}
}
