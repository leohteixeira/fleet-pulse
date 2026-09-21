package routes_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/leohteixeira/fleet-pulse/internal/routes"
)

func TestLoad_SeedShape(t *testing.T) {
	t.Parallel()

	lib, err := routes.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if n := len(lib.POIs); n < 55 || n > 70 {
		t.Fatalf("poi count = %d, want 55–70", n)
	}
	if n := len(lib.Routes); n < 280 || n > 320 {
		t.Fatalf("route count = %d, want 280–320", n)
	}
	if lib.Version == "" {
		t.Fatal("version is empty")
	}

	ids := lib.RouteIDs()
	if len(ids) != len(lib.Routes) {
		t.Fatalf("RouteIDs() = %d, want %d", len(ids), len(lib.Routes))
	}
	for _, r := range lib.Routes {
		if len(r.Points) < 2 {
			t.Fatalf("route %q has %d points, want at least 2", r.ID, len(r.Points))
		}
		for i, p := range r.Points {
			if !routes.InBounds(p.Lat, p.Lng) {
				t.Fatalf("route %q point %d (%v,%v) outside greater sp", r.ID, i, p.Lat, p.Lng)
			}
		}
		got, ok := lib.Lookup(r.ID)
		if !ok || got.ID != r.ID || len(got.Points) != len(r.Points) {
			t.Fatalf("Lookup(%q) = %+v ok=%v", r.ID, got, ok)
		}
	}
	for _, p := range lib.POIs {
		if !routes.InBounds(p.Lat, p.Lng) {
			t.Fatalf("poi %q (%v,%v) outside greater sp", p.ID, p.Lat, p.Lng)
		}
	}
}

func TestParse_CorruptSeed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  []byte
		want string
	}{
		{name: "empty", raw: []byte("  "), want: "empty seed"},
		{name: "not json", raw: []byte(`not-json`), want: "decode seed"},
		{name: "too few pois", raw: []byte(`{"version":"1","pois":[],"routes":[]}`), want: "poi count"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := routes.Parse(tt.raw)
			if err == nil {
				t.Fatal("Parse() error = nil, want wrapped failure")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Parse() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestInBounds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		lat, lng float64
		want     bool
	}{
		{name: "centro", lat: -23.55, lng: -46.63, want: true},
		{name: "south west corner", lat: -23.63, lng: -46.87, want: true},
		{name: "north east corner", lat: -23.49, lng: -46.41, want: true},
		{name: "south of viewport", lat: -23.64, lng: -46.63, want: false},
		{name: "east of viewport", lat: -23.55, lng: -46.40, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := routes.InBounds(tt.lat, tt.lng); got != tt.want {
				t.Fatalf("InBounds(%v,%v) = %v, want %v", tt.lat, tt.lng, got, tt.want)
			}
		})
	}
}

func TestRuntimeDoesNotImportSeedroutes(t *testing.T) {
	t.Parallel()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	roots := []string{
		filepath.Join(root, "cmd", "server"),
		filepath.Join(root, "internal", "sim"),
	}
	for _, dir := range roots {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("readdir %s: %v", dir, err)
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
				continue
			}
			body, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				t.Fatalf("read %s: %v", e.Name(), err)
			}
			if strings.Contains(string(body), "scripts/seedroutes") {
				t.Fatalf("%s imports scripts/seedroutes", filepath.Join(dir, e.Name()))
			}
		}
	}
}
