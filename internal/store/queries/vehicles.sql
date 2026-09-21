-- name: InsertRentalVehicle :exec
INSERT INTO vehicles (vin, fleet, display_id, plate, model)
VALUES ($1, 'rental', $2, $3, $4)
ON CONFLICT (vin) DO NOTHING;

-- name: GetRentalVehicle :one
SELECT vin, fleet, display_id, plate, model
FROM vehicles
WHERE vin = $1 AND fleet = 'rental';

-- name: ListRentalVehicles :many
SELECT vin, fleet, display_id, plate, model
FROM vehicles
WHERE fleet = 'rental'
ORDER BY vin;

-- name: UpsertVehicleState :exec
INSERT INTO vehicle_state (
    vin,
    lat,
    lng,
    battery,
    speed,
    heading,
    ignition,
    locked,
    odometer,
    trip,
    updated_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, now()
)
ON CONFLICT (vin) DO UPDATE SET
    lat = EXCLUDED.lat,
    lng = EXCLUDED.lng,
    battery = EXCLUDED.battery,
    speed = EXCLUDED.speed,
    heading = EXCLUDED.heading,
    ignition = EXCLUDED.ignition,
    locked = EXCLUDED.locked,
    odometer = EXCLUDED.odometer,
    trip = EXCLUDED.trip,
    updated_at = now();

-- name: GetVehicleState :one
SELECT vin, lat, lng, battery, speed, heading, ignition, locked, odometer, trip
FROM vehicle_state
WHERE vin = $1;

-- name: ListVehicleState :many
SELECT vin, lat, lng, battery, speed, heading, ignition, locked, odometer, trip
FROM vehicle_state
ORDER BY vin;
