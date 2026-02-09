package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/sweater-ventures/slurpee/db"
	"github.com/sweater-ventures/slurpee/testutil"
)

// --- POST /api/subscribers tests ---

func TestCreateSubscriber_MissingAdminSecret(t *testing.T) {
	mockDB := new(testutil.MockQuerier)
	slurpee := testutil.NewTestApp(mockDB)

	req := testutil.NewJSONRequest(t, http.MethodPost, "/subscribers", map[string]any{
		"name":         "test-sub",
		"endpoint_url": "https://example.com/webhook",
		"auth_secret":  "secret",
		"subscriptions": []map[string]any{
			{"subject_pattern": "events.*"},
		},
	})

	rec := callHandler(t, slurpee, createSubscriberHandler, req)
	testutil.AssertJSONError(t, rec, http.StatusUnauthorized, "Invalid or missing admin secret")
}

func TestCreateSubscriber_WrongAdminSecret(t *testing.T) {
	mockDB := new(testutil.MockQuerier)
	slurpee := testutil.NewTestApp(mockDB)

	req := testutil.NewJSONRequest(t, http.MethodPost, "/subscribers", map[string]any{
		"name":         "test-sub",
		"endpoint_url": "https://example.com/webhook",
		"auth_secret":  "secret",
		"subscriptions": []map[string]any{
			{"subject_pattern": "events.*"},
		},
	})
	testutil.WithAdminSecret(req, "wrong-admin-secret")

	rec := callHandler(t, slurpee, createSubscriberHandler, req)
	testutil.AssertJSONError(t, rec, http.StatusUnauthorized, "Invalid or missing admin secret")
}

func TestCreateSubscriber_MissingName(t *testing.T) {
	mockDB := new(testutil.MockQuerier)
	slurpee := testutil.NewTestApp(mockDB)

	req := testutil.NewJSONRequest(t, http.MethodPost, "/subscribers", map[string]any{
		"endpoint_url": "https://example.com/webhook",
		"auth_secret":  "secret",
		"subscriptions": []map[string]any{
			{"subject_pattern": "events.*"},
		},
	})
	testutil.WithAdminSecret(req, "test-admin-secret")

	rec := callHandler(t, slurpee, createSubscriberHandler, req)
	testutil.AssertJSONError(t, rec, http.StatusBadRequest, "name is required")
}

func TestCreateSubscriber_MissingEndpointURL(t *testing.T) {
	mockDB := new(testutil.MockQuerier)
	slurpee := testutil.NewTestApp(mockDB)

	req := testutil.NewJSONRequest(t, http.MethodPost, "/subscribers", map[string]any{
		"name":        "test-sub",
		"auth_secret": "secret",
		"subscriptions": []map[string]any{
			{"subject_pattern": "events.*"},
		},
	})
	testutil.WithAdminSecret(req, "test-admin-secret")

	rec := callHandler(t, slurpee, createSubscriberHandler, req)
	testutil.AssertJSONError(t, rec, http.StatusBadRequest, "endpoint_url is required")
}

func TestCreateSubscriber_MissingAuthSecret(t *testing.T) {
	mockDB := new(testutil.MockQuerier)
	slurpee := testutil.NewTestApp(mockDB)

	req := testutil.NewJSONRequest(t, http.MethodPost, "/subscribers", map[string]any{
		"name":         "test-sub",
		"endpoint_url": "https://example.com/webhook",
		"subscriptions": []map[string]any{
			{"subject_pattern": "events.*"},
		},
	})
	testutil.WithAdminSecret(req, "test-admin-secret")

	rec := callHandler(t, slurpee, createSubscriberHandler, req)
	testutil.AssertJSONError(t, rec, http.StatusBadRequest, "auth_secret is required")
}

func TestCreateSubscriber_MissingSubscriptions(t *testing.T) {
	mockDB := new(testutil.MockQuerier)
	slurpee := testutil.NewTestApp(mockDB)

	req := testutil.NewJSONRequest(t, http.MethodPost, "/subscribers", map[string]any{
		"name":         "test-sub",
		"endpoint_url": "https://example.com/webhook",
		"auth_secret":  "secret",
	})
	testutil.WithAdminSecret(req, "test-admin-secret")

	rec := callHandler(t, slurpee, createSubscriberHandler, req)
	testutil.AssertJSONError(t, rec, http.StatusBadRequest, "subscriptions is required")
}

func TestCreateSubscriber_SubscriptionMissingSubjectPattern(t *testing.T) {
	mockDB := new(testutil.MockQuerier)
	slurpee := testutil.NewTestApp(mockDB)

	req := testutil.NewJSONRequest(t, http.MethodPost, "/subscribers", map[string]any{
		"name":         "test-sub",
		"endpoint_url": "https://example.com/webhook",
		"auth_secret":  "secret",
		"subscriptions": []map[string]any{
			{"filter": map[string]any{"type": "order"}},
		},
	})
	testutil.WithAdminSecret(req, "test-admin-secret")

	rec := callHandler(t, slurpee, createSubscriberHandler, req)
	testutil.AssertJSONError(t, rec, http.StatusBadRequest, "subject_pattern is required for each subscription")
}

func TestCreateSubscriber_InvalidJSONBody(t *testing.T) {
	mockDB := new(testutil.MockQuerier)
	slurpee := testutil.NewTestApp(mockDB)

	req := httptest.NewRequest(http.MethodPost, "/subscribers", nil)
	req.Header.Set("Content-Type", "application/json")
	testutil.WithAdminSecret(req, "test-admin-secret")

	rec := callHandler(t, slurpee, createSubscriberHandler, req)
	testutil.AssertJSONError(t, rec, http.StatusBadRequest, "Invalid request body")
}

func TestCreateSubscriber_Success(t *testing.T) {
	mockDB := new(testutil.MockQuerier)
	slurpee := testutil.NewTestApp(mockDB)

	subscriber := testutil.NewSubscriber(func(s *db.Subscriber) {
		s.Name = "order-service"
		s.EndpointUrl = "https://orders.example.com/webhook"
		s.AuthSecret = "webhook-secret"
		s.MaxParallel = 5
	})

	mockDB.On("UpsertSubscriber", mock.Anything, mock.AnythingOfType("db.UpsertSubscriberParams")).
		Return(subscriber, nil)
	mockDB.On("ListSubscriptionsForSubscriber", mock.Anything, subscriber.ID).
		Return([]db.Subscription{}, nil)

	subscription := testutil.NewSubscription(func(s *db.Subscription) {
		s.SubscriberID = subscriber.ID
		s.SubjectPattern = "orders.*"
	})
	mockDB.On("CreateSubscription", mock.Anything, mock.AnythingOfType("db.CreateSubscriptionParams")).
		Return(subscription, nil)

	maxRetries := int32(3)
	req := testutil.NewJSONRequest(t, http.MethodPost, "/subscribers", map[string]any{
		"name":         "order-service",
		"endpoint_url": "https://orders.example.com/webhook",
		"auth_secret":  "webhook-secret",
		"max_parallel": 5,
		"subscriptions": []map[string]any{
			{"subject_pattern": "orders.*", "max_retries": maxRetries},
		},
	})
	testutil.WithAdminSecret(req, "test-admin-secret")

	rec := callHandler(t, slurpee, createSubscriberHandler, req)

	var resp SubscriberResponse
	testutil.AssertJSONResponse(t, rec, http.StatusOK, &resp)
	assert.Equal(t, "order-service", resp.Name)
	assert.Equal(t, "https://orders.example.com/webhook", resp.EndpointURL)
	assert.Equal(t, int32(5), resp.MaxParallel)
	assert.Len(t, resp.Subscriptions, 1)
	assert.Equal(t, "orders.*", resp.Subscriptions[0].SubjectPattern)
	assert.NotEmpty(t, resp.ID)
	assert.NotZero(t, resp.CreatedAt)
	assert.NotZero(t, resp.UpdatedAt)
	mockDB.AssertExpectations(t)
}

func TestCreateSubscriber_WithMultipleSubscriptions(t *testing.T) {
	mockDB := new(testutil.MockQuerier)
	slurpee := testutil.NewTestApp(mockDB)

	subscriber := testutil.NewSubscriber(func(s *db.Subscriber) {
		s.Name = "multi-service"
		s.EndpointUrl = "https://multi.example.com/webhook"
		s.AuthSecret = "webhook-secret"
	})

	mockDB.On("UpsertSubscriber", mock.Anything, mock.AnythingOfType("db.UpsertSubscriberParams")).
		Return(subscriber, nil)
	mockDB.On("ListSubscriptionsForSubscriber", mock.Anything, subscriber.ID).
		Return([]db.Subscription{}, nil)

	sub1 := testutil.NewSubscription(func(s *db.Subscription) {
		s.SubscriberID = subscriber.ID
		s.SubjectPattern = "orders.*"
	})
	sub2 := testutil.NewSubscription(func(s *db.Subscription) {
		s.SubscriberID = subscriber.ID
		s.SubjectPattern = "users.*"
	})

	// CreateSubscription is called once per subscription
	mockDB.On("CreateSubscription", mock.Anything, mock.AnythingOfType("db.CreateSubscriptionParams")).
		Return(sub1, nil).Once()
	mockDB.On("CreateSubscription", mock.Anything, mock.AnythingOfType("db.CreateSubscriptionParams")).
		Return(sub2, nil).Once()

	req := testutil.NewJSONRequest(t, http.MethodPost, "/subscribers", map[string]any{
		"name":         "multi-service",
		"endpoint_url": "https://multi.example.com/webhook",
		"auth_secret":  "webhook-secret",
		"subscriptions": []map[string]any{
			{"subject_pattern": "orders.*"},
			{"subject_pattern": "users.*"},
		},
	})
	testutil.WithAdminSecret(req, "test-admin-secret")

	rec := callHandler(t, slurpee, createSubscriberHandler, req)

	var resp SubscriberResponse
	testutil.AssertJSONResponse(t, rec, http.StatusOK, &resp)
	assert.Equal(t, "multi-service", resp.Name)
	assert.Len(t, resp.Subscriptions, 2)
	mockDB.AssertExpectations(t)
}

func TestCreateSubscriber_UpdateExistingSubscriptions(t *testing.T) {
	mockDB := new(testutil.MockQuerier)
	slurpee := testutil.NewTestApp(mockDB)

	subscriber := testutil.NewSubscriber(func(s *db.Subscriber) {
		s.Name = "order-service"
		s.EndpointUrl = "https://orders.example.com/webhook"
		s.AuthSecret = "webhook-secret"
	})

	existingSub := testutil.NewSubscription(func(s *db.Subscription) {
		s.SubscriberID = subscriber.ID
		s.SubjectPattern = "orders.*"
		s.MaxRetries = pgtype.Int4{Int32: 3, Valid: true}
	})

	mockDB.On("UpsertSubscriber", mock.Anything, mock.AnythingOfType("db.UpsertSubscriberParams")).
		Return(subscriber, nil)
	mockDB.On("ListSubscriptionsForSubscriber", mock.Anything, subscriber.ID).
		Return([]db.Subscription{existingSub}, nil)

	updatedSub := existingSub
	updatedSub.MaxRetries = pgtype.Int4{Int32: 5, Valid: true}
	updatedSub.Filter = []byte(`{"type":"urgent"}`)
	mockDB.On("UpdateSubscription", mock.Anything, db.UpdateSubscriptionParams{
		ID:         existingSub.ID,
		Filter:     []byte(`{"type":"urgent"}`),
		MaxRetries: pgtype.Int4{Int32: 5, Valid: true},
	}).Return(updatedSub, nil)

	maxRetries := int32(5)
	req := testutil.NewJSONRequest(t, http.MethodPost, "/subscribers", map[string]any{
		"name":         "order-service",
		"endpoint_url": "https://orders.example.com/webhook",
		"auth_secret":  "webhook-secret",
		"subscriptions": []map[string]any{
			{"subject_pattern": "orders.*", "filter": map[string]any{"type": "urgent"}, "max_retries": maxRetries},
		},
	})
	testutil.WithAdminSecret(req, "test-admin-secret")

	rec := callHandler(t, slurpee, createSubscriberHandler, req)

	var resp SubscriberResponse
	testutil.AssertJSONResponse(t, rec, http.StatusOK, &resp)
	assert.Len(t, resp.Subscriptions, 1)
	assert.Equal(t, "orders.*", resp.Subscriptions[0].SubjectPattern)
	require.NotNil(t, resp.Subscriptions[0].MaxRetries)
	assert.Equal(t, int32(5), *resp.Subscriptions[0].MaxRetries)
	assert.JSONEq(t, `{"type":"urgent"}`, string(resp.Subscriptions[0].Filter))
	mockDB.AssertExpectations(t)
}

func TestCreateSubscriber_MixedAddUpdateDelete(t *testing.T) {
	mockDB := new(testutil.MockQuerier)
	slurpee := testutil.NewTestApp(mockDB)

	subscriber := testutil.NewSubscriber(func(s *db.Subscriber) {
		s.Name = "mixed-service"
		s.EndpointUrl = "https://mixed.example.com/webhook"
		s.AuthSecret = "webhook-secret"
	})

	// Existing: orders.* (will be updated), users.* (will be deleted)
	existingOrders := testutil.NewSubscription(func(s *db.Subscription) {
		s.SubscriberID = subscriber.ID
		s.SubjectPattern = "orders.*"
	})
	existingUsers := testutil.NewSubscription(func(s *db.Subscription) {
		s.SubscriberID = subscriber.ID
		s.SubjectPattern = "users.*"
	})

	mockDB.On("UpsertSubscriber", mock.Anything, mock.AnythingOfType("db.UpsertSubscriberParams")).
		Return(subscriber, nil)
	mockDB.On("ListSubscriptionsForSubscriber", mock.Anything, subscriber.ID).
		Return([]db.Subscription{existingOrders, existingUsers}, nil)

	// orders.* is updated in place
	updatedOrders := existingOrders
	updatedOrders.Filter = []byte(`{"priority":"high"}`)
	mockDB.On("UpdateSubscription", mock.Anything, mock.AnythingOfType("db.UpdateSubscriptionParams")).
		Return(updatedOrders, nil)

	// payments.* is new
	newPayments := testutil.NewSubscription(func(s *db.Subscription) {
		s.SubscriberID = subscriber.ID
		s.SubjectPattern = "payments.*"
	})
	mockDB.On("CreateSubscription", mock.Anything, mock.AnythingOfType("db.CreateSubscriptionParams")).
		Return(newPayments, nil)

	// users.* is deleted (not in incoming)
	mockDB.On("DeleteSubscription", mock.Anything, existingUsers.ID).
		Return(nil)

	req := testutil.NewJSONRequest(t, http.MethodPost, "/subscribers", map[string]any{
		"name":         "mixed-service",
		"endpoint_url": "https://mixed.example.com/webhook",
		"auth_secret":  "webhook-secret",
		"subscriptions": []map[string]any{
			{"subject_pattern": "orders.*", "filter": map[string]any{"priority": "high"}},
			{"subject_pattern": "payments.*"},
		},
	})
	testutil.WithAdminSecret(req, "test-admin-secret")

	rec := callHandler(t, slurpee, createSubscriberHandler, req)

	var resp SubscriberResponse
	testutil.AssertJSONResponse(t, rec, http.StatusOK, &resp)
	assert.Len(t, resp.Subscriptions, 2)
	mockDB.AssertExpectations(t)
}

// --- GET /api/subscribers tests ---

func TestListSubscribers_MissingAdminSecret(t *testing.T) {
	mockDB := new(testutil.MockQuerier)
	slurpee := testutil.NewTestApp(mockDB)

	req := httptest.NewRequest(http.MethodGet, "/subscribers", nil)

	rec := callHandler(t, slurpee, listSubscribersHandler, req)
	testutil.AssertJSONError(t, rec, http.StatusUnauthorized, "Invalid or missing admin secret")
}

func TestListSubscribers_WrongAdminSecret(t *testing.T) {
	mockDB := new(testutil.MockQuerier)
	slurpee := testutil.NewTestApp(mockDB)

	req := httptest.NewRequest(http.MethodGet, "/subscribers", nil)
	testutil.WithAdminSecret(req, "wrong-admin-secret")

	rec := callHandler(t, slurpee, listSubscribersHandler, req)
	testutil.AssertJSONError(t, rec, http.StatusUnauthorized, "Invalid or missing admin secret")
}

func TestListSubscribers_EmptyList(t *testing.T) {
	mockDB := new(testutil.MockQuerier)
	slurpee := testutil.NewTestApp(mockDB)

	mockDB.On("ListSubscribers", mock.Anything).
		Return([]db.Subscriber{}, nil)

	req := httptest.NewRequest(http.MethodGet, "/subscribers", nil)
	testutil.WithAdminSecret(req, "test-admin-secret")

	rec := callHandler(t, slurpee, listSubscribersHandler, req)

	var resp []SubscriberResponse
	testutil.AssertJSONResponse(t, rec, http.StatusOK, &resp)
	assert.Len(t, resp, 0)
	mockDB.AssertExpectations(t)
}

func TestListSubscribers_Success(t *testing.T) {
	mockDB := new(testutil.MockQuerier)
	slurpee := testutil.NewTestApp(mockDB)

	sub1 := testutil.NewSubscriber(func(s *db.Subscriber) {
		s.Name = "order-service"
		s.EndpointUrl = "https://orders.example.com/webhook"
	})
	sub2 := testutil.NewSubscriber(func(s *db.Subscriber) {
		s.Name = "user-service"
		s.EndpointUrl = "https://users.example.com/webhook"
	})

	mockDB.On("ListSubscribers", mock.Anything).
		Return([]db.Subscriber{sub1, sub2}, nil)

	subscription1 := testutil.NewSubscription(func(s *db.Subscription) {
		s.SubscriberID = sub1.ID
		s.SubjectPattern = "orders.*"
	})
	subscription2 := testutil.NewSubscription(func(s *db.Subscription) {
		s.SubscriberID = sub2.ID
		s.SubjectPattern = "users.*"
	})

	mockDB.On("ListSubscriptionsForSubscriber", mock.Anything, sub1.ID).
		Return([]db.Subscription{subscription1}, nil)
	mockDB.On("ListSubscriptionsForSubscriber", mock.Anything, sub2.ID).
		Return([]db.Subscription{subscription2}, nil)

	req := httptest.NewRequest(http.MethodGet, "/subscribers", nil)
	testutil.WithAdminSecret(req, "test-admin-secret")

	rec := callHandler(t, slurpee, listSubscribersHandler, req)

	var resp []SubscriberResponse
	testutil.AssertJSONResponse(t, rec, http.StatusOK, &resp)
	require.Len(t, resp, 2)

	assert.Equal(t, "order-service", resp[0].Name)
	assert.Equal(t, "https://orders.example.com/webhook", resp[0].EndpointURL)
	assert.Len(t, resp[0].Subscriptions, 1)
	assert.Equal(t, "orders.*", resp[0].Subscriptions[0].SubjectPattern)

	assert.Equal(t, "user-service", resp[1].Name)
	assert.Equal(t, "https://users.example.com/webhook", resp[1].EndpointURL)
	assert.Len(t, resp[1].Subscriptions, 1)
	assert.Equal(t, "users.*", resp[1].Subscriptions[0].SubjectPattern)

	mockDB.AssertExpectations(t)
}

func TestListSubscribers_SuccessResponseFormat(t *testing.T) {
	mockDB := new(testutil.MockQuerier)
	slurpee := testutil.NewTestApp(mockDB)

	sub := testutil.NewSubscriber(func(s *db.Subscriber) {
		s.Name = "detailed-service"
		s.EndpointUrl = "https://detailed.example.com/webhook"
		s.MaxParallel = 3
	})

	mockDB.On("ListSubscribers", mock.Anything).
		Return([]db.Subscriber{sub}, nil)

	maxRetries := pgtype.Int4{Int32: 5, Valid: true}
	filter := []byte(`{"type":"order"}`)
	subscription := testutil.NewSubscription(func(s *db.Subscription) {
		s.SubscriberID = sub.ID
		s.SubjectPattern = "orders.*"
		s.Filter = filter
		s.MaxRetries = maxRetries
	})

	mockDB.On("ListSubscriptionsForSubscriber", mock.Anything, sub.ID).
		Return([]db.Subscription{subscription}, nil)

	req := httptest.NewRequest(http.MethodGet, "/subscribers", nil)
	testutil.WithAdminSecret(req, "test-admin-secret")

	rec := callHandler(t, slurpee, listSubscribersHandler, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp []SubscriberResponse
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	require.Len(t, resp, 1)

	// Verify subscriber fields
	assert.Equal(t, "detailed-service", resp[0].Name)
	assert.Equal(t, "https://detailed.example.com/webhook", resp[0].EndpointURL)
	assert.Equal(t, int32(3), resp[0].MaxParallel)
	assert.NotEmpty(t, resp[0].ID)
	assert.NotZero(t, resp[0].CreatedAt)
	assert.NotZero(t, resp[0].UpdatedAt)

	// Verify subscription fields
	require.Len(t, resp[0].Subscriptions, 1)
	subResp := resp[0].Subscriptions[0]
	assert.Equal(t, "orders.*", subResp.SubjectPattern)
	assert.JSONEq(t, `{"type":"order"}`, string(subResp.Filter))
	require.NotNil(t, subResp.MaxRetries)
	assert.Equal(t, int32(5), *subResp.MaxRetries)
	assert.NotEmpty(t, subResp.ID)
	assert.NotZero(t, subResp.CreatedAt)
	assert.NotZero(t, subResp.UpdatedAt)

	mockDB.AssertExpectations(t)
}
