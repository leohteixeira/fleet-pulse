// Command fetchroads downloads an Overpass extract and writes the compact Centro graph.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	south     = -23.60
	north     = -23.51
	west      = -46.71
	east      = -46.58
	kmPerDeg  = 111.0
	mergeGrid = 1e6
	colinearM = 8.0
	minEdgeM  = 1.0
)

var carHighways = map[string]struct{}{
	"motorway":       {},
	"trunk":          {},
	"primary":        {},
	"secondary":      {},
	"tertiary":       {},
	"unclassified":   {},
	"residential":    {},
	"living_street":  {},
	"motorway_link":  {},
	"trunk_link":     {},
	"primary_link":   {},
	"secondary_link": {},
	"tertiary_link":  {},
}

var overpassURLs = []string{
	"https://overpass.openstreetmap.fr/api/interpreter",
	"https://overpass.kumi.systems/api/interpreter",
	"https://overpass-api.de/api/interpreter",
}

type node struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

type edge struct {
	From   int     `json:"from"`
	To     int     `json:"to"`
	Meters float64 `json:"meters"`
}

type graph struct {
	Nodes []node `json:"nodes"`
	Edges []edge `json:"edges"`
}

type overpassResponse struct {
	Elements []overpassElement `json:"elements"`
}

type overpassElement struct {
	Type     string            `json:"type"`
	Tags     map[string]string `json:"tags"`
	Geometry []overpassPoint   `json:"geometry"`
}

type overpassPoint struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

func main() {
	out := flag.String("out", "internal/roads/centro.json", "output path")
	flag.Parse()
	if err := run(*out); err != nil {
		fmt.Fprintf(os.Stderr, "fetchroads: %v\n", err)
		os.Exit(1)
	}
}

func run(out string) error {
	raw, err := download()
	if err != nil {
		return err
	}
	g, err := compact(raw)
	if err != nil {
		return err
	}
	body, err := json.Marshal(g)
	if err != nil {
		return fmt.Errorf("encode graph: %w", err)
	}
	if err := os.WriteFile(out, append(body, '\n'), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", out, err)
	}
	fmt.Printf("wrote %s (%d nodes, %d edges, %d bytes)\n", out, len(g.Nodes), len(g.Edges), len(body)+1)
	return nil
}

func download() ([]byte, error) {
	query := fmt.Sprintf(`[out:json][timeout:90];
(
  way["highway"~"^(motorway|trunk|primary|secondary|tertiary|unclassified|residential|living_street)(_link)?$"](%g,%g,%g,%g);
);
out geom;
`, south, west, north, east)

	var last error
	for _, endpoint := range overpassURLs {
		body, err := postOverpass(endpoint, query)
		if err != nil {
			last = err
			continue
		}
		return body, nil
	}
	if last == nil {
		last = fmt.Errorf("no overpass endpoint")
	}
	return nil, last
}

func postOverpass(endpoint, query string) ([]byte, error) {
	form := url.Values{}
	form.Set("data", query)
	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "fleet-pulse-fetchroads/1.0")

	client := &http.Client{Timeout: 120 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("overpass %s: %w", endpoint, err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("overpass %s read: %w", endpoint, err)
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("overpass %s: status %d", endpoint, res.StatusCode)
	}
	return body, nil
}

func compact(raw []byte) (*graph, error) {
	var payload overpassResponse
	if err := json.NewDecoder(bytes.NewReader(raw)).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode overpass: %w", err)
	}

	type way struct {
		pts []node
		dir int
	}
	ways := make([]way, 0)
	hits := make(map[[2]int64]int)
	for _, el := range payload.Elements {
		if el.Type != "way" || len(el.Geometry) < 2 || !isCarHighway(el.Tags["highway"]) {
			continue
		}
		pts := make([]node, 0, len(el.Geometry))
		for _, p := range el.Geometry {
			pt := node{Lat: p.Lat, Lng: p.Lon}
			pts = append(pts, pt)
			hits[mergeKey(pt.Lat, pt.Lng)]++
		}
		ways = append(ways, way{pts: pts, dir: wayDir(el.Tags)})
	}

	nodes := make([]node, 0)
	index := make(map[[2]int64]int)
	lookup := func(lat, lng float64) int {
		key := mergeKey(lat, lng)
		if i, ok := index[key]; ok {
			return i
		}
		i := len(nodes)
		index[key] = i
		nodes = append(nodes, node{Lat: lat, Lng: lng})
		return i
	}

	seen := make(map[[2]int]struct{})
	edges := make([]edge, 0)
	addEdge := func(from, to int) {
		if from == to {
			return
		}
		key := [2]int{from, to}
		if _, ok := seen[key]; ok {
			return
		}
		meters := metersBetween(nodes[from], nodes[to])
		if meters < minEdgeM {
			return
		}
		seen[key] = struct{}{}
		edges = append(edges, edge{From: from, To: to, Meters: meters})
	}

	for _, w := range ways {
		keep := make([]bool, len(w.pts))
		for i, p := range w.pts {
			keep[i] = hits[mergeKey(p.Lat, p.Lng)] > 1
		}
		pts := simplifyPreserving(w.pts, keep, colinearM)
		ids := make([]int, 0, len(pts))
		for _, p := range pts {
			ids = append(ids, lookup(p.Lat, p.Lng))
		}
		for i := 0; i < len(ids)-1; i++ {
			a, b := ids[i], ids[i+1]
			switch w.dir {
			case 1:
				addEdge(a, b)
			case -1:
				addEdge(b, a)
			default:
				addEdge(a, b)
				addEdge(b, a)
			}
		}
	}

	if len(nodes) == 0 || len(edges) == 0 {
		return nil, fmt.Errorf("empty extract")
	}
	return &graph{Nodes: nodes, Edges: edges}, nil
}

func mergeKey(lat, lng float64) [2]int64 {
	return [2]int64{int64(math.Round(lat * mergeGrid)), int64(math.Round(lng * mergeGrid))}
}

func simplifyPreserving(pts []node, keep []bool, eps float64) []node {
	if len(pts) <= 2 {
		return pts
	}
	out := []node{pts[0]}
	start := 0
	for i := 1; i < len(pts); i++ {
		isEnd := i == len(pts)-1
		if !isEnd && !keep[i] {
			continue
		}
		seg := simplify(pts[start:i+1], eps)
		out = append(out, seg[1:]...)
		start = i
	}
	return out
}

func isCarHighway(kind string) bool {
	_, ok := carHighways[kind]
	return ok
}

func wayDir(tags map[string]string) int {
	switch tags["oneway"] {
	case "yes", "true", "1":
		return 1
	case "-1", "reverse":
		return -1
	}
	if tags["junction"] == "roundabout" {
		return 1
	}
	return 2
}

func simplify(pts []node, eps float64) []node {
	if len(pts) <= 2 {
		return pts
	}
	maxDist := 0.0
	idx := 0
	for i := 1; i < len(pts)-1; i++ {
		d := distToSegment(pts[i], pts[0], pts[len(pts)-1])
		if d > maxDist {
			maxDist = d
			idx = i
		}
	}
	if maxDist <= eps {
		return []node{pts[0], pts[len(pts)-1]}
	}
	left := simplify(pts[:idx+1], eps)
	right := simplify(pts[idx:], eps)
	return append(left[:len(left)-1], right...)
}

func distToSegment(p, a, b node) float64 {
	_, _, _, dist := project(p, a, b)
	return dist
}

func project(p, a, b node) (along, plat, plng, dist float64) {
	midLat := (a.Lat + b.Lat) / 2
	cosLat := math.Cos(midLat * math.Pi / 180)
	if cosLat == 0 {
		cosLat = 1
	}
	bx := (b.Lng - a.Lng) * kmPerDeg * cosLat * 1000
	by := (b.Lat - a.Lat) * kmPerDeg * 1000
	px := (p.Lng - a.Lng) * kmPerDeg * cosLat * 1000
	py := (p.Lat - a.Lat) * kmPerDeg * 1000
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
	return t, a.Lat + t*(b.Lat-a.Lat), a.Lng + t*(b.Lng-a.Lng), math.Hypot(px-qx, py-qy)
}

func metersBetween(a, b node) float64 {
	midLat := (a.Lat + b.Lat) / 2
	cosLat := math.Cos(midLat * math.Pi / 180)
	if cosLat == 0 {
		cosLat = 1
	}
	dLat := (b.Lat - a.Lat) * kmPerDeg
	dLng := (b.Lng - a.Lng) * kmPerDeg * cosLat
	return math.Hypot(dLat, dLng) * 1000
}
