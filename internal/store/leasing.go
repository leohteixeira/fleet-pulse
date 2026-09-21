package store

import (
	"context"
	"fmt"
)

// ListLeasing returns the financed roster with last-known telem when present.
// Vehicles without vehicle_state still appear with zero last-known fields.
func (p *Postgres) ListLeasing(ctx context.Context) ([]Vehicle, error) {
	rows, err := p.q.ListLeasingVehicles(ctx)
	if err != nil {
		return nil, fmt.Errorf("list leasing vehicles: %w", err)
	}
	out := make([]Vehicle, 0, len(rows))
	for _, r := range rows {
		out = append(out, Vehicle{
			VIN:       r.Vin,
			DisplayID: r.DisplayID,
			Lat:       r.Lat,
			Lng:       r.Lng,
			Plate:     r.Plate,
			Model:     r.Model,
			Battery:   int(r.Battery),
			Speed:     int(r.Speed),
			Heading:   int(r.Heading),
			Ignition:  r.Ignition,
			Locked:    r.Locked,
			Odometer:  r.Odometer,
			Trip:      r.Trip,
		})
	}
	return out, nil
}
