
-- +migrate Up
CREATE TABLE IF NOT EXISTS subscriptions (
    id              UUID PRIMARY KEY,
    subscriber_id   UUID NOT NULL REFERENCES subscribers(id) ON DELETE CASCADE,
    subject_pattern TEXT NOT NULL,
    filter          JSONB,
    max_retries     INTEGER,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_subscriptions_subject_pattern ON subscriptions (subject_pattern);

-- +migrate Down
DROP TABLE IF EXISTS subscriptions;
