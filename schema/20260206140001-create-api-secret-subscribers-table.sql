
-- +migrate Up
CREATE TABLE IF NOT EXISTS api_secret_subscribers (
    api_secret_id  UUID NOT NULL REFERENCES api_secrets(id) ON DELETE CASCADE,
    subscriber_id  UUID NOT NULL REFERENCES subscribers(id) ON DELETE CASCADE,
    PRIMARY KEY (api_secret_id, subscriber_id)
);

-- +migrate Down
DROP TABLE IF EXISTS api_secret_subscribers;
