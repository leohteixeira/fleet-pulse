// Package routes loads the versioned Greater São Paulo route library.
// The seed is a committed, embedded artifact. This package never opens a router URL.
package routes

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"
	"sync"
)

//go:embed seed.json
var seedJSON []byte

const (
	// South is the Greater SP viewport southern edge.
	South = -23.63
	// North is the Greater SP viewport northern edge.
	North = -23.49
	// West is the Greater SP viewport western edge.
	West = -46.87
	// East is the Greater SP viewport eastern edge.
	East = -46.41

	minPOIs    = 55
	maxPOIs    = 70
	minRoutes  = 280
	maxRoutes  = 320
	minPoints  = 2
	coordSlack = 1e-9
)

var (
	defaultOnce sync.Once
	defaultLib  *Library
	defaultErr  error
)

// Point is one lon/lat vertex on a route polyline.
type Point struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

// POI is a named Greater São Paulo landmark used as a route endpoint.
type POI struct {
	ID   string  `json:"id"`
	Name string  `json:"name"`
	Lat  float64 `json:"lat"`
	Lng  float64 `json:"lng"`
}

// Route is a directed polyline between two POIs.
type Route struct {
	ID     string  `json:"id"`
	From   string  `json:"from"`
	To     string  `json:"to"`
	Points []Point `json:"points"`
}

// Library is the load-only seed: POIs plus routes. Callers must not mutate it.
type Library struct {
	Version string
	POIs    []POI
	Routes  []Route
	byID    map[string]int
}

type fileLibrary struct {
	Version string  `json:"version"`
	POIs    []POI   `json:"pois"`
	Routes  []Route `json:"routes"`
}

// Parse decodes and validates a seed. A missing or corrupt payload fails
// with a wrapped error and never falls back to a router.
func Parse(raw []byte) (*Library, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, fmt.Errorf("routes: empty seed")
	}
	var file fileLibrary
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf("routes: decode seed: %w", err)
	}
	if n := len(file.POIs); n < minPOIs || n > maxPOIs {
		return nil, fmt.Errorf("routes: poi count %d is outside %d–%d", n, minPOIs, maxPOIs)
	}
	if n := len(file.Routes); n < minRoutes || n > maxRoutes {
		return nil, fmt.Errorf("routes: route count %d is outside %d–%d", n, minRoutes, maxRoutes)
	}

	pois := slices.Clone(file.POIs)
	poiIDs := make(map[string]struct{}, len(pois))
	for i, p := range pois {
		if p.ID == "" {
			return nil, fmt.Errorf("routes: poi %d missing id", i)
		}
		if _, ok := poiIDs[p.ID]; ok {
			return nil, fmt.Errorf("routes: duplicate poi id %q", p.ID)
		}
		poiIDs[p.ID] = struct{}{}
		if !InBounds(p.Lat, p.Lng) {
			return nil, fmt.Errorf("routes: poi %q outside greater sp bounds", p.ID)
		}
	}

	routes := make([]Route, 0, len(file.Routes))
	byID := make(map[string]int, len(file.Routes))
	for i, r := range file.Routes {
		if r.ID == "" {
			return nil, fmt.Errorf("routes: route %d missing id", i)
		}
		if _, ok := byID[r.ID]; ok {
			return nil, fmt.Errorf("routes: duplicate route id %q", r.ID)
		}
		if r.From == "" || r.To == "" {
			return nil, fmt.Errorf("routes: route %q missing from or to", r.ID)
		}
		if _, ok := poiIDs[r.From]; !ok {
			return nil, fmt.Errorf("routes: route %q unknown from %q", r.ID, r.From)
		}
		if _, ok := poiIDs[r.To]; !ok {
			return nil, fmt.Errorf("routes: route %q unknown to %q", r.ID, r.To)
		}
		if len(r.Points) < minPoints {
			return nil, fmt.Errorf("routes: route %q has %d points, want at least %d", r.ID, len(r.Points), minPoints)
		}
		pts := slices.Clone(r.Points)
		for j, pt := range pts {
			if !InBounds(pt.Lat, pt.Lng) {
				return nil, fmt.Errorf("routes: route %q point %d outside greater sp bounds", r.ID, j)
			}
		}
		byID[r.ID] = len(routes)
		r.Points = pts
		routes = append(routes, r)
	}

	return &Library{
		Version: file.Version,
		POIs:    pois,
		Routes:  routes,
		byID:    byID,
	}, nil
}

// Load parses the embedded seed.
func Load() (*Library, error) {
	lib, err := Parse(seedJSON)
	if err != nil {
		return nil, fmt.Errorf("routes: load embedded seed: %w", err)
	}
	return lib, nil
}

// Default returns the embedded library. A corrupt seed fails the process.
func Default() *Library {
	defaultOnce.Do(func() {
		defaultLib, defaultErr = Load()
	})
	if defaultErr != nil {
		panic(defaultErr)
	}
	return defaultLib
}

// InBounds reports whether lat/lng sits inside the Greater SP viewport.
func InBounds(lat, lng float64) bool {
	return lat+coordSlack >= South && lat-coordSlack <= North &&
		lng+coordSlack >= West && lng-coordSlack <= East
}

// RouteIDs returns the library route identifiers in seed order.
func (l *Library) RouteIDs() []string {
	if l == nil {
		return []string{}
	}
	ids := make([]string, 0, len(l.Routes))
	for _, r := range l.Routes {
		ids = append(ids, r.ID)
	}
	return ids
}

// Lookup returns a copy of the named route.
func (l *Library) Lookup(id string) (Route, bool) {
	if l == nil {
		return Route{}, false
	}
	i, ok := l.byID[id]
	if !ok {
		return Route{}, false
	}
	r := l.Routes[i]
	r.Points = slices.Clone(r.Points)
	return r, true
}
