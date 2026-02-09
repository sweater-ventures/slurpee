package e2e

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/sweater-ventures/slurpee/api"
	"github.com/sweater-ventures/slurpee/app"
)

func TestCreateEvent_HappyPath(t *testing.T) {
	truncateAll(t)
	slurpee := newTestApp(t)
	router := newTestRouter(t, slurpee)

	secret, plaintext := seedApiSecret(t, slurpee.DB, "test-secret", "my-secret-value", "*")

	body := `{"subject":"order.created","data":{"amount":99.99}}`
	req := httptest.NewRequest("POST", "/api/events", strings.NewReader(body))
	req.Header.Set("X-Slurpee-Secret-ID", app.UuidToString(secret.ID))
	req.Header.Set("X-Slurpee-Secret", plaintext)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp api.EventResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Subject != "order.created" {
		t.Errorf("expected subject order.created, got %s", resp.Subject)
	}
	if resp.DeliveryStatus != "pending" {
		t.Errorf("expected delivery_status pending, got %s", resp.DeliveryStatus)
	}
	if resp.ID == "" {
		t.Error("expected non-empty event ID")
	}

	// Verify persisted in database
	eventID, _ := uuid.Parse(resp.ID)
	dbEvent, err := slurpee.DB.GetEventByID(context.Background(), pgtype.UUID{Bytes: eventID, Valid: true})
	if err != nil {
		t.Fatalf("GetEventByID: %v", err)
	}
	if dbEvent.Subject != "order.created" {
		t.Errorf("DB subject: expected order.created, got %s", dbEvent.Subject)
	}

	// Drain the delivery channel so it doesn't block
	drainDeliveryChan(slurpee)
}

func TestCreateEvent_ClientProvidedUUID(t *testing.T) {
	truncateAll(t)
	slurpee := newTestApp(t)
	router := newTestRouter(t, slurpee)

	secret, plaintext := seedApiSecret(t, slurpee.DB, "test-secret", "my-secret-value", "*")
	clientID := uuid.Must(uuid.NewV7()).String()

	body := `{"subject":"order.created","data":{"k":"v"},"id":"` + clientID + `"}`
	req := httptest.NewRequest("POST", "/api/events", strings.NewReader(body))
	req.Header.Set("X-Slurpee-Secret-ID", app.UuidToString(secret.ID))
	req.Header.Set("X-Slurpee-Secret", plaintext)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp api.EventResponse
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp.ID != clientID {
		t.Errorf("expected ID %s, got %s", clientID, resp.ID)
	}

	drainDeliveryChan(slurpee)
}

func TestCreateEvent_ClientProvidedTimestamp(t *testing.T) {
	truncateAll(t)
	slurpee := newTestApp(t)
	router := newTestRouter(t, slurpee)

	secret, plaintext := seedApiSecret(t, slurpee.DB, "test-secret", "my-secret-value", "*")
	customTime := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)

	body := `{"subject":"order.created","data":{"k":"v"},"timestamp":"2025-06-15T12:00:00Z"}`
	req := httptest.NewRequest("POST", "/api/events", strings.NewReader(body))
	req.Header.Set("X-Slurpee-Secret-ID", app.UuidToString(secret.ID))
	req.Header.Set("X-Slurpee-Secret", plaintext)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp api.EventResponse
	json.NewDecoder(rr.Body).Decode(&resp)
	if !resp.Timestamp.Equal(customTime) {
		t.Errorf("expected timestamp %v, got %v", customTime, resp.Timestamp)
	}

	drainDeliveryChan(slurpee)
}

func TestCreateEvent_WithTraceID(t *testing.T) {
	truncateAll(t)
	slurpee := newTestApp(t)
	router := newTestRouter(t, slurpee)

	secret, plaintext := seedApiSecret(t, slurpee.DB, "test-secret", "my-secret-value", "*")
	traceID := uuid.Must(uuid.NewV7()).String()

	body := `{"subject":"order.created","data":{"k":"v"},"trace_id":"` + traceID + `"}`
	req := httptest.NewRequest("POST", "/api/events", strings.NewReader(body))
	req.Header.Set("X-Slurpee-Secret-ID", app.UuidToString(secret.ID))
	req.Header.Set("X-Slurpee-Secret", plaintext)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp api.EventResponse
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp.TraceID == nil || *resp.TraceID != traceID {
		t.Errorf("expected trace_id %s, got %v", traceID, resp.TraceID)
	}

	// Verify persisted
	eventID, _ := uuid.Parse(resp.ID)
	dbEvent, err := slurpee.DB.GetEventByID(context.Background(), pgtype.UUID{Bytes: eventID, Valid: true})
	if err != nil {
		t.Fatalf("GetEventByID: %v", err)
	}
	if !dbEvent.TraceID.Valid {
		t.Error("expected trace_id to be persisted")
	}

	drainDeliveryChan(slurpee)
}

func TestCreateEvent_MissingSecretIDHeader(t *testing.T) {
	truncateAll(t)
	slurpee := newTestApp(t)
	router := newTestRouter(t, slurpee)

	body := `{"subject":"order.created","data":{"k":"v"}}`
	req := httptest.NewRequest("POST", "/api/events", strings.NewReader(body))
	// No X-Slurpee-Secret-ID header
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestCreateEvent_InvalidSecret(t *testing.T) {
	truncateAll(t)
	slurpee := newTestApp(t)
	router := newTestRouter(t, slurpee)

	secret, _ := seedApiSecret(t, slurpee.DB, "test-secret", "my-secret-value", "*")

	body := `{"subject":"order.created","data":{"k":"v"}}`
	req := httptest.NewRequest("POST", "/api/events", strings.NewReader(body))
	req.Header.Set("X-Slurpee-Secret-ID", app.UuidToString(secret.ID))
	req.Header.Set("X-Slurpee-Secret", "wrong-secret-value")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestCreateEvent_SubjectOutOfScope(t *testing.T) {
	truncateAll(t)
	slurpee := newTestApp(t)
	router := newTestRouter(t, slurpee)

	// Secret scoped to order.* only
	secret, plaintext := seedApiSecret(t, slurpee.DB, "test-secret", "my-secret-value", "order.*")

	body := `{"subject":"user.created","data":{"k":"v"}}`
	req := httptest.NewRequest("POST", "/api/events", strings.NewReader(body))
	req.Header.Set("X-Slurpee-Secret-ID", app.UuidToString(secret.ID))
	req.Header.Set("X-Slurpee-Secret", plaintext)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestCreateEvent_MissingSubject(t *testing.T) {
	truncateAll(t)
	slurpee := newTestApp(t)
	router := newTestRouter(t, slurpee)

	secret, plaintext := seedApiSecret(t, slurpee.DB, "test-secret", "my-secret-value", "*")

	body := `{"data":{"k":"v"}}`
	req := httptest.NewRequest("POST", "/api/events", strings.NewReader(body))
	req.Header.Set("X-Slurpee-Secret-ID", app.UuidToString(secret.ID))
	req.Header.Set("X-Slurpee-Secret", plaintext)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestCreateEvent_MissingData(t *testing.T) {
	truncateAll(t)
	slurpee := newTestApp(t)
	router := newTestRouter(t, slurpee)

	secret, plaintext := seedApiSecret(t, slurpee.DB, "test-secret", "my-secret-value", "*")

	body := `{"subject":"order.created"}`
	req := httptest.NewRequest("POST", "/api/events", strings.NewReader(body))
	req.Header.Set("X-Slurpee-Secret-ID", app.UuidToString(secret.ID))
	req.Header.Set("X-Slurpee-Secret", plaintext)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestCreateEvent_InvalidJSONData(t *testing.T) {
	truncateAll(t)
	slurpee := newTestApp(t)
	router := newTestRouter(t, slurpee)

	secret, plaintext := seedApiSecret(t, slurpee.DB, "test-secret", "my-secret-value", "*")

	body := `{"subject":"order.created","data":"not-an-object"}`
	req := httptest.NewRequest("POST", "/api/events", strings.NewReader(body))
	req.Header.Set("X-Slurpee-Secret-ID", app.UuidToString(secret.ID))
	req.Header.Set("X-Slurpee-Secret", plaintext)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestCreateEvent_InvalidJSONBody(t *testing.T) {
	truncateAll(t)
	slurpee := newTestApp(t)
	router := newTestRouter(t, slurpee)

	secret, plaintext := seedApiSecret(t, slurpee.DB, "test-secret", "my-secret-value", "*")

	body := `not valid json`
	req := httptest.NewRequest("POST", "/api/events", strings.NewReader(body))
	req.Header.Set("X-Slurpee-Secret-ID", app.UuidToString(secret.ID))
	req.Header.Set("X-Slurpee-Secret", plaintext)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

// drainDeliveryChan reads any pending events from the delivery channel to prevent blocking.
func drainDeliveryChan(slurpee *app.Application) {
	for {
		select {
		case <-slurpee.DeliveryChan:
		default:
			return
		}
	}
}
