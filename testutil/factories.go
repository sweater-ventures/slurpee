package testutil

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/sweater-ventures/slurpee/app"
	"github.com/sweater-ventures/slurpee/config"
	"github.com/sweater-ventures/slurpee/db"
)

// NewUUID returns a pgtype.UUID with a new random UUID.
func NewUUID() pgtype.UUID {
	return pgtype.UUID{Bytes: uuid.Must(uuid.NewV7()), Valid: true}
}

// NewTimestamp returns a pgtype.Timestamptz set to now.
func NewTimestamp() pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}
}

// EventOpt is a functional option for building test Events.
type EventOpt func(*db.Event)

// NewEvent creates a db.Event with sensible defaults. Use options to override.
func NewEvent(opts ...EventOpt) db.Event {
	e := db.Event{
		ID:              NewUUID(),
		Subject:         "test.subject",
		Timestamp:       NewTimestamp(),
		Data:            json.RawMessage(`{"key":"value"}`),
		RetryCount:      0,
		DeliveryStatus:  "pending",
		StatusUpdatedAt: NewTimestamp(),
	}
	for _, opt := range opts {
		opt(&e)
	}
	return e
}

// SubscriberOpt is a functional option for building test Subscribers.
type SubscriberOpt func(*db.Subscriber)

// NewSubscriber creates a db.Subscriber with sensible defaults.
func NewSubscriber(opts ...SubscriberOpt) db.Subscriber {
	s := db.Subscriber{
		ID:          NewUUID(),
		Name:        "test-subscriber",
		EndpointUrl: "https://example.com/webhook",
		AuthSecret:  "test-auth-secret",
		MaxParallel: 1,
		CreatedAt:   NewTimestamp(),
		UpdatedAt:   NewTimestamp(),
	}
	for _, opt := range opts {
		opt(&s)
	}
	return s
}

// SubscriptionOpt is a functional option for building test Subscriptions.
type SubscriptionOpt func(*db.Subscription)

// NewSubscription creates a db.Subscription with sensible defaults.
func NewSubscription(opts ...SubscriptionOpt) db.Subscription {
	s := db.Subscription{
		ID:             NewUUID(),
		SubscriberID:   NewUUID(),
		SubjectPattern: "test.%",
		CreatedAt:      NewTimestamp(),
		UpdatedAt:      NewTimestamp(),
	}
	for _, opt := range opts {
		opt(&s)
	}
	return s
}

// ApiSecretOpt is a functional option for building test ApiSecrets.
type ApiSecretOpt func(*db.ApiSecret)

// NewApiSecret creates a db.ApiSecret with sensible defaults.
// The SecretHash is a bcrypt hash of "test-secret-value".
func NewApiSecret(opts ...ApiSecretOpt) db.ApiSecret {
	s := db.ApiSecret{
		ID:             NewUUID(),
		Name:           "test-secret",
		SecretHash:     "$2a$10$abcdefghijklmnopqrstuuABCDEFGHIJKLMNOPQRSTUVWXYZ012", // placeholder hash
		SubjectPattern: "*",
		CreatedAt:      NewTimestamp(),
	}
	for _, opt := range opts {
		opt(&s)
	}
	return s
}

// NewApiSecretWithHash creates an ApiSecret with a real bcrypt hash of the given plaintext.
// This is useful for testing secret validation.
func NewApiSecretWithHash(plaintext string, opts ...ApiSecretOpt) db.ApiSecret {
	hash, err := app.HashSecret(plaintext)
	if err != nil {
		panic("testutil: failed to hash secret: " + err.Error())
	}
	s := NewApiSecret(opts...)
	s.SecretHash = hash
	// Apply opts again in case they override the hash
	for _, opt := range opts {
		opt(&s)
	}
	return s
}

// AppOpt is a functional option for building test Applications.
type AppOpt func(*app.Application)

// NewTestApp creates an app.Application suitable for testing.
// It uses the provided mock Querier and sensible config defaults.
func NewTestApp(mockDB *MockQuerier, opts ...AppOpt) *app.Application {
	a := &app.Application{
		Config: config.AppConfig{
			Port:              8005,
			AdminSecret:       "test-admin-secret",
			MaxParallel:       1,
			MaxRetries:        5,
			MaxBackoffSeconds: 300,
			DeliveryQueueSize: 100,
			DeliveryWorkers:   2,
			DeliveryChanSize:  100,
		},
		DB:           mockDB,
		DeliveryChan: make(chan db.Event, 100),
		EventBus:     app.NewEventBus(),
		Sessions:     app.NewSessionStore(),
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}
