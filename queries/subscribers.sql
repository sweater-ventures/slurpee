-- name: UpsertSubscriber :one
INSERT INTO subscribers (id, name, endpoint_url, auth_secret, max_parallel, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, now(), now())
ON CONFLICT (endpoint_url) DO UPDATE SET
    name = EXCLUDED.name,
    auth_secret = EXCLUDED.auth_secret,
    max_parallel = EXCLUDED.max_parallel,
    updated_at = now()
RETURNING *;

-- name: GetSubscriberByID :one
SELECT * FROM subscribers WHERE id = $1;

-- name: GetSubscriberByEndpointURL :one
SELECT * FROM subscribers WHERE endpoint_url = $1;

-- name: ListSubscribers :many
SELECT * FROM subscribers ORDER BY created_at DESC;

-- name: DeleteSubscriber :exec
DELETE FROM subscribers WHERE id = $1;

-- name: CreateSubscription :one
INSERT INTO subscriptions (id, subscriber_id, subject_pattern, filter, max_retries, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, now(), now())
RETURNING *;

-- name: ListSubscriptionsForSubscriber :many
SELECT * FROM subscriptions WHERE subscriber_id = $1 ORDER BY created_at;

-- name: DeleteSubscriptionsForSubscriber :exec
DELETE FROM subscriptions WHERE subscriber_id = $1;

-- name: GetSubscriptionsMatchingSubject :many
SELECT * FROM subscriptions WHERE $1 LIKE replace(replace(subject_pattern, '*', '%'), '?', '_');
