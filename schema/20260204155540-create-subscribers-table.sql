
-- +migrate Up
CREATE TABLE IF NOT EXISTS subscribers (
    id            UUID PRIMARY KEY,
    name          TEXT NOT NULL,
    endpoint_url  TEXT NOT NULL UNIQUE,
    auth_secret   TEXT NOT NULL,
    max_parallel  INTEGER NOT NULL DEFAULT 1,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +migrate Down
DROP TABLE IF EXISTS subscribers;
