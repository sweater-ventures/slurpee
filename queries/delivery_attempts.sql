-- name: InsertDeliveryAttempt :one
INSERT INTO delivery_attempts (id, event_id, subscriber_id, endpoint_url, attempted_at, request_headers, response_status_code, response_headers, response_body, status)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING *;

-- name: ListDeliveryAttemptsForEvent :many
SELECT * FROM delivery_attempts WHERE event_id = $1 ORDER BY attempted_at;

-- name: ListDeliveryAttemptsForSubscriber :many
SELECT * FROM delivery_attempts WHERE subscriber_id = $1 ORDER BY attempted_at;

-- name: GetDeliverySummaryForEvent :many
SELECT
  subscriber_id,
  COUNT(*) FILTER (WHERE status = 'failed')::bigint AS failed_count,
  COUNT(*) FILTER (WHERE status = 'succeeded')::bigint AS succeeded_count
FROM delivery_attempts
WHERE event_id = $1
GROUP BY subscriber_id;

-- name: UpdateDeliveryAttemptStatus :one
UPDATE delivery_attempts SET status = $1, response_status_code = $2, response_headers = $3, response_body = $4 WHERE id = $5 RETURNING *;
