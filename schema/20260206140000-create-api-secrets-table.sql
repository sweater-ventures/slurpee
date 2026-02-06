
-- +migrate Up
CREATE TABLE IF NOT EXISTS api_secrets (
    id              UUID PRIMARY KEY,
    name            TEXT NOT NULL,
    secret_hash     TEXT NOT NULL,
    subject_pattern TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +migrate Down
DROP TABLE IF EXISTS api_secrets;
