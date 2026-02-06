-- name: InsertEvent :one
INSERT INTO events (id, subject, timestamp, trace_id, data, retry_count, delivery_status, status_updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetEventByID :one
SELECT * FROM events WHERE id = $1;

-- name: ListEvents :many
SELECT * FROM events ORDER BY timestamp DESC LIMIT $1 OFFSET $2;

-- name: SearchEventsBySubject :many
SELECT * FROM events WHERE subject = $1 ORDER BY timestamp DESC LIMIT $2 OFFSET $3;

-- name: SearchEventsByDateRange :many
SELECT * FROM events WHERE timestamp >= sqlc.arg(start_time) AND timestamp <= sqlc.arg(end_time) ORDER BY timestamp DESC LIMIT $1 OFFSET $2;

-- name: SearchEventsByDeliveryStatus :many
SELECT * FROM events WHERE delivery_status = $1 ORDER BY timestamp DESC LIMIT $2 OFFSET $3;

-- name: SearchEventsByDataContent :many
SELECT * FROM events WHERE data @> $1 ORDER BY timestamp DESC LIMIT $2 OFFSET $3;

-- name: SearchEventsFiltered :many
SELECT * FROM events
WHERE
  (sqlc.arg(subject_filter)::text = '' OR subject LIKE sqlc.arg(subject_filter))
  AND (sqlc.arg(status_filter)::text = '' OR delivery_status = sqlc.arg(status_filter))
  AND (sqlc.narg(start_time_filter)::timestamptz IS NULL OR timestamp >= sqlc.narg(start_time_filter))
  AND (sqlc.narg(end_time_filter)::timestamptz IS NULL OR timestamp <= sqlc.narg(end_time_filter))
  AND (sqlc.narg(data_filter)::jsonb IS NULL OR data @> sqlc.narg(data_filter))
ORDER BY timestamp DESC LIMIT $1 OFFSET $2;

-- name: CountEventsAfterTimestamp :one
SELECT count(*) FROM events
WHERE timestamp > sqlc.arg(after_timestamp)::timestamptz
  AND (sqlc.arg(subject_filter)::text = '' OR subject LIKE sqlc.arg(subject_filter))
  AND (sqlc.arg(status_filter)::text = '' OR delivery_status = sqlc.arg(status_filter))
  AND (sqlc.narg(data_filter)::jsonb IS NULL OR data @> sqlc.narg(data_filter));

-- name: UpdateEventDeliveryStatus :one
UPDATE events SET delivery_status = $1, retry_count = $2, status_updated_at = $3 WHERE id = $4 RETURNING *;
