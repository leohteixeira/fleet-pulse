-- name: DeleteFinishedCommandsOlderThan :execrows
DELETE FROM commands
WHERE updated_at < $1
  AND state IN ('ACKED', 'FAILED', 'TIMEOUT', 'CANCELLED');

-- name: DeleteAuditOlderThan :execrows
DELETE FROM audit_log
WHERE created_at < $1;

-- name: ListEndedContractsOlderThan :many
SELECT c.id
FROM contracts c
WHERE c.ended_on IS NOT NULL
  AND c.created_at < $1
  AND NOT EXISTS (
    SELECT 1
    FROM audit_log a
    WHERE a.contract_id = c.id
  )
LIMIT $2;

-- name: DeleteIdempotencyByContracts :exec
DELETE FROM idempotency_keys
WHERE contract_id = ANY(sqlc.arg('contract_ids')::uuid[]);

-- name: DeletePaymentsByContracts :exec
DELETE FROM payments
WHERE contract_id = ANY(sqlc.arg('contract_ids')::uuid[]);

-- name: DeleteInstallmentsByContracts :exec
DELETE FROM installments
WHERE contract_id = ANY(sqlc.arg('contract_ids')::uuid[]);

-- name: DeleteContracts :execrows
DELETE FROM contracts
WHERE id = ANY(sqlc.arg('contract_ids')::uuid[]);

-- name: DeleteCustomersWithoutContractOlderThan :execrows
DELETE FROM customers cu
WHERE cu.created_at < $1
  AND NOT EXISTS (
    SELECT 1
    FROM contracts c
    WHERE c.customer_id = cu.id
  );
