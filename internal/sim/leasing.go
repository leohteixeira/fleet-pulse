package sim

import (
	"fmt"

	"github.com/leohteixeira/fleet-pulse/internal/routes"
)

const (
	// LeasingSize is the demo leasing roster (~50 devices).
	LeasingSize = 50
	// PrefixFleet is the rental MQTT topic prefix.
	PrefixFleet = "fleet"
	// PrefixLeasing is the leasing MQTT topic prefix.
	PrefixLeasing  = "leasing"
	leasingVIN     = "FPULSELSG"
	refuseBlockPct = 15
)

// NewLeasingFleet returns about 50 leasing vehicles on library routes.
// VINs use FPULSELSG so they never collide with the rental FPULSESAO sequence.
func NewLeasingFleet() []Vehicle {
	lib := routes.Default()
	ids := lib.RouteIDs()
	src := newRNG(2)
	fleet := make([]Vehicle, 0, LeasingSize)
	for i := range LeasingSize {
		n := i + 1
		routeID := ids[src.IntN(len(ids))]
		r, ok := lib.Lookup(routeID)
		if !ok || len(r.Points) == 0 {
			panic("sim: leasing route missing from library")
		}
		start := r.Points[0]
		vin := fmt.Sprintf("%s%08d", leasingVIN, n)
		v := Vehicle{
			VIN:              vin,
			DisplayID:        fmt.Sprintf("L%02d", n),
			Plate:            leasingPlate(i),
			Model:            models[i%len(models)],
			Lat:              start.Lat,
			Lng:              start.Lng,
			Battery:          30 + src.IntN(60),
			Speed:            18 + (i*3)%40,
			Heading:          (i * 12) % 360,
			Ignition:         true,
			Locked:           false,
			Odometer:         8000 + float64(i)*400,
			Trip:             float64((i%9)+1) * 0.5,
			Prefix:           PrefixLeasing,
			RouteID:          routeID,
			routeIdx:         0,
			rng:              newRNG(uint64(n + 1000)),
			nextIncidentAt:   2 + src.IntN(4),
			nextIncidentKind: incidentKind(1 + src.IntN(3)),
		}
		fleet = append(fleet, v)
	}
	return fleet
}

// LeasingTelemetryTopic is the leasing device publish topic for vin.
func LeasingTelemetryTopic(vin string) string {
	return PrefixLeasing + "/" + vin + "/telemetry"
}

// LeasingCommandTopic is the leasing device subscribe topic for commands.
func LeasingCommandTopic(vin string) string {
	return PrefixLeasing + "/" + vin + "/commands"
}

// LeasingAckTopic is the leasing device publish topic for acknowledgements.
func LeasingAckTopic(vin string) string {
	return PrefixLeasing + "/" + vin + "/ack"
}

// RefuseBlock reports whether a leasing device refuses a future block command.
// About 15% of rolls are true under a fixed SIM_SEED. Story 4 will call it.
func (v *Vehicle) RefuseBlock() bool {
	if v.refuse != nil {
		return v.refuse()
	}
	if v.rng != nil {
		return v.rng.IntN(100) < refuseBlockPct
	}
	return newRNG(99).IntN(100) < refuseBlockPct
}

func leasingPlate(i int) string {
	letters := []byte("BCDFGH")
	return fmt.Sprintf("L%cS%dB%02d", letters[i%len(letters)], i%10, i+1)
}

func (v Vehicle) topicPrefix() string {
	if v.Prefix == "" {
		return PrefixFleet
	}
	return v.Prefix
}

func (v Vehicle) telemetryTopic() string {
	return v.topicPrefix() + "/" + v.VIN + "/telemetry"
}

func (v Vehicle) commandTopic() string {
	return v.topicPrefix() + "/" + v.VIN + "/commands"
}

func (v Vehicle) ackTopic() string {
	return v.topicPrefix() + "/" + v.VIN + "/ack"
}
