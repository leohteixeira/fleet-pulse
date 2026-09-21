-- name: InsertCommand :exec
INSERT INTO commands (id, vin, fleet, action, state, correlation_id, sent_at)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: UpdateCommandState :exec
UPDATE commands
SET state = $2,
    sent_at = COALESCE(sqlc.narg('sent_at')::timestamptz, sent_at),
    updated_at = now()
WHERE id = $1;

-- name: GetCommand :one
SELECT id, vin, fleet, action, state, correlation_id, created_at, sent_at, updated_at
FROM commands
WHERE id = $1;

-- name: ListActiveLeasingCommands :many
SELECT id, vin, fleet, action, state, correlation_id, created_at, sent_at, updated_at
FROM commands
WHERE fleet = 'leasing'
  AND state IN ('REQUESTED', 'ARMED', 'SENT')
ORDER BY created_at;

-- name: InsertAuditLog :exec
INSERT INTO audit_log (action, payload)
VALUES ($1, $2);

-- name: InsertOutbox :exec
INSERT INTO outbox (topic, payload)
VALUES ($1, $2);

-- name: ListUnsentOutbox :many
SELECT id, topic, payload, created_at
FROM outbox
WHERE sent_at IS NULL
ORDER BY id
LIMIT $1;

-- name: MarkOutboxSent :exec
UPDATE outbox
SET sent_at = $2
WHERE id = $1;

-- name: HasLeasingVehicle :one
SELECT EXISTS (
    SELECT 1
    FROM vehicles
    WHERE vin = $1 AND fleet = 'leasing'
);

-- name: ListLatestAckedLeasing :many
SELECT DISTINCT ON (vin)
    id, vin, fleet, action, state, correlation_id, created_at, sent_at, updated_at
FROM commands
WHERE fleet = 'leasing' AND state = 'ACKED'
ORDER BY vin, updated_at DESC;
