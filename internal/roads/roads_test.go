package roads

import (
	"math"
	"os"
	"testing"
)

func TestLoad(t *testing.T) {
	t.Parallel()

	g := loadTiny(t)
	if len(g.Nodes) != 5 {
		t.Fatalf("nodes = %d, want 5", len(g.Nodes))
	}
	if g.legal != 6 {
		t.Fatalf("legal edges = %d, want 6", g.legal)
	}
	if got := outgoingTo(g, 1); containsNode(g, got, 4) {
		t.Fatal("junction outgoing includes reverse of one-way east inbound")
	}
}

func TestGraph_Snap(t *testing.T) {
	t.Parallel()

	g := loadTiny(t)
	c := g.Snap(0.0005, 0)
	if math.Abs(c.Lat-0.0005) > 1e-6 || math.Abs(c.Lng) > 1e-6 {
		t.Fatalf("snap = (%v,%v), want mid north arm", c.Lat, c.Lng)
	}
	e := g.Edges[c.Edge]
	ns := (e.From == 0 && e.To == 1) || (e.From == 1 && e.To == 0)
	if !ns {
		t.Fatalf("snapped to %d->%d, want a north-south arm edge", e.From, e.To)
	}
	if math.Abs(c.Along-55.5) > 1 {
		t.Fatalf("along = %v, want ~55.5", c.Along)
	}
}

func TestGraph_Advance(t *testing.T) {
	t.Parallel()

	g := loadTiny(t)

	t.Run("crosses node with forced west pick", func(t *testing.T) {
		t.Parallel()

		c := g.Snap(0.0005, 0)
		c = g.Advance(c, 80, func(_ int, outgoing []int) int {
			for _, ei := range outgoing {
				if g.Edges[ei].To == 2 {
					return ei
				}
			}
			t.Fatal("west edge was not offered at the junction")
			return outgoing[0]
		})
		if c.Lng >= 0 {
			t.Fatalf("lng = %v, want west of the junction", c.Lng)
		}
		if g.Edges[c.Edge].To != 2 {
			t.Fatalf("edge to %d, want west node 2", g.Edges[c.Edge].To)
		}
	})

	t.Run("respects oneway", func(t *testing.T) {
		t.Parallel()

		c := g.Snap(0, 0.001)
		if g.Edges[c.Edge].From != 4 || g.Edges[c.Edge].To != 1 {
			t.Fatalf("snap edge = %d->%d, want 4->1", g.Edges[c.Edge].From, g.Edges[c.Edge].To)
		}
		c = g.Advance(c, 250, func(_ int, outgoing []int) int {
			for _, ei := range outgoing {
				if g.Edges[ei].To == 4 {
					t.Error("pick was offered the reverse of a one-way")
				}
			}
			return outgoing[0]
		})
		if g.Edges[c.Edge].To == 4 || g.Edges[c.Edge].From == 4 {
			t.Fatal("walked back onto the one-way inbound")
		}
	})

	t.Run("dead-end reverses", func(t *testing.T) {
		t.Parallel()

		west := edgeFromTo(t, g, 1, 2)
		c := g.Advance(Cursor{Edge: west, Along: 0}, 200, func(from int, outgoing []int) int {
			t.Fatalf("pick called at node %d with %d outgoing", from, len(outgoing))
			return 0
		})
		if c.Heading < 45 || c.Heading > 135 {
			t.Fatalf("heading = %d, want east after reverse", c.Heading)
		}
		if c.Lng <= -0.001+1e-9 {
			t.Fatal("still sitting on the west dead-end")
		}
		if c.Lng >= 0 {
			t.Fatalf("lng = %v, want still on the west arm", c.Lng)
		}
	})
}

func TestDefault_GreedyWestReachesPastCentro(t *testing.T) {
	t.Parallel()

	g := Default()
	if len(g.Edges) == 0 {
		t.Fatal("embedded centro graph has no edges")
	}
	c := g.Snap(-23.55, -46.63)
	const west = -46.685
	const step = 200.0
	const maxMeters = 20_000.0
	walked := 0.0
	pick := g.NewWestPicker()
	for walked < maxMeters && c.Lng >= west {
		c = g.Advance(c, step, pick)
		walked += step
	}
	if c.Lng >= west {
		t.Fatalf("greedy west from (-23.55,-46.63) ended at lng %v after %.0fm, want < %v", c.Lng, walked, west)
	}
}

func loadTiny(t *testing.T) *Graph {
	t.Helper()
	f, err := os.Open("testdata/tiny.json")
	if err != nil {
		t.Fatalf("open tiny.json: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })
	g, err := Load(f)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	return g
}

func edgeFromTo(t *testing.T, g *Graph, from, to int) int {
	t.Helper()
	for i, e := range g.Edges {
		if e.From == from && e.To == to {
			return i
		}
	}
	t.Fatalf("missing edge %d->%d", from, to)
	return -1
}

func outgoingTo(g *Graph, node int) []int {
	return append([]int{}, g.out[node]...)
}

func containsNode(g *Graph, edges []int, to int) bool {
	for _, ei := range edges {
		if g.Edges[ei].To == to {
			return true
		}
	}
	return false
}
