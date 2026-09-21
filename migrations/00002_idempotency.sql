-- +goose Up

CREATE TABLE idempotency_keys (
    contract_id UUID NOT NULL REFERENCES contracts (id),
    action TEXT NOT NULL,
    key TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    status_code INTEGER NOT NULL,
    body JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (contract_id, action, key)
);

CREATE INDEX idempotency_keys_created_idx ON idempotency_keys (created_at);
