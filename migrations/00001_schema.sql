-- +goose Up

CREATE TABLE vehicles (
    vin TEXT PRIMARY KEY,
    fleet TEXT NOT NULL CHECK (fleet IN ('rental', 'leasing')),
    display_id TEXT NOT NULL,
    plate TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX vehicles_fleet_idx ON vehicles (fleet);

CREATE TABLE vehicle_state (
    vin TEXT PRIMARY KEY REFERENCES vehicles (vin),
    lat DOUBLE PRECISION NOT NULL DEFAULT 0,
    lng DOUBLE PRECISION NOT NULL DEFAULT 0,
    battery INTEGER NOT NULL DEFAULT 0,
    speed INTEGER NOT NULL DEFAULT 0,
    heading INTEGER NOT NULL DEFAULT 0,
    ignition BOOLEAN NOT NULL DEFAULT false,
    locked BOOLEAN NOT NULL DEFAULT false,
    odometer DOUBLE PRECISION NOT NULL DEFAULT 0,
    trip DOUBLE PRECISION NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE routes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    version INTEGER NOT NULL,
    name TEXT NOT NULL,
    waypoints JSONB NOT NULL DEFAULT '[]',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (name, version)
);

CREATE TABLE customers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    home_lat DOUBLE PRECISION,
    home_lng DOUBLE PRECISION,
    work_lat DOUBLE PRECISION,
    work_lng DOUBLE PRECISION,
    weekend_lat DOUBLE PRECISION,
    weekend_lng DOUBLE PRECISION,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE contracts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_id UUID NOT NULL REFERENCES customers (id),
    vin TEXT NOT NULL REFERENCES vehicles (vin),
    installment_value NUMERIC(12, 2) NOT NULL,
    total_installments INTEGER NOT NULL,
    paid_count INTEGER NOT NULL DEFAULT 0,
    payer_profile TEXT NOT NULL,
    started_on DATE NOT NULL,
    ended_on DATE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE installments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    contract_id UUID NOT NULL REFERENCES contracts (id),
    due_on DATE NOT NULL,
    amount NUMERIC(12, 2) NOT NULL,
    UNIQUE (contract_id, due_on)
);

CREATE TABLE payments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    contract_id UUID NOT NULL REFERENCES contracts (id),
    installment_id UUID REFERENCES installments (id),
    amount NUMERIC(12, 2) NOT NULL,
    source TEXT NOT NULL,
    paid_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE commands (
    id TEXT PRIMARY KEY,
    vin TEXT NOT NULL REFERENCES vehicles (vin),
    fleet TEXT NOT NULL CHECK (fleet IN ('rental', 'leasing')),
    action TEXT NOT NULL,
    state TEXT NOT NULL,
    correlation_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    sent_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE audit_log (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    contract_id UUID REFERENCES contracts (id),
    action TEXT NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}',
    visitor_hash TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE outbox (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    topic TEXT NOT NULL,
    payload BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    sent_at TIMESTAMPTZ
);
