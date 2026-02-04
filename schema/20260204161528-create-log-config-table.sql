-- +migrate Up
CREATE TABLE IF NOT EXISTS log_config (
    id UUID PRIMARY KEY,
    subject TEXT NOT NULL UNIQUE,
    log_properties TEXT[] NOT NULL
);

-- +migrate Down
DROP TABLE IF EXISTS log_config;
