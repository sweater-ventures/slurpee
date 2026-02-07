package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/sweater-ventures/slurpee/app"
	"github.com/sweater-ventures/slurpee/db"
	"github.com/sweater-ventures/slurpee/testutil"
)

// callHandler invokes an appHandler via routeHandler with the given app and request.
func callHandler(t *testing.T, slurpee *app.Application, handler appHandler, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	routeHandler(slurpee, handler).ServeHTTP(rec, req)
	return rec
}

func TestCreateEvent_MissingSecretHeaders(t *testing.T) {
	mockDB := new(testutil.MockQuerier)
	slurpee := testutil.NewTestApp(mockDB)

	req := testutil.NewJSONRequest(t, http.MethodPost, "/events", map[string]any{
		"subject": "test.subject",
		"data":    map[string]any{"key": "value"},
	})

	rec := callHandler(t, slurpee, createEventHandler, req)
	testutil.AssertJSONError(t, rec, http.StatusUnauthorized, "Missing X-Slurpee-Secret-ID")
}

func TestCreateEvent_MissingSecretValue(t *testing.T) {
	mockDB := new(testutil.MockQuerier)
	slurpee := testutil.NewTestApp(mockDB)

	req := testutil.NewJSONRequest(t, http.MethodPost, "/events", map[string]any{
		"subject": "test.subject",
		"data":    map[string]any{"key": "value"},
	})
	req.Header.Set("X-Slurpee-Secret-ID", uuid.Must(uuid.NewV7()).String())

	rec := callHandler(t, slurpee, createEventHandler, req)
	testutil.AssertJSONError(t, rec, http.StatusUnauthorized, "Missing or invalid API secret")
}

func TestCreateEvent_InvalidSecretIDFormat(t *testing.T) {
	mockDB := new(testutil.MockQuerier)
	slurpee := testutil.NewTestApp(mockDB)

	req := testutil.NewJSONRequest(t, http.MethodPost, "/events", map[string]any{
		"subject": "test.subject",
		"data":    map[string]any{"key": "value"},
	})
	req.Header.Set("X-Slurpee-Secret-ID", "not-a-uuid")
	req.Header.Set("X-Slurpee-Secret", "some-secret")

	rec := callHandler(t, slurpee, createEventHandler, req)
	testutil.AssertJSONError(t, rec, http.StatusBadRequest, "X-Slurpee-Secret-ID must be a valid UUID")
}

func TestCreateEvent_WrongSecretValue(t *testing.T) {
	mockDB := new(testutil.MockQuerier)
	slurpee := testutil.NewTestApp(mockDB)

	secretID := uuid.Must(uuid.NewV7())
	secret := testutil.NewApiSecretWithHash("correct-secret", func(s *db.ApiSecret) {
		s.ID = pgtype.UUID{Bytes: secretID, Valid: true}
		s.SubjectPattern = "%"
	})

	mockDB.On("GetApiSecretByID", mock.Anything, pgtype.UUID{Bytes: secretID, Valid: true}).
		Return(secret, nil)

	req := testutil.NewJSONRequest(t, http.MethodPost, "/events", map[string]any{
		"subject": "test.subject",
		"data":    map[string]any{"key": "value"},
	})
	testutil.WithSecretHeaders(req, secretID.String(), "wrong-secret")

	rec := callHandler(t, slurpee, createEventHandler, req)
	testutil.AssertJSONError(t, rec, http.StatusUnauthorized, "Missing or invalid API secret")
}

func TestCreateEvent_SubjectOutOfScope(t *testing.T) {
	mockDB := new(testutil.MockQuerier)
	slurpee := testutil.NewTestApp(mockDB)

	secretID := uuid.Must(uuid.NewV7())
	secret := testutil.NewApiSecretWithHash("test-secret", func(s *db.ApiSecret) {
		s.ID = pgtype.UUID{Bytes: secretID, Valid: true}
		s.SubjectPattern = "orders.%"
	})

	mockDB.On("GetApiSecretByID", mock.Anything, pgtype.UUID{Bytes: secretID, Valid: true}).
		Return(secret, nil)

	req := testutil.NewJSONRequest(t, http.MethodPost, "/events", map[string]any{
		"subject": "users.created",
		"data":    map[string]any{"key": "value"},
	})
	testutil.WithSecretHeaders(req, secretID.String(), "test-secret")

	rec := callHandler(t, slurpee, createEventHandler, req)
	testutil.AssertJSONError(t, rec, http.StatusForbidden, "Subject not permitted by API secret scope")
}

func TestCreateEvent_MissingSubject(t *testing.T) {
	mockDB := new(testutil.MockQuerier)
	slurpee := testutil.NewTestApp(mockDB)

	secretID := uuid.Must(uuid.NewV7())
	secret := testutil.NewApiSecretWithHash("test-secret", func(s *db.ApiSecret) {
		s.ID = pgtype.UUID{Bytes: secretID, Valid: true}
		s.SubjectPattern = "%"
	})

	mockDB.On("GetApiSecretByID", mock.Anything, pgtype.UUID{Bytes: secretID, Valid: true}).
		Return(secret, nil)

	req := testutil.NewJSONRequest(t, http.MethodPost, "/events", map[string]any{
		"data": map[string]any{"key": "value"},
	})
	testutil.WithSecretHeaders(req, secretID.String(), "test-secret")

	rec := callHandler(t, slurpee, createEventHandler, req)
	testutil.AssertJSONError(t, rec, http.StatusBadRequest, "subject is required")
}

func TestCreateEvent_MissingData(t *testing.T) {
	mockDB := new(testutil.MockQuerier)
	slurpee := testutil.NewTestApp(mockDB)

	secretID := uuid.Must(uuid.NewV7())
	secret := testutil.NewApiSecretWithHash("test-secret", func(s *db.ApiSecret) {
		s.ID = pgtype.UUID{Bytes: secretID, Valid: true}
		s.SubjectPattern = "%"
	})

	mockDB.On("GetApiSecretByID", mock.Anything, pgtype.UUID{Bytes: secretID, Valid: true}).
		Return(secret, nil)

	req := testutil.NewJSONRequest(t, http.MethodPost, "/events", map[string]any{
		"subject": "test.subject",
	})
	testutil.WithSecretHeaders(req, secretID.String(), "test-secret")

	rec := callHandler(t, slurpee, createEventHandler, req)
	testutil.AssertJSONError(t, rec, http.StatusBadRequest, "data is required")
}

func TestCreateEvent_InvalidJSONBody(t *testing.T) {
	mockDB := new(testutil.MockQuerier)
	slurpee := testutil.NewTestApp(mockDB)

	secretID := uuid.Must(uuid.NewV7())
	secret := testutil.NewApiSecretWithHash("test-secret", func(s *db.ApiSecret) {
		s.ID = pgtype.UUID{Bytes: secretID, Valid: true}
		s.SubjectPattern = "%"
	})

	mockDB.On("GetApiSecretByID", mock.Anything, pgtype.UUID{Bytes: secretID, Valid: true}).
		Return(secret, nil)

	req := httptest.NewRequest(http.MethodPost, "/events", nil)
	req.Header.Set("Content-Type", "application/json")
	testutil.WithSecretHeaders(req, secretID.String(), "test-secret")

	rec := callHandler(t, slurpee, createEventHandler, req)
	testutil.AssertJSONError(t, rec, http.StatusBadRequest, "Invalid request body")
}

func TestCreateEvent_Success(t *testing.T) {
	mockDB := new(testutil.MockQuerier)
	slurpee := testutil.NewTestApp(mockDB)

	secretID := uuid.Must(uuid.NewV7())
	secret := testutil.NewApiSecretWithHash("test-secret", func(s *db.ApiSecret) {
		s.ID = pgtype.UUID{Bytes: secretID, Valid: true}
		s.SubjectPattern = "%"
	})

	mockDB.On("GetApiSecretByID", mock.Anything, pgtype.UUID{Bytes: secretID, Valid: true}).
		Return(secret, nil)

	insertedEvent := testutil.NewEvent(func(e *db.Event) {
		e.Subject = "order.created"
		e.Data = json.RawMessage(`{"order_id":"123"}`)
		e.DeliveryStatus = "pending"
	})
	mockDB.On("InsertEvent", mock.Anything, mock.AnythingOfType("db.InsertEventParams")).
		Return(insertedEvent, nil)

	// GetLogConfigBySubject is called by LogEvent — return error to skip detailed logging
	mockDB.On("GetLogConfigBySubject", mock.Anything, "order.created").
		Return(db.LogConfig{}, assert.AnError)

	req := testutil.NewJSONRequest(t, http.MethodPost, "/events", map[string]any{
		"subject": "order.created",
		"data":    map[string]any{"order_id": "123"},
	})
	testutil.WithSecretHeaders(req, secretID.String(), "test-secret")

	rec := callHandler(t, slurpee, createEventHandler, req)

	var resp EventResponse
	testutil.AssertJSONResponse(t, rec, http.StatusCreated, &resp)
	assert.Equal(t, "order.created", resp.Subject)
	assert.Equal(t, "pending", resp.DeliveryStatus)
	assert.NotEmpty(t, resp.ID)

	// Assert event was sent to delivery channel
	select {
	case event := <-slurpee.DeliveryChan:
		assert.Equal(t, insertedEvent.Subject, event.Subject)
	default:
		t.Fatal("expected event on DeliveryChan but channel was empty")
	}

	mockDB.AssertExpectations(t)
}

// --- GET /api/events/{id} tests ---

func TestGetEvent_MissingSecretHeaders(t *testing.T) {
	mockDB := new(testutil.MockQuerier)
	slurpee := testutil.NewTestApp(mockDB)

	eventID := uuid.Must(uuid.NewV7())
	req := httptest.NewRequest(http.MethodGet, "/events/"+eventID.String(), nil)
	req.SetPathValue("id", eventID.String())

	rec := callHandler(t, slurpee, getEventHandler, req)
	testutil.AssertJSONError(t, rec, http.StatusUnauthorized, "Missing X-Slurpee-Secret-ID")
}

func TestGetEvent_InvalidSecretIDFormat(t *testing.T) {
	mockDB := new(testutil.MockQuerier)
	slurpee := testutil.NewTestApp(mockDB)

	eventID := uuid.Must(uuid.NewV7())
	req := httptest.NewRequest(http.MethodGet, "/events/"+eventID.String(), nil)
	req.SetPathValue("id", eventID.String())
	req.Header.Set("X-Slurpee-Secret-ID", "not-a-uuid")
	req.Header.Set("X-Slurpee-Secret", "some-secret")

	rec := callHandler(t, slurpee, getEventHandler, req)
	testutil.AssertJSONError(t, rec, http.StatusBadRequest, "X-Slurpee-Secret-ID must be a valid UUID")
}

func TestGetEvent_WrongSecretValue(t *testing.T) {
	mockDB := new(testutil.MockQuerier)
	slurpee := testutil.NewTestApp(mockDB)

	secretID := uuid.Must(uuid.NewV7())
	secret := testutil.NewApiSecretWithHash("correct-secret", func(s *db.ApiSecret) {
		s.ID = pgtype.UUID{Bytes: secretID, Valid: true}
		s.SubjectPattern = "%"
	})

	mockDB.On("GetApiSecretByID", mock.Anything, pgtype.UUID{Bytes: secretID, Valid: true}).
		Return(secret, nil)

	eventID := uuid.Must(uuid.NewV7())
	req := httptest.NewRequest(http.MethodGet, "/events/"+eventID.String(), nil)
	req.SetPathValue("id", eventID.String())
	testutil.WithSecretHeaders(req, secretID.String(), "wrong-secret")

	rec := callHandler(t, slurpee, getEventHandler, req)
	testutil.AssertJSONError(t, rec, http.StatusUnauthorized, "Missing or invalid API secret")
}

func TestGetEvent_NonexistentEvent(t *testing.T) {
	mockDB := new(testutil.MockQuerier)
	slurpee := testutil.NewTestApp(mockDB)

	secretID := uuid.Must(uuid.NewV7())
	secret := testutil.NewApiSecretWithHash("test-secret", func(s *db.ApiSecret) {
		s.ID = pgtype.UUID{Bytes: secretID, Valid: true}
		s.SubjectPattern = "%"
	})

	mockDB.On("GetApiSecretByID", mock.Anything, pgtype.UUID{Bytes: secretID, Valid: true}).
		Return(secret, nil)

	eventID := uuid.Must(uuid.NewV7())
	mockDB.On("GetEventByID", mock.Anything, pgtype.UUID{Bytes: eventID, Valid: true}).
		Return(db.Event{}, pgx.ErrNoRows)

	req := httptest.NewRequest(http.MethodGet, "/events/"+eventID.String(), nil)
	req.SetPathValue("id", eventID.String())
	testutil.WithSecretHeaders(req, secretID.String(), "test-secret")

	rec := callHandler(t, slurpee, getEventHandler, req)
	testutil.AssertJSONError(t, rec, http.StatusNotFound, "event not found")
	mockDB.AssertExpectations(t)
}

func TestGetEvent_Success(t *testing.T) {
	mockDB := new(testutil.MockQuerier)
	slurpee := testutil.NewTestApp(mockDB)

	secretID := uuid.Must(uuid.NewV7())
	secret := testutil.NewApiSecretWithHash("test-secret", func(s *db.ApiSecret) {
		s.ID = pgtype.UUID{Bytes: secretID, Valid: true}
		s.SubjectPattern = "%"
	})

	mockDB.On("GetApiSecretByID", mock.Anything, pgtype.UUID{Bytes: secretID, Valid: true}).
		Return(secret, nil)

	eventID := uuid.Must(uuid.NewV7())
	event := testutil.NewEvent(func(e *db.Event) {
		e.ID = pgtype.UUID{Bytes: eventID, Valid: true}
		e.Subject = "order.created"
		e.Data = json.RawMessage(`{"order_id":"123"}`)
		e.DeliveryStatus = "delivered"
		e.RetryCount = 2
	})

	mockDB.On("GetEventByID", mock.Anything, pgtype.UUID{Bytes: eventID, Valid: true}).
		Return(event, nil)

	req := httptest.NewRequest(http.MethodGet, "/events/"+eventID.String(), nil)
	req.SetPathValue("id", eventID.String())
	testutil.WithSecretHeaders(req, secretID.String(), "test-secret")

	rec := callHandler(t, slurpee, getEventHandler, req)

	var resp EventResponse
	testutil.AssertJSONResponse(t, rec, http.StatusOK, &resp)
	assert.Equal(t, eventID.String(), resp.ID)
	assert.Equal(t, "order.created", resp.Subject)
	assert.Equal(t, "delivered", resp.DeliveryStatus)
	assert.Equal(t, int32(2), resp.RetryCount)
	assert.JSONEq(t, `{"order_id":"123"}`, string(resp.Data))
	assert.NotZero(t, resp.Timestamp)
	mockDB.AssertExpectations(t)
}

func TestGetEvent_SuccessResponseFormat(t *testing.T) {
	mockDB := new(testutil.MockQuerier)
	slurpee := testutil.NewTestApp(mockDB)

	secretID := uuid.Must(uuid.NewV7())
	secret := testutil.NewApiSecretWithHash("test-secret", func(s *db.ApiSecret) {
		s.ID = pgtype.UUID{Bytes: secretID, Valid: true}
		s.SubjectPattern = "%"
	})

	mockDB.On("GetApiSecretByID", mock.Anything, pgtype.UUID{Bytes: secretID, Valid: true}).
		Return(secret, nil)

	traceUUID := uuid.Must(uuid.NewV7())
	eventID := uuid.Must(uuid.NewV7())
	event := testutil.NewEvent(func(e *db.Event) {
		e.ID = pgtype.UUID{Bytes: eventID, Valid: true}
		e.Subject = "user.updated"
		e.Data = json.RawMessage(`{"user_id":"456"}`)
		e.TraceID = pgtype.UUID{Bytes: traceUUID, Valid: true}
		e.DeliveryStatus = "pending"
		e.RetryCount = 0
	})

	mockDB.On("GetEventByID", mock.Anything, pgtype.UUID{Bytes: eventID, Valid: true}).
		Return(event, nil)

	req := httptest.NewRequest(http.MethodGet, "/events/"+eventID.String(), nil)
	req.SetPathValue("id", eventID.String())
	testutil.WithSecretHeaders(req, secretID.String(), "test-secret")

	rec := callHandler(t, slurpee, getEventHandler, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp EventResponse
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, eventID.String(), resp.ID)
	assert.Equal(t, "user.updated", resp.Subject)
	assert.NotZero(t, resp.Timestamp)
	assert.JSONEq(t, `{"user_id":"456"}`, string(resp.Data))
	assert.Equal(t, int32(0), resp.RetryCount)
	assert.Equal(t, "pending", resp.DeliveryStatus)
	assert.NotNil(t, resp.TraceID)
	assert.Equal(t, traceUUID.String(), *resp.TraceID)
	assert.NotNil(t, resp.StatusUpdatedAt)
	mockDB.AssertExpectations(t)
}

func TestCreateEvent_SuccessResponseFormat(t *testing.T) {
	mockDB := new(testutil.MockQuerier)
	slurpee := testutil.NewTestApp(mockDB)

	secretID := uuid.Must(uuid.NewV7())
	secret := testutil.NewApiSecretWithHash("test-secret", func(s *db.ApiSecret) {
		s.ID = pgtype.UUID{Bytes: secretID, Valid: true}
		s.SubjectPattern = "%"
	})

	mockDB.On("GetApiSecretByID", mock.Anything, pgtype.UUID{Bytes: secretID, Valid: true}).
		Return(secret, nil)

	eventData := json.RawMessage(`{"user_id":"456"}`)
	insertedEvent := testutil.NewEvent(func(e *db.Event) {
		e.Subject = "user.updated"
		e.Data = eventData
	})
	mockDB.On("InsertEvent", mock.Anything, mock.AnythingOfType("db.InsertEventParams")).
		Return(insertedEvent, nil)

	mockDB.On("GetLogConfigBySubject", mock.Anything, "user.updated").
		Return(db.LogConfig{}, assert.AnError)

	req := testutil.NewJSONRequest(t, http.MethodPost, "/events", map[string]any{
		"subject": "user.updated",
		"data":    map[string]any{"user_id": "456"},
	})
	testutil.WithSecretHeaders(req, secretID.String(), "test-secret")

	rec := callHandler(t, slurpee, createEventHandler, req)
	require.Equal(t, http.StatusCreated, rec.Code)

	// Verify full response structure
	var resp EventResponse
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, "user.updated", resp.Subject)
	assert.NotEmpty(t, resp.ID)
	assert.NotZero(t, resp.Timestamp)
	assert.JSONEq(t, `{"user_id":"456"}`, string(resp.Data))
	assert.Equal(t, int32(0), resp.RetryCount)

	// Verify event was dispatched
	select {
	case event := <-slurpee.DeliveryChan:
		assert.Equal(t, "user.updated", event.Subject)
	default:
		t.Fatal("expected event on DeliveryChan but channel was empty")
	}

	mockDB.AssertExpectations(t)
}
