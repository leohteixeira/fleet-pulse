-- +goose Up

-- The book ticker reads the active contracts several times per second. Ended
-- contracts accumulate between retention runs, so every "ended_on IS NULL"
-- read must stay proportional to the 40–60 active rows, not to the history.
CREATE INDEX contracts_active_vin_idx ON contracts (vin) WHERE ended_on IS NULL;

-- Referencing columns read per contract and checked by the foreign keys when
-- retention deletes ended contracts, their installments and their customers.
CREATE INDEX contracts_customer_id_idx ON contracts (customer_id);
CREATE INDEX payments_contract_id_idx ON payments (contract_id);
CREATE INDEX payments_installment_id_idx ON payments (installment_id);
CREATE INDEX audit_log_contract_id_idx ON audit_log (contract_id, created_at);
