package testutil

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/mock"
	"github.com/sweater-ventures/slurpee/db"
)

// MockQuerier is a testify mock implementation of db.Querier.
type MockQuerier struct {
	mock.Mock
}

var _ db.Querier = (*MockQuerier)(nil)

func (m *MockQuerier) AddApiSecretSubscriber(ctx context.Context, arg db.AddApiSecretSubscriberParams) error {
	args := m.Called(ctx, arg)
	return args.Error(0)
}

func (m *MockQuerier) CountEventsAfterTimestamp(ctx context.Context, arg db.CountEventsAfterTimestampParams) (int64, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(int64), args.Error(1)
}

func (m *MockQuerier) CreateSubscription(ctx context.Context, arg db.CreateSubscriptionParams) (db.Subscription, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.Subscription), args.Error(1)
}

func (m *MockQuerier) DeleteApiSecret(ctx context.Context, id pgtype.UUID) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockQuerier) DeleteLogConfigForSubject(ctx context.Context, subject string) error {
	args := m.Called(ctx, subject)
	return args.Error(0)
}

func (m *MockQuerier) DeleteSubscriber(ctx context.Context, id pgtype.UUID) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockQuerier) DeleteSubscription(ctx context.Context, id pgtype.UUID) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockQuerier) DeleteSubscriptionsForSubscriber(ctx context.Context, subscriberID pgtype.UUID) error {
	args := m.Called(ctx, subscriberID)
	return args.Error(0)
}

func (m *MockQuerier) GetApiSecretByID(ctx context.Context, id pgtype.UUID) (db.ApiSecret, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(db.ApiSecret), args.Error(1)
}

func (m *MockQuerier) GetApiSecretSubscriberExists(ctx context.Context, arg db.GetApiSecretSubscriberExistsParams) (bool, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(bool), args.Error(1)
}

func (m *MockQuerier) GetDeliverySummaryForEvent(ctx context.Context, eventID pgtype.UUID) ([]db.GetDeliverySummaryForEventRow, error) {
	args := m.Called(ctx, eventID)
	return args.Get(0).([]db.GetDeliverySummaryForEventRow), args.Error(1)
}

func (m *MockQuerier) GetEventByID(ctx context.Context, id pgtype.UUID) (db.Event, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(db.Event), args.Error(1)
}

func (m *MockQuerier) GetResumableEvents(ctx context.Context) ([]db.Event, error) {
	args := m.Called(ctx)
	return args.Get(0).([]db.Event), args.Error(1)
}

func (m *MockQuerier) GetLogConfigBySubject(ctx context.Context, subject string) (db.LogConfig, error) {
	args := m.Called(ctx, subject)
	return args.Get(0).(db.LogConfig), args.Error(1)
}

func (m *MockQuerier) GetSubscriberByEndpointURL(ctx context.Context, endpointUrl string) (db.Subscriber, error) {
	args := m.Called(ctx, endpointUrl)
	return args.Get(0).(db.Subscriber), args.Error(1)
}

func (m *MockQuerier) GetSubscriberByID(ctx context.Context, id pgtype.UUID) (db.Subscriber, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(db.Subscriber), args.Error(1)
}

func (m *MockQuerier) GetSubscriptionsMatchingSubject(ctx context.Context, subjectPattern string) ([]db.Subscription, error) {
	args := m.Called(ctx, subjectPattern)
	return args.Get(0).([]db.Subscription), args.Error(1)
}

func (m *MockQuerier) InsertApiSecret(ctx context.Context, arg db.InsertApiSecretParams) (db.ApiSecret, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.ApiSecret), args.Error(1)
}

func (m *MockQuerier) InsertDeliveryAttempt(ctx context.Context, arg db.InsertDeliveryAttemptParams) (db.DeliveryAttempt, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.DeliveryAttempt), args.Error(1)
}

func (m *MockQuerier) InsertEvent(ctx context.Context, arg db.InsertEventParams) (db.Event, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.Event), args.Error(1)
}

func (m *MockQuerier) ListAllApiSecretHashes(ctx context.Context) ([]db.ListAllApiSecretHashesRow, error) {
	args := m.Called(ctx)
	return args.Get(0).([]db.ListAllApiSecretHashesRow), args.Error(1)
}

func (m *MockQuerier) ListApiSecrets(ctx context.Context) ([]db.ListApiSecretsRow, error) {
	args := m.Called(ctx)
	return args.Get(0).([]db.ListApiSecretsRow), args.Error(1)
}

func (m *MockQuerier) ListApiSecretsForSubscriber(ctx context.Context, subscriberID pgtype.UUID) ([]db.ApiSecret, error) {
	args := m.Called(ctx, subscriberID)
	return args.Get(0).([]db.ApiSecret), args.Error(1)
}

func (m *MockQuerier) ListDeliveryAttemptsForEvent(ctx context.Context, eventID pgtype.UUID) ([]db.DeliveryAttempt, error) {
	args := m.Called(ctx, eventID)
	return args.Get(0).([]db.DeliveryAttempt), args.Error(1)
}

func (m *MockQuerier) ListDeliveryAttemptsForSubscriber(ctx context.Context, subscriberID pgtype.UUID) ([]db.DeliveryAttempt, error) {
	args := m.Called(ctx, subscriberID)
	return args.Get(0).([]db.DeliveryAttempt), args.Error(1)
}

func (m *MockQuerier) ListEvents(ctx context.Context, arg db.ListEventsParams) ([]db.Event, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).([]db.Event), args.Error(1)
}

func (m *MockQuerier) ListEventsAfterTimestamp(ctx context.Context, arg db.ListEventsAfterTimestampParams) ([]db.Event, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).([]db.Event), args.Error(1)
}

func (m *MockQuerier) ListLogConfigs(ctx context.Context) ([]db.LogConfig, error) {
	args := m.Called(ctx)
	return args.Get(0).([]db.LogConfig), args.Error(1)
}

func (m *MockQuerier) ListSubscribers(ctx context.Context) ([]db.Subscriber, error) {
	args := m.Called(ctx)
	return args.Get(0).([]db.Subscriber), args.Error(1)
}

func (m *MockQuerier) ListSubscribersForApiSecret(ctx context.Context, apiSecretID pgtype.UUID) ([]db.Subscriber, error) {
	args := m.Called(ctx, apiSecretID)
	return args.Get(0).([]db.Subscriber), args.Error(1)
}

func (m *MockQuerier) ListSubscribersWithCounts(ctx context.Context) ([]db.ListSubscribersWithCountsRow, error) {
	args := m.Called(ctx)
	return args.Get(0).([]db.ListSubscribersWithCountsRow), args.Error(1)
}

func (m *MockQuerier) ListSubscriptionsForSubscriber(ctx context.Context, subscriberID pgtype.UUID) ([]db.Subscription, error) {
	args := m.Called(ctx, subscriberID)
	return args.Get(0).([]db.Subscription), args.Error(1)
}

func (m *MockQuerier) RemoveAllApiSecretSubscribers(ctx context.Context, apiSecretID pgtype.UUID) error {
	args := m.Called(ctx, apiSecretID)
	return args.Error(0)
}

func (m *MockQuerier) RemoveApiSecretSubscriber(ctx context.Context, arg db.RemoveApiSecretSubscriberParams) error {
	args := m.Called(ctx, arg)
	return args.Error(0)
}

func (m *MockQuerier) SearchEventsByDataContent(ctx context.Context, arg db.SearchEventsByDataContentParams) ([]db.Event, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).([]db.Event), args.Error(1)
}

func (m *MockQuerier) SearchEventsByDateRange(ctx context.Context, arg db.SearchEventsByDateRangeParams) ([]db.Event, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).([]db.Event), args.Error(1)
}

func (m *MockQuerier) SearchEventsByDeliveryStatus(ctx context.Context, arg db.SearchEventsByDeliveryStatusParams) ([]db.Event, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).([]db.Event), args.Error(1)
}

func (m *MockQuerier) SearchEventsBySubject(ctx context.Context, arg db.SearchEventsBySubjectParams) ([]db.Event, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).([]db.Event), args.Error(1)
}

func (m *MockQuerier) SearchEventsFiltered(ctx context.Context, arg db.SearchEventsFilteredParams) ([]db.Event, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).([]db.Event), args.Error(1)
}

func (m *MockQuerier) UpdateApiSecret(ctx context.Context, arg db.UpdateApiSecretParams) (db.ApiSecret, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.ApiSecret), args.Error(1)
}

func (m *MockQuerier) UpdateDeliveryAttemptStatus(ctx context.Context, arg db.UpdateDeliveryAttemptStatusParams) (db.DeliveryAttempt, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.DeliveryAttempt), args.Error(1)
}

func (m *MockQuerier) UpdateEventDeliveryStatus(ctx context.Context, arg db.UpdateEventDeliveryStatusParams) (db.Event, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.Event), args.Error(1)
}

func (m *MockQuerier) UpdateSubscriber(ctx context.Context, arg db.UpdateSubscriberParams) (db.Subscriber, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.Subscriber), args.Error(1)
}

func (m *MockQuerier) UpsertLogConfig(ctx context.Context, arg db.UpsertLogConfigParams) (db.LogConfig, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.LogConfig), args.Error(1)
}

func (m *MockQuerier) UpsertSubscriber(ctx context.Context, arg db.UpsertSubscriberParams) (db.Subscriber, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.Subscriber), args.Error(1)
}
