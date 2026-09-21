// Command seedroutes builds the committed Greater São Paulo route library.
//
// Run it offline only:
//
//	go run ./scripts/seedroutes -out internal/routes/seed.json
//
// The running server and tests never import this package and never call an
// external router. Production loads the committed seed via go:embed.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
)

const (
	south = -23.63
	north = -23.49
	west  = -46.87
	east  = -46.41
)

type point struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

type poi struct {
	ID   string  `json:"id"`
	Name string  `json:"name"`
	Lat  float64 `json:"lat"`
	Lng  float64 `json:"lng"`
}

type route struct {
	ID     string  `json:"id"`
	From   string  `json:"from"`
	To     string  `json:"to"`
	Points []point `json:"points"`
}

type library struct {
	Version string  `json:"version"`
	POIs    []poi   `json:"pois"`
	Routes  []route `json:"routes"`
}

func main() {
	out := flag.String("out", "internal/routes/seed.json", "output path")
	flag.Parse()
	if err := run(*out); err != nil {
		fmt.Fprintf(os.Stderr, "seedroutes: %v\n", err)
		os.Exit(1)
	}
}

func run(out string) error {
	lib := build()
	body, err := json.Marshal(lib)
	if err != nil {
		return fmt.Errorf("encode seed: %w", err)
	}
	if err := os.WriteFile(out, append(body, '\n'), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", out, err)
	}
	fmt.Printf("wrote %s (%d pois, %d routes, %d bytes)\n", out, len(lib.POIs), len(lib.Routes), len(body)+1)
	return nil
}

func build() library {
	pois := catalog()

	const want = 300
	routes := make([]route, 0, want)
	n := len(pois)
	i := 0
	for a := 0; a < n && len(routes) < want; a++ {
		for step := 1; step < n && len(routes) < want; step++ {
			b := (a + step) % n
			pts := walk(pois[a], pois[b])
			if i%3 == 0 {
				via := pois[(a+b+7)%n]
				if via.ID != pois[a].ID && via.ID != pois[b].ID {
					pts = append(walk(pois[a], via), walk(via, pois[b])[1:]...)
				}
			}
			i++
			pts = clampPoints(dedupe(pts))
			if len(pts) < 2 {
				continue
			}
			routes = append(routes, route{
				ID:     fmt.Sprintf("r%04d", len(routes)+1),
				From:   pois[a].ID,
				To:     pois[b].ID,
				Points: pts,
			})
		}
	}

	return library{
		Version: "1",
		POIs:    pois,
		Routes:  routes,
	}
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

func walk(from, to poi) []point {
	// Manhattan walk on the local viewport grid: east-west, then north-south.
	steps := 4
	out := make([]point, 0, steps+2)
	out = append(out, point{Lat: from.Lat, Lng: from.Lng})
	for i := 1; i <= steps; i++ {
		t := float64(i) / float64(steps+1)
		lat, lng := from.Lat, from.Lng
		if i <= steps/2 {
			lng = from.Lng + t*2*(to.Lng-from.Lng)
			if i == steps/2 {
				lng = to.Lng
			}
		} else {
			lng = to.Lng
			u := float64(i-steps/2) / float64(steps-steps/2)
			lat = from.Lat + u*(to.Lat-from.Lat)
		}
		out = append(out, point{Lat: lat, Lng: lng})
	}
	out = append(out, point{Lat: to.Lat, Lng: to.Lng})
	return out
}

func dedupe(pts []point) []point {
	if len(pts) == 0 {
		return []point{}
	}
	out := []point{pts[0]}
	for _, p := range pts[1:] {
		last := out[len(out)-1]
		if p.Lat == last.Lat && p.Lng == last.Lng {
			continue
		}
		out = append(out, p)
	}
	return out
}

func clampPoints(pts []point) []point {
	out := make([]point, 0, len(pts))
	for _, p := range pts {
		p.Lat = clamp(p.Lat, south, north)
		p.Lng = clamp(p.Lng, west, east)
		out = append(out, p)
	}
	return out
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
