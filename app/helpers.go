package app

import (
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// UuidToString converts a pgtype.UUID to its string representation.
func UuidToString(u pgtype.UUID) string {
	return uuid.UUID(u.Bytes).String()
}
