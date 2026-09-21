package book

import (
	"math/rand/v2"
	"slices"
	"time"

	"github.com/leohteixeira/fleet-pulse/internal/sim"
	"github.com/leohteixeira/fleet-pulse/internal/store"
)

var clientNames = []string{
	"Ana Costa", "Bruno Lima", "Carla Souza", "Diego Alves", "Elisa Martins",
	"Felipe Rocha", "Giulia Nunes", "Henrique Dias", "Isabela Freitas", "Joao Mendes",
	"Karina Lopes", "Lucas Barbosa", "Marina Teixeira", "Nicolas Araujo", "Olivia Castro",
	"Paulo Ribeiro", "Quezia Fernandes", "Rafael Pinto", "Sofia Cardoso", "Tiago Moreira",
	"Ursula Prado", "Vitor Campos", "Wendy Azevedo", "Xavier Duarte", "Yasmin Correia",
	"Zeca Farias", "Alice Monteiro", "Breno Pires", "Cecilia Ramos", "Davi Siqueira",
	"Eduarda Melo", "Fabio Cunha", "Gabriela Reis", "Hugo Batista", "Ingrid Moraes",
	"Jorge Tavares", "Livia Andrade", "Murilo Vieira", "Nina Carvalho", "Otavio Borges",
	"Patricia Gomes", "Renato Silveira", "Sabrina Moura", "Tales Fonseca", "Una Peixoto",
	"Valentina Cruz", "Wagner Matos", "Yago Santana", "Zuleica Barros", "Amanda Figueiredo",
	"Caio Holanda", "Debora Leal", "Enzo Guimaraes", "Fernanda Brito", "Gustavo Pacheco",
	"Helena Queiroz", "Igor Vasconcelos", "Julia Sampaio", "Kaique Nogueira", "Larissa Furtado",
}

func planBook(origin time.Time, fleet []sim.Vehicle, seed uint64) ([]store.LeasingVehicle, []store.SeedContract) {
	rng := rand.New(rand.NewPCG(seed, 7))
	vehicles := leasingVehicles(fleet)
	n := bookSize
	if len(vehicles) < n {
		n = len(vehicles)
	}
	profiles := assignProfiles(n, rng)
	names := assignNames(n, rng)
	contracts := make([]store.SeedContract, 0, n)
	for i := range n {
		contracts = append(contracts, store.SeedContract{
			VIN:          vehicles[i].VIN,
			CustomerName: names[i],
			Profile:      profiles[i],
			Amount:       defaultInstallment,
		})
	}
	placeBands(contracts, origin, rng)
	return vehicles, contracts
}

func leasingVehicles(fleet []sim.Vehicle) []store.LeasingVehicle {
	out := make([]store.LeasingVehicle, 0, len(fleet))
	for _, v := range fleet {
		out = append(out, store.LeasingVehicle{
			VIN:       v.VIN,
			DisplayID: v.DisplayID,
			Plate:     v.Plate,
			Model:     v.Model,
		})
	}
	return out
}

func assignProfiles(n int, rng *rand.Rand) []string {
	pontual := n * 40 / 100
	atrasa := n * 30 / 100
	regulariza := n * 20 / 100
	inadimplente := n - pontual - atrasa - regulariza
	out := make([]string, 0, n)
	out = appendCount(out, ProfilePontual, pontual)
	out = appendCount(out, ProfileAtrasa, atrasa)
	out = appendCount(out, ProfileRegulariza, regulariza)
	out = appendCount(out, ProfileInadimplente, inadimplente)
	rng.Shuffle(len(out), func(i, j int) {
		out[i], out[j] = out[j], out[i]
	})
	return out
}

func appendCount(dst []string, profile string, n int) []string {
	for range n {
		dst = append(dst, profile)
	}
	return dst
}

func assignNames(n int, rng *rand.Rand) []string {
	names := slices.Clone(clientNames)
	rng.Shuffle(len(names), func(i, j int) {
		names[i], names[j] = names[j], names[i]
	})
	if n > len(names) {
		n = len(names)
	}
	return names[:n]
}

func placeBands(contracts []store.SeedContract, origin time.Time, rng *rand.Rand) {
	var late []int
	var current []int
	for i, c := range contracts {
		switch c.Profile {
		case ProfileRegulariza, ProfileInadimplente:
			late = append(late, i)
		default:
			current = append(current, i)
		}
	}
	need := floorNeed(len(contracts))
	bands := []string{Band115, Band1630, BandAcima30}
	var lateIdx int
	for _, band := range bands {
		for range need {
			if lateIdx < len(late) {
				overlayBand(&contracts[late[lateIdx]], band, origin, rng)
				lateIdx++
				continue
			}
			if len(current) == 0 {
				break
			}
			last := current[len(current)-1]
			current = current[:len(current)-1]
			overlayBand(&contracts[last], band, origin, rng)
		}
	}
	for lateIdx < len(late) {
		overlayBand(&contracts[late[lateIdx]], BandAcima30, origin, rng)
		lateIdx++
	}
	for _, i := range current {
		overlayBand(&contracts[i], BandEmDia, origin, rng)
	}
}

func overlayBand(c *store.SeedContract, band string, origin time.Time, rng *rand.Rand) {
	firstDue := firstDueFor(band, origin, rng)
	c.StartedOn = firstDue
	c.Installments = monthly(firstDue, c.Amount, installmentCount)
	switch band {
	case BandEmDia:
		for i := range c.Installments {
			if !c.Installments[i].DueOn.After(origin) && c.Profile == ProfilePontual {
				c.Installments[i].Paid = true
			}
		}
	default:
		if len(c.Installments) > 0 {
			c.Installments[0].Paid = false
		}
	}
}

func firstDueFor(band string, origin time.Time, rng *rand.Rand) time.Time {
	switch band {
	case Band115:
		return origin.AddDate(0, 0, -(1 + rng.IntN(15)))
	case Band1630:
		return origin.AddDate(0, 0, -(16 + rng.IntN(15)))
	case BandAcima30:
		return origin.AddDate(0, 0, -(31 + rng.IntN(40)))
	default:
		return origin.AddDate(0, 0, 1+rng.IntN(20))
	}
}

func monthly(first time.Time, amount string, n int) []store.SeedInstallment {
	out := make([]store.SeedInstallment, 0, n)
	start := dateOnly(first)
	for i := range n {
		out = append(out, store.SeedInstallment{
			DueOn:  start.AddDate(0, i, 0),
			Amount: amount,
		})
	}
	return out
}

func newContractInBand(vin, name, profile, band string, origin time.Time, rng *rand.Rand) store.SeedContract {
	c := store.SeedContract{
		VIN:          vin,
		CustomerName: name,
		Profile:      profile,
		Amount:       defaultInstallment,
	}
	overlayBand(&c, band, origin, rng)
	return c
}

func profileForBand(band string) string {
	switch band {
	case BandEmDia:
		return ProfilePontual
	case Band115:
		return ProfileRegulariza
	default:
		return ProfileInadimplente
	}
}
