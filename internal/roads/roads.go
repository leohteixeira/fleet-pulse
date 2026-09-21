// Package roads loads a directed street graph and walks a cursor along its edges.
package roads

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sync"
)

//go:embed centro.json
var centroJSON []byte

const (
	kmPerDeg = 111.0
	meterEps = 1e-6
)

var (
	defaultOnce  sync.Once
	defaultGraph *Graph
	defaultErr   error
)

// Node is a geographic vertex in the compact graph.
type Node struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

// Edge is a directed segment between node indices.
type Edge struct {
	From   int     `json:"from"`
	To     int     `json:"to"`
	Meters float64 `json:"meters"`
}

// Graph is a directed road mesh with adjacency built at load time.
type Graph struct {
	Nodes []Node
	Edges []Edge
	out   [][]int
	legal int
}

// Cursor is a position along one directed edge.
type Cursor struct {
	Edge    int
	Along   float64
	Lat     float64
	Lng     float64
	Heading int
}

type fileGraph struct {
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
}

// Load decodes a compact graph and builds outgoing adjacency.
func Load(r io.Reader) (*Graph, error) {
	var raw fileGraph
	if err := json.NewDecoder(r).Decode(&raw); err != nil {
		return nil, fmt.Errorf("roads: decode graph: %w", err)
	}
	if len(raw.Nodes) == 0 || len(raw.Edges) == 0 {
		return nil, fmt.Errorf("roads: empty graph")
	}

	nodes := make([]Node, len(raw.Nodes))
	copy(nodes, raw.Nodes)

	edges := make([]Edge, 0, len(raw.Edges))
	for i, e := range raw.Edges {
		if e.From < 0 || e.From >= len(nodes) || e.To < 0 || e.To >= len(nodes) {
			return nil, fmt.Errorf("roads: edge %d node index out of range", i)
		}
		if e.From == e.To || e.Meters <= 0 {
			continue
		}
		edges = append(edges, e)
	}
	if len(edges) == 0 {
		return nil, fmt.Errorf("roads: no usable edges")
	}

	g := &Graph{
		Nodes: nodes,
		Edges: edges,
		out:   make([][]int, len(nodes)),
		legal: len(edges),
	}
	for i, e := range g.Edges {
		g.out[e.From] = append(g.out[e.From], i)
	}
	g.addHiddenReverses()
	return g, nil
}

// Default returns the embedded Centro extract. Invalid JSON fails the process.
func Default() *Graph {
	defaultOnce.Do(func() {
		defaultGraph, defaultErr = Load(bytes.NewReader(centroJSON))
	})
	if defaultErr != nil {
		panic(defaultErr)
	}
	return defaultGraph
}

// Snap projects lat/lng onto the nearest legal edge (t clamped to 0..1).
func (g *Graph) Snap(lat, lng float64) Cursor {
	if g == nil || g.legal == 0 {
		return Cursor{}
	}
	best := Cursor{}
	bestDist := math.MaxFloat64
	for i := range g.legal {
		e := g.Edges[i]
		along, plat, plng, dist := projectOnto(g.Nodes[e.From], g.Nodes[e.To], lat, lng, e.Meters)
		if dist < bestDist {
			bestDist = dist
			best = Cursor{
				Edge:    i,
				Along:   along,
				Lat:     plat,
				Lng:     plng,
				Heading: segmentHeading(g.Nodes[e.From], g.Nodes[e.To]),
			}
		}
	}
	return best
}

// Advance walks meters along edges. pick chooses an outgoing edge index at a node.
// A dead-end reverses on the same geometry. One-way legal exits stay in outgoing.
func (g *Graph) Advance(c Cursor, meters float64, pick func(from int, outgoing []int) int) Cursor {
	if g == nil || len(g.Edges) == 0 || meters <= 0 {
		return g.locate(c)
	}
	if c.Edge < 0 || c.Edge >= len(g.Edges) {
		return g.locate(Cursor{})
	}
	if c.Along < 0 {
		c.Along = 0
	}

	const maxHops = 100_000
	for hop := 0; hop < maxHops && meters > meterEps; hop++ {
		e := g.Edges[c.Edge]
		if e.Meters <= meterEps {
			next, ok := g.leaveNode(e.To, c.Edge, pick)
			if !ok {
				return g.locate(c)
			}
			c.Edge = next
			c.Along = 0
			continue
		}
		if c.Along > e.Meters {
			c.Along = e.Meters
		}
		remain := e.Meters - c.Along
		if remain <= meterEps {
			next, ok := g.leaveNode(e.To, c.Edge, pick)
			if !ok {
				return g.locate(c)
			}
			c.Edge = next
			c.Along = 0
			continue
		}
		if meters <= remain {
			c.Along += meters
			return g.locate(c)
		}
		meters -= remain
		next, ok := g.leaveNode(e.To, c.Edge, pick)
		if !ok {
			c.Along = e.Meters
			return g.locate(c)
		}
		c.Edge = next
		c.Along = 0
	}
	return g.locate(c)
}

// Distance is the equirectangular distance in meters.
func Distance(aLat, aLng, bLat, bLng float64) float64 {
	return metersBetween(Node{Lat: aLat, Lng: aLng}, Node{Lat: bLat, Lng: bLng})
}

// NewWestPicker returns a greedy west pick. It prefers unused westbound exits, then
// any westbound exit, then an unused escape, so a city block cannot trap the walk.
func (g *Graph) NewWestPicker() func(from int, outgoing []int) int {
	used := map[int]int{}
	return func(from int, outgoing []int) int {
		chosen := pickWestProgress(g, from, outgoing, used)
		used[chosen]++
		return chosen
	}
}

func pickWestProgress(g *Graph, from int, outgoing []int, used map[int]int) int {
	fromLng := g.Nodes[from].Lng
	bestUnusedWest, bestWest, bestUnused, bestAny := -1, -1, -1, outgoing[0]
	var unusedWestLng, westLng, unusedLng, anyLng float64
	anyLng = g.Nodes[g.Edges[bestAny].To].Lng

	for _, ei := range outgoing {
		lng := g.Nodes[g.Edges[ei].To].Lng
		if lng < anyLng {
			bestAny = ei
			anyLng = lng
		}
		if used[ei] == 0 {
			if bestUnused < 0 || lng < unusedLng {
				bestUnused = ei
				unusedLng = lng
			}
			if lng < fromLng && (bestUnusedWest < 0 || lng < unusedWestLng) {
				bestUnusedWest = ei
				unusedWestLng = lng
			}
		}
		if lng < fromLng && (bestWest < 0 || lng < westLng) {
			bestWest = ei
			westLng = lng
		}
	}
	switch {
	case bestUnusedWest >= 0:
		return bestUnusedWest
	case bestWest >= 0 && used[bestWest] == 0:
		return bestWest
	case bestUnused >= 0:
		return bestUnused
	case bestWest >= 0:
		return bestWest
	default:
		return bestAny
	}
}

// ReverseOf reports whether edge b is the reverse of edge a.
func (g *Graph) ReverseOf(a, b int) bool {
	if a < 0 || b < 0 || a >= len(g.Edges) || b >= len(g.Edges) {
		return false
	}
	ea, eb := g.Edges[a], g.Edges[b]
	return ea.From == eb.To && ea.To == eb.From
}

func (g *Graph) leaveNode(node, arrived int, pick func(from int, outgoing []int) int) (int, bool) {
	outgoing := g.out[node]
	if len(outgoing) == 0 {
		rev := g.reverseIndex(arrived)
		if rev < 0 {
			return 0, false
		}
		return rev, true
	}
	if pick == nil {
		return outgoing[0], true
	}
	chosen := pick(node, outgoing)
	if !containsEdge(outgoing, chosen) {
		return outgoing[0], true
	}
	return chosen, true
}

func (g *Graph) reverseIndex(edgeIdx int) int {
	e := g.Edges[edgeIdx]
	for i, other := range g.Edges {
		if other.From == e.To && other.To == e.From {
			return i
		}
	}
	return -1
}

func (g *Graph) addHiddenReverses() {
	n := len(g.Edges)
	have := make(map[[2]int]struct{}, n)
	for _, e := range g.Edges {
		have[[2]int{e.From, e.To}] = struct{}{}
	}
	hidden := make([]Edge, 0)
	for _, e := range g.Edges[:n] {
		key := [2]int{e.To, e.From}
		if _, ok := have[key]; ok {
			continue
		}
		have[key] = struct{}{}
		hidden = append(hidden, Edge{From: e.To, To: e.From, Meters: e.Meters})
	}
	g.Edges = append(g.Edges, hidden...)
}

func (g *Graph) locate(c Cursor) Cursor {
	if g == nil || len(g.Edges) == 0 {
		return c
	}
	if c.Edge < 0 || c.Edge >= len(g.Edges) {
		c.Edge = 0
		c.Along = 0
	}
	e := g.Edges[c.Edge]
	if e.Meters <= 0 {
		n := g.Nodes[e.From]
		c.Lat, c.Lng = n.Lat, n.Lng
		c.Heading = segmentHeading(n, g.Nodes[e.To])
		return c
	}
	t := c.Along / e.Meters
	if t < 0 {
		t = 0
		c.Along = 0
	}
	if t > 1 {
		t = 1
		c.Along = e.Meters
	}
	a, b := g.Nodes[e.From], g.Nodes[e.To]
	c.Lat = a.Lat + t*(b.Lat-a.Lat)
	c.Lng = a.Lng + t*(b.Lng-a.Lng)
	c.Heading = segmentHeading(a, b)
	return c
}

func projectOnto(a, b Node, lat, lng, meters float64) (along, plat, plng, dist float64) {
	midLat := (a.Lat + b.Lat) / 2
	cosLat := math.Cos(midLat * math.Pi / 180)
	if cosLat == 0 {
		cosLat = 1
	}
	ax, ay := 0.0, 0.0
	bx := (b.Lng - a.Lng) * kmPerDeg * cosLat * 1000
	by := (b.Lat - a.Lat) * kmPerDeg * 1000
	px := (lng - a.Lng) * kmPerDeg * cosLat * 1000
	py := (lat - a.Lat) * kmPerDeg * 1000
	abx, aby := bx-ax, by-ay
	ab2 := abx*abx + aby*aby
	t := 0.0
	if ab2 > 0 {
		t = ((px-ax)*abx + (py-ay)*aby) / ab2
	}
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	qx := ax + t*abx
	qy := ay + t*aby
	along = t * meters
	plat = a.Lat + t*(b.Lat-a.Lat)
	plng = a.Lng + t*(b.Lng-a.Lng)
	dist = math.Hypot(px-qx, py-qy)
	return along, plat, plng, dist
}

func metersBetween(a, b Node) float64 {
	midLat := (a.Lat + b.Lat) / 2
	cosLat := math.Cos(midLat * math.Pi / 180)
	if cosLat == 0 {
		cosLat = 1
	}
	dLat := (b.Lat - a.Lat) * kmPerDeg
	dLng := (b.Lng - a.Lng) * kmPerDeg * cosLat
	return math.Hypot(dLat, dLng) * 1000
}

func segmentHeading(from, to Node) int {
	h := int(math.Round(math.Atan2(to.Lng-from.Lng, to.Lat-from.Lat) * 180 / math.Pi))
	h %= 360
	if h < 0 {
		h += 360
	}
	return h
}

func containsEdge(outgoing []int, edge int) bool {
	for _, ei := range outgoing {
		if ei == edge {
			return true
		}
	}
	return false
}
