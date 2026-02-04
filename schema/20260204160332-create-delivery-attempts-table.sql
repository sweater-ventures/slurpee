-- +migrate Up
CREATE TABLE IF NOT EXISTS delivery_attempts (
    id               UUID        PRIMARY KEY,
    event_id         UUID        NOT NULL REFERENCES events(id),
    subscriber_id    UUID        NOT NULL REFERENCES subscribers(id),
    endpoint_url     TEXT        NOT NULL,
    attempted_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    request_headers  JSONB,
    response_status_code INTEGER,
    response_headers JSONB,
    response_body    TEXT,
    status           TEXT        NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_delivery_attempts_event_id ON delivery_attempts(event_id);
CREATE INDEX IF NOT EXISTS idx_delivery_attempts_subscriber_id ON delivery_attempts(subscriber_id);
CREATE INDEX IF NOT EXISTS idx_delivery_attempts_status ON delivery_attempts(status);

-- +migrate Down
DROP TABLE IF EXISTS delivery_attempts;
