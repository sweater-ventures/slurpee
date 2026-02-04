
-- +migrate Up
CREATE TABLE IF NOT EXISTS events (
    id                UUID PRIMARY KEY,
    subject           TEXT NOT NULL,
    timestamp         TIMESTAMPTZ NOT NULL,
    trace_id          UUID,
    data              JSONB NOT NULL,
    retry_count       INTEGER NOT NULL DEFAULT 0,
    delivery_status   TEXT NOT NULL DEFAULT 'pending',
    status_updated_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_events_subject ON events (subject);
CREATE INDEX IF NOT EXISTS idx_events_timestamp ON events (timestamp);
CREATE INDEX IF NOT EXISTS idx_events_delivery_status ON events (delivery_status);

-- +migrate Down
DROP TABLE IF EXISTS events;
