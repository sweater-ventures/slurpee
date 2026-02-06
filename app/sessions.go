package app

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

type SessionInfo struct {
	SecretID       pgtype.UUID
	SubjectPattern string
	SubscriberIDs  []pgtype.UUID
	IsAdmin        bool
	ExpiresAt      time.Time
}

type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]SessionInfo
}

func NewSessionStore() *SessionStore {
	return &SessionStore{
		sessions: make(map[string]SessionInfo),
	}
}

// CreateSession generates a crypto/rand token (32 bytes, hex encoded),
// stores the session with 24h expiry, and returns the token.
func (s *SessionStore) CreateSession(info SessionInfo) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b)

	info.ExpiresAt = time.Now().Add(24 * time.Hour)

	s.mu.Lock()
	s.sessions[token] = info
	s.mu.Unlock()

	return token, nil
}

// GetSession returns the SessionInfo for the given token, or nil if expired or missing.
// Expired sessions are deleted on access.
func (s *SessionStore) GetSession(token string) *SessionInfo {
	s.mu.RLock()
	info, ok := s.sessions[token]
	s.mu.RUnlock()

	if !ok {
		return nil
	}

	if time.Now().After(info.ExpiresAt) {
		s.mu.Lock()
		delete(s.sessions, token)
		s.mu.Unlock()
		return nil
	}

	return &info
}

// DeleteSession removes the session from the store.
func (s *SessionStore) DeleteSession(token string) {
	s.mu.Lock()
	delete(s.sessions, token)
	s.mu.Unlock()
}
