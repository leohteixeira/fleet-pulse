package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/leohteixeira/fleet-pulse/internal/routes"
)

func TestBuild_PreservesRouterGeometry(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("geometries") != "geojson" || q.Get("steps") != "true" || q.Get("continue_straight") != "false" {
			t.Errorf("router options = %v, want complete step geometry and connected tour legs", q)
		}
		raw := strings.TrimPrefix(r.URL.Path, "/route/v1/driving/")
		coordinates := make([][2]float64, 0)
		for _, pair := range strings.Split(raw, ";") {
			parts := strings.Split(pair, ",")
			lng, err := strconv.ParseFloat(parts[0], 64)
			if err != nil {
				t.Error(err)
				return
			}
			lat, err := strconv.ParseFloat(parts[1], 64)
			if err != nil {
				t.Error(err)
				return
			}
			coordinates = append(coordinates, [2]float64{lng, lat})
		}
		legs := make([]any, 0, len(coordinates)-1)
		waypoints := make([]any, 0, len(coordinates))
		for i, coord := range coordinates {
			waypoints = append(waypoints, map[string]any{"location": coord})
			if i == len(coordinates)-1 {
				break
			}
			next := coordinates[i+1]
			turn := [2]float64{(coord[0] + next[0]) / 2, coord[1]}
			legs = append(legs, map[string]any{"steps": []any{
				map[string]any{"geometry": map[string]any{"coordinates": [][2]float64{coord, turn}}},
				map[string]any{"geometry": map[string]any{"coordinates": [][2]float64{turn, next}}},
			}})
		}
		if err := json.NewEncoder(w).Encode(map[string]any{"code": "Ok", "waypoints": waypoints, "routes": []any{map[string]any{"legs": legs}}}); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	lib, err := build(t.Context(), server.URL, catalog())
	if err != nil {
		t.Fatal(err)
	}
	if len(lib.Routes) != 300 {
		t.Fatalf("routes=%d, want 300", len(lib.Routes))
	}
	from, to := catalog()[0], catalog()[1]
	want := []routes.Point{
		{Lat: from.Lat, Lng: from.Lng},
		{Lat: from.Lat, Lng: (from.Lng + to.Lng) / 2},
		{Lat: to.Lat, Lng: to.Lng},
	}
	if !reflect.DeepEqual(lib.Routes[0].Points, want) {
		t.Fatalf("points=%v, want router vertices %v", lib.Routes[0].Points, want)
	}
	body, err := json.Marshal(lib)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := routes.Parse(body); err != nil {
		t.Fatal(err)
	}
}

func TestRun_RouterFailurePreservesSeed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, body string
		status     int
	}{
		{name: "service unavailable", status: http.StatusServiceUnavailable, body: `{}`},
		{name: "no road route", status: http.StatusOK, body: `{"code":"NoRoute"}`},
		{name: "incomplete geometry", status: http.StatusOK, body: `{"code":"Ok","routes":[{"legs":[]}]}`},
		{name: "invalid json", status: http.StatusOK, body: `invalid`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				if _, err := w.Write([]byte(tt.body)); err != nil {
					t.Error(err)
				}
			}))
			defer server.Close()
			out := filepath.Join(t.TempDir(), "seed.json")
			const original = "existing road seed"
			if err := os.WriteFile(out, []byte(original), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := run(t.Context(), out, server.URL); err == nil {
				t.Fatal("expected router failure, got synthetic fallback")
			}
			got, err := os.ReadFile(out)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != original {
				t.Fatalf("seed replaced on failure: %q", got)
			}
		})
	}
}
