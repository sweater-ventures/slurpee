package app

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/sweater-ventures/slurpee/db"
)

func TestSubscriptionCache_LazyLoading(t *testing.T) {
	mockDB := new(deliveryMockQuerier)
	cache := NewSubscriptionCache(mockDB)

	subscriber := newTestSubscriber()
	subscription := newTestSubscription(func(s *db.Subscription) {
		s.SubscriberID = subscriber.ID
		s.SubjectPattern = "orders.*"
	})

	mockDB.On("ListSubscribers", mock.Anything).
		Return([]db.Subscriber{subscriber}, nil).Once()
	mockDB.On("ListAllSubscriptions", mock.Anything).
		Return([]db.Subscription{subscription}, nil).Once()

	ctx := context.Background()

	// First call triggers load
	subs, err := cache.GetMatchingSubscriptions(ctx, "orders.created")
	assert.NoError(t, err)
	assert.Len(t, subs, 1)

	// Second call uses cache (no additional DB calls)
	subs, err = cache.GetMatchingSubscriptions(ctx, "orders.created")
	assert.NoError(t, err)
	assert.Len(t, subs, 1)

	mockDB.AssertExpectations(t)
}

func TestSubscriptionCache_PatternMatching_Star(t *testing.T) {
	mockDB := new(deliveryMockQuerier)
	cache := NewSubscriptionCache(mockDB)

	subscriber := newTestSubscriber()
	sub1 := newTestSubscription(func(s *db.Subscription) {
		s.SubscriberID = subscriber.ID
		s.SubjectPattern = "orders.*"
	})
	sub2 := newTestSubscription(func(s *db.Subscription) {
		s.SubscriberID = subscriber.ID
		s.SubjectPattern = "payments.*"
	})

	mockDB.On("ListSubscribers", mock.Anything).
		Return([]db.Subscriber{subscriber}, nil)
	mockDB.On("ListAllSubscriptions", mock.Anything).
		Return([]db.Subscription{sub1, sub2}, nil)

	ctx := context.Background()

	// Should match orders.* but not payments.*
	subs, err := cache.GetMatchingSubscriptions(ctx, "orders.created")
	assert.NoError(t, err)
	assert.Len(t, subs, 1)
	assert.Equal(t, "orders.*", subs[0].SubjectPattern)

	// Should match payments.* but not orders.*
	subs, err = cache.GetMatchingSubscriptions(ctx, "payments.processed")
	assert.NoError(t, err)
	assert.Len(t, subs, 1)
	assert.Equal(t, "payments.*", subs[0].SubjectPattern)

	// Should match neither
	subs, err = cache.GetMatchingSubscriptions(ctx, "users.created")
	assert.NoError(t, err)
	assert.Len(t, subs, 0)
}

func TestSubscriptionCache_PatternMatching_QuestionMark(t *testing.T) {
	mockDB := new(deliveryMockQuerier)
	cache := NewSubscriptionCache(mockDB)

	subscriber := newTestSubscriber()
	sub := newTestSubscription(func(s *db.Subscription) {
		s.SubscriberID = subscriber.ID
		s.SubjectPattern = "orders.?"
	})

	mockDB.On("ListSubscribers", mock.Anything).
		Return([]db.Subscriber{subscriber}, nil)
	mockDB.On("ListAllSubscriptions", mock.Anything).
		Return([]db.Subscription{sub}, nil)

	ctx := context.Background()

	// ? matches exactly one character
	subs, err := cache.GetMatchingSubscriptions(ctx, "orders.a")
	assert.NoError(t, err)
	assert.Len(t, subs, 1)

	// ? does not match empty or multiple characters
	subs, err = cache.GetMatchingSubscriptions(ctx, "orders.")
	assert.NoError(t, err)
	assert.Len(t, subs, 0)

	subs, err = cache.GetMatchingSubscriptions(ctx, "orders.ab")
	assert.NoError(t, err)
	assert.Len(t, subs, 0)
}

func TestSubscriptionCache_GetSubscriberByID(t *testing.T) {
	mockDB := new(deliveryMockQuerier)
	cache := NewSubscriptionCache(mockDB)

	subscriber := newTestSubscriber()

	mockDB.On("ListSubscribers", mock.Anything).
		Return([]db.Subscriber{subscriber}, nil)
	mockDB.On("ListAllSubscriptions", mock.Anything).
		Return([]db.Subscription{}, nil)

	ctx := context.Background()

	s, err := cache.GetSubscriberByID(ctx, subscriber.ID)
	assert.NoError(t, err)
	assert.Equal(t, subscriber.ID, s.ID)
	assert.Equal(t, subscriber.Name, s.Name)
}

func TestSubscriptionCache_GetSubscriberByID_NotFound(t *testing.T) {
	mockDB := new(deliveryMockQuerier)
	cache := NewSubscriptionCache(mockDB)

	mockDB.On("ListSubscribers", mock.Anything).
		Return([]db.Subscriber{}, nil)
	mockDB.On("ListAllSubscriptions", mock.Anything).
		Return([]db.Subscription{}, nil)

	ctx := context.Background()

	_, err := cache.GetSubscriberByID(ctx, newTestUUID())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "subscriber not found")
}

func TestSubscriptionCache_FlushAndReload(t *testing.T) {
	mockDB := new(deliveryMockQuerier)
	cache := NewSubscriptionCache(mockDB)

	subscriber := newTestSubscriber()
	sub1 := newTestSubscription(func(s *db.Subscription) {
		s.SubscriberID = subscriber.ID
		s.SubjectPattern = "orders.*"
	})

	// First load
	mockDB.On("ListSubscribers", mock.Anything).
		Return([]db.Subscriber{subscriber}, nil).Once()
	mockDB.On("ListAllSubscriptions", mock.Anything).
		Return([]db.Subscription{sub1}, nil).Once()

	ctx := context.Background()

	subs, err := cache.GetMatchingSubscriptions(ctx, "orders.created")
	assert.NoError(t, err)
	assert.Len(t, subs, 1)

	// Add a new subscription and flush
	sub2 := newTestSubscription(func(s *db.Subscription) {
		s.SubscriberID = subscriber.ID
		s.SubjectPattern = "payments.*"
	})

	mockDB.On("ListSubscribers", mock.Anything).
		Return([]db.Subscriber{subscriber}, nil).Once()
	mockDB.On("ListAllSubscriptions", mock.Anything).
		Return([]db.Subscription{sub1, sub2}, nil).Once()

	cache.Flush()

	// After flush, next access reloads and sees the new subscription
	subs, err = cache.GetMatchingSubscriptions(ctx, "payments.processed")
	assert.NoError(t, err)
	assert.Len(t, subs, 1)
	assert.Equal(t, "payments.*", subs[0].SubjectPattern)

	mockDB.AssertExpectations(t)
}

func TestSubscriptionCache_LoadError_Propagates(t *testing.T) {
	mockDB := new(deliveryMockQuerier)
	cache := NewSubscriptionCache(mockDB)

	mockDB.On("ListSubscribers", mock.Anything).
		Return([]db.Subscriber(nil), assert.AnError)

	ctx := context.Background()

	_, err := cache.GetMatchingSubscriptions(ctx, "orders.created")
	assert.Error(t, err)

	_, err = cache.GetSubscriberByID(ctx, pgtype.UUID{})
	assert.Error(t, err)
}
