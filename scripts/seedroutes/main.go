// Command seedroutes generates the committed route library from OSRM road geometry.
// Run this maintenance command explicitly; the server and tests never call OSRM.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/leohteixeira/fleet-pulse/internal/routes"
)

type poi = routes.POI

type library struct {
	Version string         `json:"version"`
	Source  string         `json:"source"`
	POIs    []routes.POI   `json:"pois"`
	Routes  []routes.Route `json:"routes"`
}

type routerResponse struct {
	Code      string `json:"code"`
	Waypoints []struct {
		Location [2]float64 `json:"location"`
	} `json:"waypoints"`
	Routes []struct {
		Legs []struct {
			Steps []struct {
				Geometry struct {
					Coordinates [][2]float64 `json:"coordinates"`
				} `json:"geometry"`
			} `json:"steps"`
		} `json:"legs"`
	} `json:"routes"`
}

func main() {
	out := flag.String("out", "internal/routes/seed.json", "output path")
	endpoint := flag.String("router", "https://router.project-osrm.org", "OSRM base URL used only for seed generation")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, *out, *endpoint); err != nil {
		fmt.Fprintf(os.Stderr, "seedroutes: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, out, endpoint string) error {
	lib, err := build(ctx, endpoint, catalog())
	if err != nil {
		return err
	}
	body, err := json.Marshal(lib)
	if err != nil {
		return fmt.Errorf("encode seed: %w", err)
	}
	// Validate the entire artifact before replacing the working embedded seed.
	if _, err := routes.Parse(body); err != nil {
		return fmt.Errorf("validate generated seed: %w", err)
	}
	if err := os.WriteFile(out, append(body, '\n'), 0o644); err != nil {
		return fmt.Errorf("write seed: %w", err)
	}
	fmt.Printf("wrote %s (%d pois, %d road routes, %d bytes)\n", out, len(lib.POIs), len(lib.Routes), len(body)+1)
	return nil
}

func build(ctx context.Context, endpoint string, pois []routes.POI) (*library, error) {
	const want = 300
	lib := &library{Version: "2", Source: "osrm/openstreetmap", POIs: pois, Routes: make([]routes.Route, 0, want)}
	// Each tour visits every POI. Coprime strides give different destinations,
	// while retaining outgoing routes across the whole catalog.
	for _, stride := range []int{1, 3, 5, 7, 9, 11, 13, 15, 17, 19} {
		tour := make([]routes.POI, 0, len(pois)+1)
		for i := 0; i <= len(pois); i++ {
			tour = append(tour, pois[(i*stride)%len(pois)])
		}
		response, err := fetchTour(ctx, endpoint, tour)
		if err != nil {
			return nil, err
		}
		if stride == 1 {
			for i := range pois {
				steps := response.Routes[0].Legs[i].Steps
				if len(steps) == 0 || len(steps[0].Geometry.Coordinates) == 0 {
					return nil, fmt.Errorf("router returned empty road geometry for poi %q", pois[i].ID)
				}
				location := steps[0].Geometry.Coordinates[0]
				pois[i].Lng, pois[i].Lat = location[0], location[1]
			}
		}
		for i, leg := range response.Routes[0].Legs {
			from, to := tour[i].ID, tour[i+1].ID
			points := make([]routes.Point, 0)
			inBounds := true
			for _, step := range leg.Steps {
				for _, coord := range step.Geometry.Coordinates {
					p := routes.Point{Lng: coord[0], Lat: coord[1]}
					if !routes.InBounds(p.Lat, p.Lng) {
						inBounds = false
					}
					if len(points) == 0 || points[len(points)-1] != p {
						points = append(points, p)
					}
				}
			}
			// Reject a route outside the viewport. Clamping or simplifying vertices
			// would cut across blocks and recreate the off-road regression.
			if !inBounds || len(points) < 2 {
				continue
			}
			first, last := points[0], points[len(points)-1]
			a, b := pois[(i*stride)%len(pois)], pois[((i+1)*stride)%len(pois)]
			if first != (routes.Point{Lat: a.Lat, Lng: a.Lng}) || last != (routes.Point{Lat: b.Lat, Lng: b.Lng}) {
				continue
			}
			lib.Routes = append(lib.Routes, routes.Route{
				ID: fmt.Sprintf("r%04d", len(lib.Routes)+1), From: from, To: to, Points: points,
			})
			if len(lib.Routes) == want {
				return lib, nil
			}
		}
	}
	return nil, fmt.Errorf("only %d connected in-bounds road routes, want %d", len(lib.Routes), want)
}

func fetchTour(ctx context.Context, endpoint string, tour []routes.POI) (*routerResponse, error) {
	coords := make([]string, 0, len(tour))
	for _, p := range tour {
		coords = append(coords, fmt.Sprintf("%.6f,%.6f", p.Lng, p.Lat))
	}
	url := strings.TrimRight(endpoint, "/") + "/route/v1/driving/" + strings.Join(coords, ";") +
		"?overview=false&geometries=geojson&steps=true&continue_straight=false"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build router request: %w", err)
	}
	req.Header.Set("User-Agent", "fleet-pulse-seedroutes/2.0")
	client := &http.Client{Timeout: 45 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch road routes: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("router status %d", res.StatusCode)
	}
	var response routerResponse
	if err := json.NewDecoder(io.LimitReader(res.Body, 32<<20)).Decode(&response); err != nil {
		return nil, fmt.Errorf("decode router response: %w", err)
	}
	valid := response.Code == "Ok" && len(response.Routes) == 1
	if !valid || len(response.Waypoints) != len(tour) {
		return nil, fmt.Errorf("invalid router response: %q", response.Code)
	}
	if len(response.Routes[0].Legs) != len(tour)-1 {
		return nil, fmt.Errorf("router returned incomplete tour")
	}
	return &response, nil
}

func catalog() []poi {
	return []poi{
		{ID: "se", Name: "Praça da Sé", Lat: -23.5505, Lng: -46.6333},
		{ID: "ibirapuera", Name: "Parque Ibirapuera", Lat: -23.5874, Lng: -46.6576},
		{ID: "masp", Name: "MASP / Paulista", Lat: -23.5614, Lng: -46.6559},
		{ID: "luz", Name: "Estação da Luz", Lat: -23.5349, Lng: -46.6345},
		{ID: "pinheiros", Name: "Pinheiros", Lat: -23.5667, Lng: -46.7019},
		{ID: "madalena", Name: "Vila Madalena", Lat: -23.5464, Lng: -46.6907},
		{ID: "lapa", Name: "Lapa", Lat: -23.5213, Lng: -46.7028},
		{ID: "barrafunda", Name: "Barra Funda", Lat: -23.5255, Lng: -46.6674},
		{ID: "republica", Name: "República", Lat: -23.5436, Lng: -46.6426},
		{ID: "consolacao", Name: "Consolação", Lat: -23.5576, Lng: -46.6606},
		{ID: "jardins", Name: "Jardins", Lat: -23.5670, Lng: -46.6700},
		{ID: "itaim", Name: "Itaim Bibi", Lat: -23.5840, Lng: -46.6750},
		{ID: "olimpia", Name: "Vila Olímpia", Lat: -23.5950, Lng: -46.6870},
		{ID: "brooklin", Name: "Brooklin", Lat: -23.6100, Lng: -46.6960},
		{ID: "moema", Name: "Moema", Lat: -23.6018, Lng: -46.6660},
		{ID: "saude", Name: "Saúde", Lat: -23.6180, Lng: -46.6370},
		{ID: "mariana", Name: "Vila Mariana", Lat: -23.5890, Lng: -46.6345},
		{ID: "liberdade", Name: "Liberdade", Lat: -23.5605, Lng: -46.6320},
		{ID: "aclimacao", Name: "Aclimação", Lat: -23.5715, Lng: -46.6280},
		{ID: "cambuci", Name: "Cambuci", Lat: -23.5660, Lng: -46.6150},
		{ID: "belavista", Name: "Bela Vista", Lat: -23.5560, Lng: -46.6460},
		{ID: "higienopolis", Name: "Higienópolis", Lat: -23.5450, Lng: -46.6580},
		{ID: "pacaembu", Name: "Pacaembu", Lat: -23.5480, Lng: -46.6650},
		{ID: "perdizes", Name: "Perdizes", Lat: -23.5360, Lng: -46.6760},
		{ID: "pompeia", Name: "Pompeia", Lat: -23.5280, Lng: -46.6820},
		{ID: "aguabranca", Name: "Água Branca", Lat: -23.5300, Lng: -46.6900},
		{ID: "leopoldina", Name: "Vila Leopoldina", Lat: -23.5280, Lng: -46.7300},
		{ID: "jaguare", Name: "Jaguaré", Lat: -23.5450, Lng: -46.7480},
		{ID: "butanta", Name: "Butantã", Lat: -23.5690, Lng: -46.7320},
		{ID: "usp", Name: "Cidade Universitária", Lat: -23.5590, Lng: -46.7210},
		{ID: "farialima", Name: "Faria Lima", Lat: -23.5670, Lng: -46.6930},
		{ID: "berrini", Name: "Berrini", Lat: -23.6100, Lng: -46.6970},
		{ID: "morumbi", Name: "Morumbi Shopping", Lat: -23.6220, Lng: -46.6990},
		{ID: "campobelo", Name: "Campo Belo", Lat: -23.6180, Lng: -46.6680},
		{ID: "jabaquara", Name: "Jabaquara", Lat: -23.6260, Lng: -46.6400},
		{ID: "sacoma", Name: "Sacomã", Lat: -23.6280, Lng: -46.6180},
		{ID: "ipiranga", Name: "Museu do Ipiranga", Lat: -23.5850, Lng: -46.6100},
		{ID: "tatuape", Name: "Tatuapé", Lat: -23.5400, Lng: -46.5760},
		{ID: "belem", Name: "Belém", Lat: -23.5400, Lng: -46.5900},
		{ID: "bras", Name: "Brás", Lat: -23.5450, Lng: -46.6160},
		{ID: "bomretiro", Name: "Bom Retiro", Lat: -23.5280, Lng: -46.6380},
		{ID: "cecilia", Name: "Santa Cecília", Lat: -23.5380, Lng: -46.6500},
		{ID: "eliseos", Name: "Campos Elíseos", Lat: -23.5350, Lng: -46.6450},
		{ID: "casaverde", Name: "Casa Verde", Lat: -23.5080, Lng: -46.6560},
		{ID: "santana", Name: "Santana", Lat: -23.5050, Lng: -46.6250},
		{ID: "guilherme", Name: "Vila Guilherme", Lat: -23.5180, Lng: -46.6000},
		{ID: "carandiru", Name: "Carandiru", Lat: -23.5090, Lng: -46.6230},
		{ID: "limao", Name: "Limão", Lat: -23.5100, Lng: -46.6700},
		{ID: "freguesia", Name: "Freguesia do Ó", Lat: -23.5030, Lng: -46.7000},
		{ID: "lapastation", Name: "Estação Lapa", Lat: -23.5200, Lng: -46.7040},
		{ID: "osasco", Name: "Osasco Centro", Lat: -23.5320, Lng: -46.7910},
		{ID: "vilayara", Name: "Vila Yara", Lat: -23.5280, Lng: -46.7750},
		{ID: "continental", Name: "Continental", Lat: -23.5480, Lng: -46.8200},
		{ID: "ceasa", Name: "CEASA", Lat: -23.5250, Lng: -46.7450},
		{ID: "jockey", Name: "Jockey Club", Lat: -23.5800, Lng: -46.7000},
		{ID: "cidadejardim", Name: "Cidade Jardim", Lat: -23.5900, Lng: -46.6850},
		{ID: "conceicao", Name: "Vila Nova Conceição", Lat: -23.5900, Lng: -46.6700},
		{ID: "paraiso", Name: "Paraíso", Lat: -23.5750, Lng: -46.6410},
		{ID: "anarosa", Name: "Ana Rosa", Lat: -23.5810, Lng: -46.6380},
		{ID: "clementino", Name: "Vila Clementino", Lat: -23.5980, Lng: -46.6450},
		{ID: "trianon", Name: "Trianon-Masp", Lat: -23.5625, Lng: -46.6540},
		{ID: "augusta", Name: "Augusta", Lat: -23.5530, Lng: -46.6510},
	}
}
