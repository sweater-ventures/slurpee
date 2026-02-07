-- name: InsertApiSecret :one
INSERT INTO api_secrets (id, name, secret_hash, subject_pattern, created_at)
VALUES ($1, $2, $3, $4, now())
RETURNING *;

-- name: GetApiSecretByID :one
SELECT * FROM api_secrets WHERE id = $1;

-- name: ListApiSecrets :many
SELECT
    s.*,
    COALESCE(
        string_agg(sub.name, ', ' ORDER BY sub.name),
        ''
    )::text AS subscriber_names
FROM api_secrets s
LEFT JOIN api_secret_subscribers ass ON ass.api_secret_id = s.id
LEFT JOIN subscribers sub ON sub.id = ass.subscriber_id
GROUP BY s.id
ORDER BY s.created_at DESC;

-- name: DeleteApiSecret :exec
DELETE FROM api_secrets WHERE id = $1;

-- name: AddApiSecretSubscriber :exec
INSERT INTO api_secret_subscribers (api_secret_id, subscriber_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: RemoveApiSecretSubscriber :exec
DELETE FROM api_secret_subscribers
WHERE api_secret_id = $1 AND subscriber_id = $2;

-- name: ListSubscribersForApiSecret :many
SELECT sub.*
FROM subscribers sub
JOIN api_secret_subscribers ass ON ass.subscriber_id = sub.id
WHERE ass.api_secret_id = $1
ORDER BY sub.name;

-- name: ListApiSecretsForSubscriber :many
SELECT s.*
FROM api_secrets s
JOIN api_secret_subscribers ass ON ass.api_secret_id = s.id
WHERE ass.subscriber_id = $1
ORDER BY s.created_at DESC;

-- name: UpdateApiSecret :one
UPDATE api_secrets SET
    name = sqlc.arg(name),
    subject_pattern = sqlc.arg(subject_pattern)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: RemoveAllApiSecretSubscribers :exec
DELETE FROM api_secret_subscribers WHERE api_secret_id = $1;

-- name: ListAllApiSecretHashes :many
SELECT id, secret_hash, subject_pattern FROM api_secrets;

-- name: GetApiSecretSubscriberExists :one
SELECT EXISTS(
    SELECT 1 FROM api_secret_subscribers
    WHERE api_secret_id = $1 AND subscriber_id = $2
) AS exists;
