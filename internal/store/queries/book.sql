-- name: InsertCustomer :one
INSERT INTO customers (name)
VALUES ($1)
RETURNING id;

-- name: InsertContract :one
INSERT INTO contracts (
    customer_id,
    vin,
    installment_value,
    total_installments,
    paid_count,
    payer_profile,
    started_on
) VALUES (
    $1, $2, $3, $4, $5, $6, $7
)
RETURNING id;

-- name: InsertInstallment :one
INSERT INTO installments (contract_id, due_on, amount)
VALUES ($1, $2, $3)
RETURNING id;

-- name: InsertPayment :exec
INSERT INTO payments (contract_id, installment_id, amount, source, paid_at)
VALUES ($1, $2, $3, $4, $5);

-- name: IncrementPaidCount :exec
UPDATE contracts
SET paid_count = paid_count + 1
WHERE id = $1;

-- name: EndContract :exec
UPDATE contracts
SET ended_on = $2
WHERE id = $1;

-- name: CountActiveContracts :one
SELECT count(*)::bigint
FROM contracts
WHERE ended_on IS NULL;

-- name: ListActiveContracts :many
SELECT
    c.id,
    c.vin,
    c.payer_profile,
    c.installment_value,
    c.total_installments,
    c.paid_count,
    c.started_on,
    cu.name AS customer_name
FROM contracts c
JOIN customers cu ON cu.id = c.customer_id
JOIN vehicles v ON v.vin = c.vin
WHERE c.ended_on IS NULL
  AND v.fleet = 'leasing'
ORDER BY cu.name, c.vin;

-- name: ListActiveInstallments :many
SELECT i.id, i.contract_id, i.due_on, i.amount
FROM installments i
JOIN contracts c ON c.id = i.contract_id
WHERE c.ended_on IS NULL
ORDER BY i.due_on;

-- name: ListActivePayments :many
SELECT p.id, p.contract_id, p.installment_id, p.amount, p.source
FROM payments p
JOIN contracts c ON c.id = p.contract_id
WHERE c.ended_on IS NULL;

-- name: ListFreeLeasingVins :many
SELECT v.vin
FROM vehicles v
WHERE v.fleet = 'leasing'
  AND NOT EXISTS (
    SELECT 1
    FROM contracts c
    WHERE c.vin = v.vin
      AND c.ended_on IS NULL
  )
ORDER BY v.vin;
