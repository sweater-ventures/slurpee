-- name: UpsertLogConfig :one
INSERT INTO log_config (id, subject, log_properties)
VALUES ($1, $2, $3)
ON CONFLICT (subject) DO UPDATE SET
    log_properties = EXCLUDED.log_properties
RETURNING *;

-- name: GetLogConfigBySubject :one
SELECT * FROM log_config WHERE subject = $1;

-- name: ListLogConfigs :many
SELECT * FROM log_config ORDER BY subject;

-- name: DeleteLogConfigForSubject :exec
DELETE FROM log_config WHERE subject = $1;
