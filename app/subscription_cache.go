package app

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/sweater-ventures/slurpee/db"
)

// SubscriptionCache lazily bulk-loads all subscribers and subscriptions into
// memory. Subject-pattern matching is performed in Go using MatchLikePattern.
// Call Flush after any subscriber/subscription mutation; the next access
// reloads from the database.
type SubscriptionCache struct {
	mu            sync.RWMutex
	loaded        bool
	subscribers   map[[16]byte]db.Subscriber      // keyed by UUID bytes
	subscriptions []db.Subscription
	db            db.Querier
}

func NewSubscriptionCache(querier db.Querier) *SubscriptionCache {
	return &SubscriptionCache{db: querier}
}

// load performs lazy bulk loading with double-checked locking.
func (c *SubscriptionCache) load(ctx context.Context) error {
	c.mu.RLock()
	if c.loaded {
		c.mu.RUnlock()
		return nil
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check after acquiring write lock
	if c.loaded {
		return nil
	}

	subscribers, err := c.db.ListSubscribers(ctx)
	if err != nil {
		return fmt.Errorf("loading subscribers: %w", err)
	}

	subscriptions, err := c.db.ListAllSubscriptions(ctx)
	if err != nil {
		return fmt.Errorf("loading subscriptions: %w", err)
	}

	c.subscribers = make(map[[16]byte]db.Subscriber, len(subscribers))
	for _, s := range subscribers {
		c.subscribers[s.ID.Bytes] = s
	}
	c.subscriptions = subscriptions
	c.loaded = true
	return nil
}

// GetMatchingSubscriptions returns all subscriptions whose subject_pattern
// matches the given subject. Patterns use * (any sequence) and ? (single char).
func (c *SubscriptionCache) GetMatchingSubscriptions(ctx context.Context, subject string) ([]db.Subscription, error) {
	if err := c.load(ctx); err != nil {
		return nil, err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	var matched []db.Subscription
	for _, sub := range c.subscriptions {
		// Convert SQL-style wildcards to the glob style used by MatchLikePattern:
		// subject_pattern already uses * and ? (not SQL % and _), so we can
		// match directly. But the DB query used replace(*, '%') and replace(?, '_')
		// meaning the stored pattern uses * for "any" and ? for "single char".
		pattern := strings.ReplaceAll(sub.SubjectPattern, "?", "_")
		if MatchLikePattern(pattern, subject) {
			matched = append(matched, sub)
		}
	}
	return matched, nil
}

// GetSubscriberByID returns a subscriber by UUID, or an error if not found.
func (c *SubscriptionCache) GetSubscriberByID(ctx context.Context, id pgtype.UUID) (db.Subscriber, error) {
	if err := c.load(ctx); err != nil {
		return db.Subscriber{}, err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	s, ok := c.subscribers[id.Bytes]
	if !ok {
		return db.Subscriber{}, fmt.Errorf("subscriber not found: %x", id.Bytes)
	}
	return s, nil
}

// Flush clears the cache. The next access will reload from the database.
func (c *SubscriptionCache) Flush() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.loaded = false
	c.subscribers = nil
	c.subscriptions = nil
}
