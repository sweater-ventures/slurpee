package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/sweater-ventures/slurpee/config"
	"github.com/sweater-ventures/slurpee/db"
)

// --- local test helpers (avoid importing testutil to prevent import cycle) ---

// deliveryMockQuerier is a testify mock implementation of db.Querier for delivery tests.
type deliveryMockQuerier struct {
	mock.Mock
}

var _ db.Querier = (*deliveryMockQuerier)(nil)

func (m *deliveryMockQuerier) AddApiSecretSubscriber(ctx context.Context, arg db.AddApiSecretSubscriberParams) error {
	return m.Called(ctx, arg).Error(0)
}
func (m *deliveryMockQuerier) CountEventsAfterTimestamp(ctx context.Context, arg db.CountEventsAfterTimestampParams) (int64, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(int64), args.Error(1)
}
func (m *deliveryMockQuerier) CreateSubscription(ctx context.Context, arg db.CreateSubscriptionParams) (db.Subscription, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.Subscription), args.Error(1)
}
func (m *deliveryMockQuerier) DeleteApiSecret(ctx context.Context, id pgtype.UUID) error {
	return m.Called(ctx, id).Error(0)
}
func (m *deliveryMockQuerier) DeleteLogConfigForSubject(ctx context.Context, subject string) error {
	return m.Called(ctx, subject).Error(0)
}
func (m *deliveryMockQuerier) DeleteSubscriber(ctx context.Context, id pgtype.UUID) error {
	return m.Called(ctx, id).Error(0)
}
func (m *deliveryMockQuerier) DeleteSubscription(ctx context.Context, id pgtype.UUID) error {
	return m.Called(ctx, id).Error(0)
}
func (m *deliveryMockQuerier) DeleteSubscriptionsForSubscriber(ctx context.Context, subscriberID pgtype.UUID) error {
	return m.Called(ctx, subscriberID).Error(0)
}
func (m *deliveryMockQuerier) GetApiSecretByID(ctx context.Context, id pgtype.UUID) (db.ApiSecret, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(db.ApiSecret), args.Error(1)
}
func (m *deliveryMockQuerier) GetApiSecretSubscriberExists(ctx context.Context, arg db.GetApiSecretSubscriberExistsParams) (bool, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(bool), args.Error(1)
}
func (m *deliveryMockQuerier) GetDeliverySummaryForEvent(ctx context.Context, eventID pgtype.UUID) ([]db.GetDeliverySummaryForEventRow, error) {
	args := m.Called(ctx, eventID)
	return args.Get(0).([]db.GetDeliverySummaryForEventRow), args.Error(1)
}
func (m *deliveryMockQuerier) GetEventByID(ctx context.Context, id pgtype.UUID) (db.Event, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(db.Event), args.Error(1)
}
func (m *deliveryMockQuerier) GetResumableEvents(ctx context.Context) ([]db.Event, error) {
	args := m.Called(ctx)
	return args.Get(0).([]db.Event), args.Error(1)
}
func (m *deliveryMockQuerier) GetLogConfigBySubject(ctx context.Context, subject string) (db.LogConfig, error) {
	args := m.Called(ctx, subject)
	return args.Get(0).(db.LogConfig), args.Error(1)
}
func (m *deliveryMockQuerier) GetSubscriberByEndpointURL(ctx context.Context, endpointUrl string) (db.Subscriber, error) {
	args := m.Called(ctx, endpointUrl)
	return args.Get(0).(db.Subscriber), args.Error(1)
}
func (m *deliveryMockQuerier) GetSubscriberByID(ctx context.Context, id pgtype.UUID) (db.Subscriber, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(db.Subscriber), args.Error(1)
}
func (m *deliveryMockQuerier) GetSubscriptionsMatchingSubject(ctx context.Context, subjectPattern string) ([]db.Subscription, error) {
	args := m.Called(ctx, subjectPattern)
	return args.Get(0).([]db.Subscription), args.Error(1)
}
func (m *deliveryMockQuerier) InsertApiSecret(ctx context.Context, arg db.InsertApiSecretParams) (db.ApiSecret, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.ApiSecret), args.Error(1)
}
func (m *deliveryMockQuerier) InsertDeliveryAttempt(ctx context.Context, arg db.InsertDeliveryAttemptParams) (db.DeliveryAttempt, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.DeliveryAttempt), args.Error(1)
}
func (m *deliveryMockQuerier) InsertEvent(ctx context.Context, arg db.InsertEventParams) (db.Event, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.Event), args.Error(1)
}
func (m *deliveryMockQuerier) ListAllApiSecretHashes(ctx context.Context) ([]db.ListAllApiSecretHashesRow, error) {
	args := m.Called(ctx)
	return args.Get(0).([]db.ListAllApiSecretHashesRow), args.Error(1)
}
func (m *deliveryMockQuerier) ListAllSubscriptions(ctx context.Context) ([]db.Subscription, error) {
	args := m.Called(ctx)
	return args.Get(0).([]db.Subscription), args.Error(1)
}
func (m *deliveryMockQuerier) ListApiSecrets(ctx context.Context) ([]db.ListApiSecretsRow, error) {
	args := m.Called(ctx)
	return args.Get(0).([]db.ListApiSecretsRow), args.Error(1)
}
func (m *deliveryMockQuerier) ListApiSecretsForSubscriber(ctx context.Context, subscriberID pgtype.UUID) ([]db.ApiSecret, error) {
	args := m.Called(ctx, subscriberID)
	return args.Get(0).([]db.ApiSecret), args.Error(1)
}
func (m *deliveryMockQuerier) ListDeliveryAttemptsForEvent(ctx context.Context, eventID pgtype.UUID) ([]db.DeliveryAttempt, error) {
	args := m.Called(ctx, eventID)
	return args.Get(0).([]db.DeliveryAttempt), args.Error(1)
}
func (m *deliveryMockQuerier) ListDeliveryAttemptsForSubscriber(ctx context.Context, subscriberID pgtype.UUID) ([]db.DeliveryAttempt, error) {
	args := m.Called(ctx, subscriberID)
	return args.Get(0).([]db.DeliveryAttempt), args.Error(1)
}
func (m *deliveryMockQuerier) ListEvents(ctx context.Context, arg db.ListEventsParams) ([]db.Event, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).([]db.Event), args.Error(1)
}
func (m *deliveryMockQuerier) ListEventsAfterTimestamp(ctx context.Context, arg db.ListEventsAfterTimestampParams) ([]db.Event, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).([]db.Event), args.Error(1)
}
func (m *deliveryMockQuerier) ListLogConfigs(ctx context.Context) ([]db.LogConfig, error) {
	args := m.Called(ctx)
	return args.Get(0).([]db.LogConfig), args.Error(1)
}
func (m *deliveryMockQuerier) ListSubscribers(ctx context.Context) ([]db.Subscriber, error) {
	args := m.Called(ctx)
	return args.Get(0).([]db.Subscriber), args.Error(1)
}
func (m *deliveryMockQuerier) ListSubscribersForApiSecret(ctx context.Context, apiSecretID pgtype.UUID) ([]db.Subscriber, error) {
	args := m.Called(ctx, apiSecretID)
	return args.Get(0).([]db.Subscriber), args.Error(1)
}
func (m *deliveryMockQuerier) ListSubscribersWithCounts(ctx context.Context) ([]db.ListSubscribersWithCountsRow, error) {
	args := m.Called(ctx)
	return args.Get(0).([]db.ListSubscribersWithCountsRow), args.Error(1)
}
func (m *deliveryMockQuerier) ListSubscriptionsForSubscriber(ctx context.Context, subscriberID pgtype.UUID) ([]db.Subscription, error) {
	args := m.Called(ctx, subscriberID)
	return args.Get(0).([]db.Subscription), args.Error(1)
}
func (m *deliveryMockQuerier) RemoveAllApiSecretSubscribers(ctx context.Context, apiSecretID pgtype.UUID) error {
	return m.Called(ctx, apiSecretID).Error(0)
}
func (m *deliveryMockQuerier) RemoveApiSecretSubscriber(ctx context.Context, arg db.RemoveApiSecretSubscriberParams) error {
	return m.Called(ctx, arg).Error(0)
}
func (m *deliveryMockQuerier) SearchEventsByDataContent(ctx context.Context, arg db.SearchEventsByDataContentParams) ([]db.Event, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).([]db.Event), args.Error(1)
}
func (m *deliveryMockQuerier) SearchEventsByDateRange(ctx context.Context, arg db.SearchEventsByDateRangeParams) ([]db.Event, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).([]db.Event), args.Error(1)
}
func (m *deliveryMockQuerier) SearchEventsByDeliveryStatus(ctx context.Context, arg db.SearchEventsByDeliveryStatusParams) ([]db.Event, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).([]db.Event), args.Error(1)
}
func (m *deliveryMockQuerier) SearchEventsBySubject(ctx context.Context, arg db.SearchEventsBySubjectParams) ([]db.Event, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).([]db.Event), args.Error(1)
}
func (m *deliveryMockQuerier) SearchEventsFiltered(ctx context.Context, arg db.SearchEventsFilteredParams) ([]db.Event, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).([]db.Event), args.Error(1)
}
func (m *deliveryMockQuerier) UpdateApiSecret(ctx context.Context, arg db.UpdateApiSecretParams) (db.ApiSecret, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.ApiSecret), args.Error(1)
}
func (m *deliveryMockQuerier) UpdateDeliveryAttemptStatus(ctx context.Context, arg db.UpdateDeliveryAttemptStatusParams) (db.DeliveryAttempt, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.DeliveryAttempt), args.Error(1)
}
func (m *deliveryMockQuerier) UpdateEventDeliveryStatus(ctx context.Context, arg db.UpdateEventDeliveryStatusParams) (db.Event, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.Event), args.Error(1)
}
func (m *deliveryMockQuerier) UpdateSubscriber(ctx context.Context, arg db.UpdateSubscriberParams) (db.Subscriber, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.Subscriber), args.Error(1)
}
func (m *deliveryMockQuerier) UpdateSubscription(ctx context.Context, arg db.UpdateSubscriptionParams) (db.Subscription, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.Subscription), args.Error(1)
}
func (m *deliveryMockQuerier) UpsertLogConfig(ctx context.Context, arg db.UpsertLogConfigParams) (db.LogConfig, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.LogConfig), args.Error(1)
}
func (m *deliveryMockQuerier) UpsertSubscriber(ctx context.Context, arg db.UpsertSubscriberParams) (db.Subscriber, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(db.Subscriber), args.Error(1)
}

func newTestUUID() pgtype.UUID {
	return pgtype.UUID{Bytes: uuid.Must(uuid.NewV7()), Valid: true}
}

func newTestTimestamp() pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}
}

func newTestEvent(opts ...func(*db.Event)) db.Event {
	e := db.Event{
		ID:              newTestUUID(),
		Subject:         "test.subject",
		Timestamp:       newTestTimestamp(),
		Data:            json.RawMessage(`{"key":"value"}`),
		RetryCount:      0,
		DeliveryStatus:  "pending",
		StatusUpdatedAt: newTestTimestamp(),
	}
	for _, opt := range opts {
		opt(&e)
	}
	return e
}

func newTestSubscriber(opts ...func(*db.Subscriber)) db.Subscriber {
	s := db.Subscriber{
		ID:          newTestUUID(),
		Name:        "test-subscriber",
		EndpointUrl: "https://example.com/webhook",
		AuthSecret:  "test-auth-secret",
		MaxParallel: 1,
		CreatedAt:   newTestTimestamp(),
		UpdatedAt:   newTestTimestamp(),
	}
	for _, opt := range opts {
		opt(&s)
	}
	return s
}

func newTestSubscription(opts ...func(*db.Subscription)) db.Subscription {
	s := db.Subscription{
		ID:             newTestUUID(),
		SubscriberID:   newTestUUID(),
		SubjectPattern: "test.*",
		CreatedAt:      newTestTimestamp(),
		UpdatedAt:      newTestTimestamp(),
	}
	for _, opt := range opts {
		opt(&s)
	}
	return s
}

func newDeliveryTestApp(mockDB *deliveryMockQuerier) *Application {
	return &Application{
		Config: config.AppConfig{
			Port:              8005,
			AdminSecret:       "test-admin-secret",
			MaxParallel:       1,
			MaxRetries:        3,
			MaxBackoffSeconds: 300,
			DeliveryQueueSize: 100,
			DeliveryWorkers:   2,
			DeliveryChanSize:  100,
		},
		DB:                mockDB,
		DeliveryChan:      make(chan db.Event, 100),
		EventBus:          NewEventBus(),
		Sessions:          NewSessionStore(),
		SubscriptionCache: NewSubscriptionCache(mockDB),
	}
}

// --- deliverToSubscriber tests ---

func TestDeliverToSubscriber_CorrectHeadersAndBody(t *testing.T) {
	var receivedHeaders http.Header
	var receivedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		body := make([]byte, r.ContentLength)
		r.Body.Read(body)
		receivedBody = body
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	mockDB := new(deliveryMockQuerier)
	app := newDeliveryTestApp(mockDB)

	event := newTestEvent()
	subscriber := newTestSubscriber(func(s *db.Subscriber) {
		s.EndpointUrl = server.URL
		s.AuthSecret = "webhook-secret"
	})

	mockDB.On("InsertDeliveryAttempt", mock.Anything, mock.AnythingOfType("db.InsertDeliveryAttemptParams")).
		Return(db.DeliveryAttempt{}, nil)

	result := deliverToSubscriber(context.Background(), app, event, subscriber, 0, slog.Default())

	assert.True(t, result)
	assert.Equal(t, "application/json", receivedHeaders.Get("Content-Type"))
	assert.Equal(t, "webhook-secret", receivedHeaders.Get("X-Slurpee-Secret"))
	assert.Equal(t, UuidToString(event.ID), receivedHeaders.Get("X-Event-ID"))
	assert.Equal(t, event.Subject, receivedHeaders.Get("X-Event-Subject"))
	assert.JSONEq(t, string(event.Data), string(receivedBody))
	mockDB.AssertExpectations(t)
}

func TestDeliverToSubscriber_Returns_True_For_2xx(t *testing.T) {
	statusCodes := []int{200, 201, 202, 204, 299}

	for _, code := range statusCodes {
		t.Run(http.StatusText(code), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(code)
			}))
			defer server.Close()

			mockDB := new(deliveryMockQuerier)
			app := newDeliveryTestApp(mockDB)
			event := newTestEvent()
			subscriber := newTestSubscriber(func(s *db.Subscriber) {
				s.EndpointUrl = server.URL
			})

			mockDB.On("InsertDeliveryAttempt", mock.Anything, mock.AnythingOfType("db.InsertDeliveryAttemptParams")).
				Return(db.DeliveryAttempt{}, nil)

			result := deliverToSubscriber(context.Background(), app, event, subscriber, 0, slog.Default())
			assert.True(t, result)
			mockDB.AssertExpectations(t)
		})
	}
}

func TestDeliverToSubscriber_Returns_False_For_Non2xx(t *testing.T) {
	statusCodes := []int{400, 401, 403, 404, 500, 502, 503}

	for _, code := range statusCodes {
		t.Run(http.StatusText(code), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(code)
			}))
			defer server.Close()

			mockDB := new(deliveryMockQuerier)
			app := newDeliveryTestApp(mockDB)
			event := newTestEvent()
			subscriber := newTestSubscriber(func(s *db.Subscriber) {
				s.EndpointUrl = server.URL
			})

			mockDB.On("InsertDeliveryAttempt", mock.Anything, mock.AnythingOfType("db.InsertDeliveryAttemptParams")).
				Return(db.DeliveryAttempt{}, nil)

			result := deliverToSubscriber(context.Background(), app, event, subscriber, 0, slog.Default())
			assert.False(t, result)
			mockDB.AssertExpectations(t)
		})
	}
}

func TestDeliverToSubscriber_RecordsDeliveryAttempt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Custom", "response-header")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	mockDB := new(deliveryMockQuerier)
	app := newDeliveryTestApp(mockDB)

	event := newTestEvent()
	subscriber := newTestSubscriber(func(s *db.Subscriber) {
		s.EndpointUrl = server.URL
	})

	var capturedParams db.InsertDeliveryAttemptParams
	mockDB.On("InsertDeliveryAttempt", mock.Anything, mock.AnythingOfType("db.InsertDeliveryAttemptParams")).
		Run(func(args mock.Arguments) {
			capturedParams = args.Get(1).(db.InsertDeliveryAttemptParams)
		}).
		Return(db.DeliveryAttempt{}, nil)

	deliverToSubscriber(context.Background(), app, event, subscriber, 0, slog.Default())

	assert.Equal(t, event.ID, capturedParams.EventID)
	assert.Equal(t, subscriber.ID, capturedParams.SubscriberID)
	assert.Equal(t, server.URL, capturedParams.EndpointUrl)
	assert.True(t, capturedParams.AttemptedAt.Valid)
	assert.Equal(t, "succeeded", capturedParams.Status)
	assert.Equal(t, int32(200), capturedParams.ResponseStatusCode.Int32)
	assert.True(t, capturedParams.ResponseStatusCode.Valid)
	assert.NotEmpty(t, capturedParams.RequestHeaders)
	assert.NotEmpty(t, capturedParams.ResponseHeaders)
	assert.Equal(t, `{"ok":true}`, capturedParams.ResponseBody)
	mockDB.AssertExpectations(t)
}

func TestDeliverToSubscriber_RecordsFailedAttempt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("server error"))
	}))
	defer server.Close()

	mockDB := new(deliveryMockQuerier)
	app := newDeliveryTestApp(mockDB)

	event := newTestEvent()
	subscriber := newTestSubscriber(func(s *db.Subscriber) {
		s.EndpointUrl = server.URL
	})

	var capturedParams db.InsertDeliveryAttemptParams
	mockDB.On("InsertDeliveryAttempt", mock.Anything, mock.AnythingOfType("db.InsertDeliveryAttemptParams")).
		Run(func(args mock.Arguments) {
			capturedParams = args.Get(1).(db.InsertDeliveryAttemptParams)
		}).
		Return(db.DeliveryAttempt{}, nil)

	deliverToSubscriber(context.Background(), app, event, subscriber, 0, slog.Default())

	assert.Equal(t, "failed", capturedParams.Status)
	assert.Equal(t, int32(500), capturedParams.ResponseStatusCode.Int32)
	mockDB.AssertExpectations(t)
}

func TestDeliverToSubscriber_SendsEventDataAsBody(t *testing.T) {
	eventData := json.RawMessage(`{"user_id":123,"action":"signup","nested":{"key":"val"}}`)

	var receivedBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		r.Body.Read(body)
		receivedBody = string(body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	mockDB := new(deliveryMockQuerier)
	app := newDeliveryTestApp(mockDB)

	event := newTestEvent(func(e *db.Event) {
		e.Data = eventData
	})
	subscriber := newTestSubscriber(func(s *db.Subscriber) {
		s.EndpointUrl = server.URL
	})

	mockDB.On("InsertDeliveryAttempt", mock.Anything, mock.AnythingOfType("db.InsertDeliveryAttemptParams")).
		Return(db.DeliveryAttempt{}, nil)

	deliverToSubscriber(context.Background(), app, event, subscriber, 0, slog.Default())

	assert.JSONEq(t, string(eventData), receivedBody)
}

// --- dispatchEvent tests ---

func TestDispatchEvent_FindsMatchingSubscriptionsAndEnqueuesTasks(t *testing.T) {
	mockDB := new(deliveryMockQuerier)
	app := newDeliveryTestApp(mockDB)

	event := newTestEvent(func(e *db.Event) {
		e.Subject = "orders.created"
	})

	subscriber := newTestSubscriber()
	subscription := newTestSubscription(func(s *db.Subscription) {
		s.SubscriberID = subscriber.ID
		s.SubjectPattern = "orders.*"
	})

	mockDB.On("ListSubscribers", mock.Anything).
		Return([]db.Subscriber{subscriber}, nil)
	mockDB.On("ListAllSubscriptions", mock.Anything).
		Return([]db.Subscription{subscription}, nil)

	var inflightWg sync.WaitGroup
	taskQueue := make(chan deliveryTask, 100)
	registry := newEventRegistry()

	dispatchEvent(app, event, &inflightWg, taskQueue, registry)

	assert.Equal(t, 1, len(taskQueue))

	task := <-taskQueue
	assert.Equal(t, event.ID, task.event.ID)
	assert.Equal(t, subscription.ID, task.subscription.ID)
	assert.Equal(t, subscriber.ID, task.subscriber.ID)
	assert.Equal(t, 0, task.attemptNum)
	assert.Equal(t, app.Config.MaxRetries, task.maxRetries)
	assert.NotNil(t, task.tracker)
	mockDB.AssertExpectations(t)
}

func TestDispatchEvent_SkipsSubscriptionsWhoseFiltersDontMatch(t *testing.T) {
	mockDB := new(deliveryMockQuerier)
	app := newDeliveryTestApp(mockDB)

	event := newTestEvent(func(e *db.Event) {
		e.Subject = "orders.created"
		e.Data = []byte(`{"type":"order","action":"create"}`)
	})

	subscriber := newTestSubscriber()

	matchingSub := newTestSubscription(func(s *db.Subscription) {
		s.SubscriberID = subscriber.ID
		s.SubjectPattern = "orders.*"
		s.Filter = []byte(`{"type":"order"}`)
	})
	nonMatchingSub := newTestSubscription(func(s *db.Subscription) {
		s.SubscriberID = subscriber.ID
		s.SubjectPattern = "orders.*"
		s.Filter = []byte(`{"type":"payment"}`)
	})

	mockDB.On("ListSubscribers", mock.Anything).
		Return([]db.Subscriber{subscriber}, nil)
	mockDB.On("ListAllSubscriptions", mock.Anything).
		Return([]db.Subscription{matchingSub, nonMatchingSub}, nil)

	var inflightWg sync.WaitGroup
	taskQueue := make(chan deliveryTask, 100)
	registry := newEventRegistry()

	dispatchEvent(app, event, &inflightWg, taskQueue, registry)

	assert.Equal(t, 1, len(taskQueue))

	task := <-taskQueue
	assert.Equal(t, matchingSub.ID, task.subscription.ID)
	mockDB.AssertExpectations(t)
}

func TestDispatchEvent_NoMatchingSubscriptions_MarksDelivered(t *testing.T) {
	mockDB := new(deliveryMockQuerier)
	app := newDeliveryTestApp(mockDB)

	event := newTestEvent(func(e *db.Event) {
		e.Subject = "orders.created"
	})

	mockDB.On("ListSubscribers", mock.Anything).
		Return([]db.Subscriber{}, nil)
	mockDB.On("ListAllSubscriptions", mock.Anything).
		Return([]db.Subscription{}, nil)
	mockDB.On("UpdateEventDeliveryStatus", mock.Anything, mock.AnythingOfType("db.UpdateEventDeliveryStatusParams")).
		Return(db.Event{}, nil)

	var inflightWg sync.WaitGroup
	taskQueue := make(chan deliveryTask, 100)
	registry := newEventRegistry()

	dispatchEvent(app, event, &inflightWg, taskQueue, registry)

	assert.Equal(t, 0, len(taskQueue))
	mockDB.AssertExpectations(t)
}

func TestDispatchEvent_SubscriptionDbError_MarksFailed(t *testing.T) {
	mockDB := new(deliveryMockQuerier)
	app := newDeliveryTestApp(mockDB)

	event := newTestEvent(func(e *db.Event) {
		e.Subject = "orders.created"
	})

	mockDB.On("ListSubscribers", mock.Anything).
		Return([]db.Subscriber(nil), assert.AnError)
	mockDB.On("UpdateEventDeliveryStatus", mock.Anything, mock.AnythingOfType("db.UpdateEventDeliveryStatusParams")).
		Return(db.Event{}, nil)

	var inflightWg sync.WaitGroup
	taskQueue := make(chan deliveryTask, 100)
	registry := newEventRegistry()

	dispatchEvent(app, event, &inflightWg, taskQueue, registry)

	assert.Equal(t, 0, len(taskQueue))
	mockDB.AssertExpectations(t)
}

func TestDispatchEvent_UsesSubscriptionMaxRetries(t *testing.T) {
	mockDB := new(deliveryMockQuerier)
	app := newDeliveryTestApp(mockDB)

	event := newTestEvent(func(e *db.Event) {
		e.Subject = "orders.created"
	})

	subscriber := newTestSubscriber()
	subscription := newTestSubscription(func(s *db.Subscription) {
		s.SubscriberID = subscriber.ID
		s.SubjectPattern = "orders.*"
		s.MaxRetries = pgtype.Int4{Int32: 10, Valid: true}
	})

	mockDB.On("ListSubscribers", mock.Anything).
		Return([]db.Subscriber{subscriber}, nil)
	mockDB.On("ListAllSubscriptions", mock.Anything).
		Return([]db.Subscription{subscription}, nil)

	var inflightWg sync.WaitGroup
	taskQueue := make(chan deliveryTask, 100)
	registry := newEventRegistry()

	dispatchEvent(app, event, &inflightWg, taskQueue, registry)

	task := <-taskQueue
	assert.Equal(t, 10, task.maxRetries)
	mockDB.AssertExpectations(t)
}

func TestDispatchEvent_MultipleSubscribers(t *testing.T) {
	mockDB := new(deliveryMockQuerier)
	app := newDeliveryTestApp(mockDB)

	event := newTestEvent(func(e *db.Event) {
		e.Subject = "orders.created"
		e.Data = []byte(`{"type":"order"}`)
	})

	sub1 := newTestSubscriber(func(s *db.Subscriber) {
		s.Name = "subscriber-1"
	})
	sub2 := newTestSubscriber(func(s *db.Subscriber) {
		s.Name = "subscriber-2"
	})

	subscription1 := newTestSubscription(func(s *db.Subscription) {
		s.SubscriberID = sub1.ID
		s.SubjectPattern = "orders.*"
	})
	subscription2 := newTestSubscription(func(s *db.Subscription) {
		s.SubscriberID = sub2.ID
		s.SubjectPattern = "orders.*"
	})

	mockDB.On("ListSubscribers", mock.Anything).
		Return([]db.Subscriber{sub1, sub2}, nil)
	mockDB.On("ListAllSubscriptions", mock.Anything).
		Return([]db.Subscription{subscription1, subscription2}, nil)

	var inflightWg sync.WaitGroup
	taskQueue := make(chan deliveryTask, 100)
	registry := newEventRegistry()

	dispatchEvent(app, event, &inflightWg, taskQueue, registry)

	assert.Equal(t, 2, len(taskQueue))

	subscriberIDs := make(map[[16]byte]bool)
	for i := 0; i < 2; i++ {
		task := <-taskQueue
		subscriberIDs[task.subscriber.ID.Bytes] = true
	}
	assert.True(t, subscriberIDs[sub1.ID.Bytes])
	assert.True(t, subscriberIDs[sub2.ID.Bytes])
	mockDB.AssertExpectations(t)
}

// --- processDeliveryTask tests ---

func TestProcessDeliveryTask_RetriesOnFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	mockDB := new(deliveryMockQuerier)
	app := newDeliveryTestApp(mockDB)
	app.Config.MaxBackoffSeconds = 1

	event := newTestEvent()
	subscriber := newTestSubscriber(func(s *db.Subscriber) {
		s.EndpointUrl = server.URL
		s.MaxParallel = 1
	})
	subscription := newTestSubscription(func(s *db.Subscription) {
		s.SubscriberID = subscriber.ID
	})

	tracker := &eventTracker{
		event:    event,
		expected: 1,
		results:  make(map[[16]byte]deliveryResult),
		logger:   slog.Default(),
	}

	task := deliveryTask{
		event:        event,
		subscription: subscription,
		subscriber:   subscriber,
		attemptNum:   0,
		maxRetries:   3,
		tracker:      tracker,
	}

	mockDB.On("InsertDeliveryAttempt", mock.Anything, mock.AnythingOfType("db.InsertDeliveryAttemptParams")).
		Return(db.DeliveryAttempt{}, nil)
	mockDB.On("UpdateEventDeliveryStatus", mock.Anything, mock.AnythingOfType("db.UpdateEventDeliveryStatusParams")).
		Return(db.Event{}, nil)

	shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
	defer shutdownCancel()

	getSemaphore := func(id [16]byte, maxParallel int32) chan struct{} {
		return make(chan struct{}, maxParallel)
	}

	var inflightWg sync.WaitGroup
	taskQueue := make(chan deliveryTask, 100)
	registry := newEventRegistry()
	registry.register(event.ID.Bytes, tracker)

	inflightWg.Add(1)
	processDeliveryTask(shutdownCtx, app, task, getSemaphore, &inflightWg, taskQueue, registry)

	// Wait for time.AfterFunc to fire (backoff for attempt 0 = 1s, capped at 1s)
	time.Sleep(2 * time.Second)

	assert.Equal(t, 1, len(taskQueue), "Expected retry task to be enqueued")

	retryTask := <-taskQueue
	assert.Equal(t, 1, retryTask.attemptNum, "Retry should increment attempt number")
	assert.Equal(t, event.ID, retryTask.event.ID)
}

func TestProcessDeliveryTask_MaxRetriesExhausted(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	mockDB := new(deliveryMockQuerier)
	app := newDeliveryTestApp(mockDB)

	event := newTestEvent()
	subscriber := newTestSubscriber(func(s *db.Subscriber) {
		s.EndpointUrl = server.URL
		s.MaxParallel = 1
	})
	subscription := newTestSubscription(func(s *db.Subscription) {
		s.SubscriberID = subscriber.ID
	})

	tracker := &eventTracker{
		event:    event,
		expected: 1,
		results:  make(map[[16]byte]deliveryResult),
		logger:   slog.Default(),
	}

	task := deliveryTask{
		event:        event,
		subscription: subscription,
		subscriber:   subscriber,
		attemptNum:   3,
		maxRetries:   3,
		tracker:      tracker,
	}

	mockDB.On("InsertDeliveryAttempt", mock.Anything, mock.AnythingOfType("db.InsertDeliveryAttemptParams")).
		Return(db.DeliveryAttempt{}, nil)
	mockDB.On("UpdateEventDeliveryStatus", mock.Anything, mock.AnythingOfType("db.UpdateEventDeliveryStatusParams")).
		Return(db.Event{}, nil)

	shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
	defer shutdownCancel()

	getSemaphore := func(id [16]byte, maxParallel int32) chan struct{} {
		return make(chan struct{}, maxParallel)
	}

	var inflightWg sync.WaitGroup
	taskQueue := make(chan deliveryTask, 100)
	registry := newEventRegistry()
	registry.register(event.ID.Bytes, tracker)

	inflightWg.Add(1)
	processDeliveryTask(shutdownCtx, app, task, getSemaphore, &inflightWg, taskQueue, registry)

	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, 0, len(taskQueue), "No retry should be enqueued when max retries exhausted")

	tracker.mu.Lock()
	assert.Equal(t, 1, len(tracker.results))
	for _, r := range tracker.results {
		assert.False(t, r.succeeded)
		assert.True(t, r.exhausted)
	}
	tracker.mu.Unlock()
}

func TestProcessDeliveryTask_SuccessRecordsResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	mockDB := new(deliveryMockQuerier)
	app := newDeliveryTestApp(mockDB)

	event := newTestEvent()
	subscriber := newTestSubscriber(func(s *db.Subscriber) {
		s.EndpointUrl = server.URL
		s.MaxParallel = 1
	})
	subscription := newTestSubscription(func(s *db.Subscription) {
		s.SubscriberID = subscriber.ID
	})

	tracker := &eventTracker{
		event:    event,
		expected: 1,
		results:  make(map[[16]byte]deliveryResult),
		logger:   slog.Default(),
	}

	task := deliveryTask{
		event:        event,
		subscription: subscription,
		subscriber:   subscriber,
		attemptNum:   0,
		maxRetries:   3,
		tracker:      tracker,
	}

	mockDB.On("InsertDeliveryAttempt", mock.Anything, mock.AnythingOfType("db.InsertDeliveryAttemptParams")).
		Return(db.DeliveryAttempt{}, nil)
	mockDB.On("UpdateEventDeliveryStatus", mock.Anything, mock.AnythingOfType("db.UpdateEventDeliveryStatusParams")).
		Return(db.Event{}, nil)

	shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
	defer shutdownCancel()

	getSemaphore := func(id [16]byte, maxParallel int32) chan struct{} {
		return make(chan struct{}, maxParallel)
	}

	var inflightWg sync.WaitGroup
	taskQueue := make(chan deliveryTask, 100)
	registry := newEventRegistry()
	registry.register(event.ID.Bytes, tracker)

	inflightWg.Add(1)
	processDeliveryTask(shutdownCtx, app, task, getSemaphore, &inflightWg, taskQueue, registry)

	tracker.mu.Lock()
	assert.Equal(t, 1, len(tracker.results))
	for _, r := range tracker.results {
		assert.True(t, r.succeeded)
		assert.False(t, r.exhausted)
	}
	tracker.mu.Unlock()

	assert.Equal(t, 0, len(taskQueue))
}

// --- finalizeEvent tests ---

func TestFinalizeEvent_AllSucceeded_MarksDelivered(t *testing.T) {
	mockDB := new(deliveryMockQuerier)
	app := newDeliveryTestApp(mockDB)

	event := newTestEvent()
	sub1ID := newTestUUID()
	sub2ID := newTestUUID()

	tracker := &eventTracker{
		event:    event,
		expected: 2,
		results: map[[16]byte]deliveryResult{
			sub1ID.Bytes: {subscriptionID: sub1ID, succeeded: true},
			sub2ID.Bytes: {subscriptionID: sub2ID, succeeded: true},
		},
		logger: slog.Default(),
	}

	var capturedParams db.UpdateEventDeliveryStatusParams
	mockDB.On("UpdateEventDeliveryStatus", mock.Anything, mock.AnythingOfType("db.UpdateEventDeliveryStatusParams")).
		Run(func(args mock.Arguments) {
			capturedParams = args.Get(1).(db.UpdateEventDeliveryStatusParams)
		}).
		Return(db.Event{}, nil)

	registry := newEventRegistry()
	registry.register(event.ID.Bytes, tracker)

	finalizeEvent(context.Background(), app, tracker, registry)

	assert.Equal(t, "delivered", capturedParams.DeliveryStatus)
	mockDB.AssertExpectations(t)
}

func TestFinalizeEvent_AnyFailed_MarksFailed(t *testing.T) {
	mockDB := new(deliveryMockQuerier)
	app := newDeliveryTestApp(mockDB)

	event := newTestEvent()
	sub1ID := newTestUUID()
	sub2ID := newTestUUID()

	tracker := &eventTracker{
		event:    event,
		expected: 2,
		results: map[[16]byte]deliveryResult{
			sub1ID.Bytes: {subscriptionID: sub1ID, succeeded: true},
			sub2ID.Bytes: {subscriptionID: sub2ID, succeeded: false, exhausted: true},
		},
		logger: slog.Default(),
	}

	var capturedParams db.UpdateEventDeliveryStatusParams
	mockDB.On("UpdateEventDeliveryStatus", mock.Anything, mock.AnythingOfType("db.UpdateEventDeliveryStatusParams")).
		Run(func(args mock.Arguments) {
			capturedParams = args.Get(1).(db.UpdateEventDeliveryStatusParams)
		}).
		Return(db.Event{}, nil)

	registry := newEventRegistry()
	registry.register(event.ID.Bytes, tracker)

	finalizeEvent(context.Background(), app, tracker, registry)

	assert.Equal(t, "failed", capturedParams.DeliveryStatus)
	mockDB.AssertExpectations(t)
}

func TestFinalizeEvent_AllFailed_MarksFailed(t *testing.T) {
	mockDB := new(deliveryMockQuerier)
	app := newDeliveryTestApp(mockDB)

	event := newTestEvent()
	sub1ID := newTestUUID()

	tracker := &eventTracker{
		event:    event,
		expected: 1,
		results: map[[16]byte]deliveryResult{
			sub1ID.Bytes: {subscriptionID: sub1ID, succeeded: false, exhausted: true},
		},
		logger: slog.Default(),
	}

	var capturedParams db.UpdateEventDeliveryStatusParams
	mockDB.On("UpdateEventDeliveryStatus", mock.Anything, mock.AnythingOfType("db.UpdateEventDeliveryStatusParams")).
		Run(func(args mock.Arguments) {
			capturedParams = args.Get(1).(db.UpdateEventDeliveryStatusParams)
		}).
		Return(db.Event{}, nil)

	registry := newEventRegistry()
	registry.register(event.ID.Bytes, tracker)

	finalizeEvent(context.Background(), app, tracker, registry)

	assert.Equal(t, "failed", capturedParams.DeliveryStatus)
	mockDB.AssertExpectations(t)
}

// --- eventTracker tests ---

func TestEventTracker_RecordReturnsTrueWhenAllResultsCollected(t *testing.T) {
	event := newTestEvent()
	sub1ID := newTestUUID()
	sub2ID := newTestUUID()

	tracker := &eventTracker{
		event:    event,
		expected: 2,
		results:  make(map[[16]byte]deliveryResult),
		logger:   slog.Default(),
	}

	complete := tracker.record(deliveryResult{subscriptionID: sub1ID, succeeded: true})
	assert.False(t, complete)

	complete = tracker.record(deliveryResult{subscriptionID: sub2ID, succeeded: true})
	assert.True(t, complete)
}
