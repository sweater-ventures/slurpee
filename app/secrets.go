package app

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/sweater-ventures/slurpee/db"
	"golang.org/x/crypto/bcrypt"
)

// GenerateSecret returns a 32+ character URL-safe base64 string using crypto/rand.
func GenerateSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating secret: %w", err)
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// HashSecret returns a bcrypt hash with cost 10.
func HashSecret(plaintext string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(plaintext), 10)
	if err != nil {
		return "", fmt.Errorf("hashing secret: %w", err)
	}
	return string(hash), nil
}

// ValidateSecretByID fetches a single secret by UUID and validates the plaintext
// against its stored hash. Returns the full ApiSecret record or an error.
func ValidateSecretByID(ctx context.Context, queries db.Querier, secretID uuid.UUID, plaintext string) (db.ApiSecret, error) {
	secret, err := queries.GetApiSecretByID(ctx, pgtype.UUID{Bytes: secretID, Valid: true})
	if err != nil {
		return db.ApiSecret{}, fmt.Errorf("secret not found")
	}
	if bcrypt.CompareHashAndPassword([]byte(secret.SecretHash), []byte(plaintext)) != nil {
		return db.ApiSecret{}, fmt.Errorf("invalid secret")
	}
	return secret, nil
}

// CheckSubscriberScope checks if the given secret is associated with the subscriber
// via the join table.
func CheckSubscriberScope(ctx context.Context, queries db.Querier, secretID pgtype.UUID, subscriberID pgtype.UUID) (bool, error) {
	return queries.GetApiSecretSubscriberExists(ctx, db.GetApiSecretSubscriberExistsParams{
		ApiSecretID:  secretID,
		SubscriberID: subscriberID,
	})
}

// CheckSendScope returns true if the subject matches the secret's subject_pattern
// using SQL LIKE semantics (% = any sequence, _ = single char).
func CheckSendScope(subjectPattern, subject string) bool {
	return MatchLikePattern(subjectPattern, subject)
}

// MatchLikePattern implements glob-style matching in Go.
// * matches any sequence of characters (including empty).
// _ matches exactly one character.
func MatchLikePattern(pattern, value string) bool {
	return matchLike(pattern, 0, value, 0)
}

func matchLike(pattern string, pi int, value string, vi int) bool {
	for pi < len(pattern) {
		switch pattern[pi] {
		case '*':
			// Skip consecutive * characters
			for pi < len(pattern) && pattern[pi] == '*' {
				pi++
			}
			if pi == len(pattern) {
				return true
			}
			// Try matching the rest of the pattern against every suffix of value
			for vi <= len(value) {
				if matchLike(pattern, pi, value, vi) {
					return true
				}
				vi++
			}
			return false
		case '_':
			if vi >= len(value) {
				return false
			}
			pi++
			vi++
		default:
			if vi >= len(value) || pattern[pi] != value[vi] {
				return false
			}
			pi++
			vi++
		}
	}
	return vi == len(value)
}
