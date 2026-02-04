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

-- name: UpdateEventDeliveryStatus :one
UPDATE events SET delivery_status = $1, retry_count = $2, status_updated_at = $3 WHERE id = $4 RETURNING *;
