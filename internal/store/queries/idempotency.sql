-- name: GetIdempotency :one
SELECT contract_id, action, key, resource_id, status_code, body
FROM idempotency_keys
WHERE contract_id = $1 AND action = $2 AND key = $3;

-- name: InsertIdempotency :exec
INSERT INTO idempotency_keys (contract_id, action, key, resource_id, status_code, body)
VALUES ($1, $2, $3, $4, $5, $6);
