-- name: InsertRentalVehicle :exec
INSERT INTO vehicles (vin, fleet, display_id, plate, model)
VALUES ($1, 'rental', $2, $3, $4)
ON CONFLICT (vin) DO NOTHING;

-- name: InsertLeasingVehicle :exec
INSERT INTO vehicles (vin, fleet, display_id, plate, model)
VALUES ($1, 'leasing', $2, $3, $4)
ON CONFLICT (vin) DO NOTHING;

-- name: CountLeasingVehicles :one
SELECT count(*)::bigint
FROM vehicles
WHERE fleet = 'leasing';

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

-- name: ListLeasingVehicles :many
SELECT
    v.vin,
    v.display_id,
    v.plate,
    v.model,
    COALESCE(vs.lat, 0)::float8 AS lat,
    COALESCE(vs.lng, 0)::float8 AS lng,
    COALESCE(vs.battery, 0)::int AS battery,
    COALESCE(vs.speed, 0)::int AS speed,
    COALESCE(vs.heading, 0)::int AS heading,
    COALESCE(vs.ignition, false)::bool AS ignition,
    COALESCE(vs.locked, false)::bool AS locked,
    COALESCE(vs.odometer, 0)::float8 AS odometer,
    COALESCE(vs.trip, 0)::float8 AS trip
FROM vehicles v
LEFT JOIN vehicle_state vs ON vs.vin = v.vin
WHERE v.fleet = 'leasing'
ORDER BY v.vin;
