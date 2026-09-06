-- Runtime schema version 1. Startup applies this shape under an advisory lock.
-- The encrypted payload is a versioned control snapshot, not plaintext lease data.
CREATE TABLE IF NOT EXISTS control_state (
    id integer PRIMARY KEY CHECK (id = 1),
    schema_version integer NOT NULL CHECK (schema_version = 1),
    payload bytea NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS control_audit (
    id bigserial PRIMARY KEY,
    occurred_at timestamptz NOT NULL DEFAULT now(),
    action text NOT NULL
);
CREATE INDEX IF NOT EXISTS control_audit_occurred_at
    ON control_audit (occurred_at);
