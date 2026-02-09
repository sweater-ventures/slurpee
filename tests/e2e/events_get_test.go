package e2e

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/sweater-ventures/slurpee/api"
	"github.com/sweater-ventures/slurpee/app"
)

func TestGetEvent_HappyPath(t *testing.T) {
	truncateAll(t)
	slurpee := newTestApp(t)
	router := newTestRouter(t, slurpee)

	secret, plaintext := seedApiSecret(t, slurpee.DB, "test-secret", "my-secret-value", "*")

	// Create an event via POST first
	body := `{"subject":"order.created","data":{"amount":42}}`
	createReq := httptest.NewRequest("POST", "/api/events", strings.NewReader(body))
	createReq.Header.Set("X-Slurpee-Secret-ID", app.UuidToString(secret.ID))
	createReq.Header.Set("X-Slurpee-Secret", plaintext)
	createRR := httptest.NewRecorder()
	router.ServeHTTP(createRR, createReq)

	if createRR.Code != http.StatusCreated {
		t.Fatalf("POST expected 201, got %d: %s", createRR.Code, createRR.Body.String())
	}

	var created api.EventResponse
	json.NewDecoder(createRR.Body).Decode(&created)
	drainDeliveryChan(slurpee)

	// GET the event by ID
	getReq := httptest.NewRequest("GET", "/api/events/"+created.ID, nil)
	getReq.Header.Set("X-Slurpee-Secret-ID", app.UuidToString(secret.ID))
	getReq.Header.Set("X-Slurpee-Secret", plaintext)
	getRR := httptest.NewRecorder()
	router.ServeHTTP(getRR, getReq)

	if getRR.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", getRR.Code, getRR.Body.String())
	}

	var got api.EventResponse
	if err := json.NewDecoder(getRR.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if got.ID != created.ID {
		t.Errorf("expected ID %s, got %s", created.ID, got.ID)
	}
	if got.Subject != created.Subject {
		t.Errorf("expected subject %s, got %s", created.Subject, got.Subject)
	}
	if got.DeliveryStatus != created.DeliveryStatus {
		t.Errorf("expected delivery_status %s, got %s", created.DeliveryStatus, got.DeliveryStatus)
	}
	if !got.Timestamp.Equal(created.Timestamp) {
		t.Errorf("expected timestamp %v, got %v", created.Timestamp, got.Timestamp)
	}
}

func TestGetEvent_NotFound(t *testing.T) {
	truncateAll(t)
	slurpee := newTestApp(t)
	router := newTestRouter(t, slurpee)

	secret, plaintext := seedApiSecret(t, slurpee.DB, "test-secret", "my-secret-value", "*")

	// Use a valid UUID that doesn't exist
	fakeID := uuid.Must(uuid.NewV7()).String()
	req := httptest.NewRequest("GET", "/api/events/"+fakeID, nil)
	req.Header.Set("X-Slurpee-Secret-ID", app.UuidToString(secret.ID))
	req.Header.Set("X-Slurpee-Secret", plaintext)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestGetEvent_InvalidUUIDFormat(t *testing.T) {
	truncateAll(t)
	slurpee := newTestApp(t)
	router := newTestRouter(t, slurpee)

	secret, plaintext := seedApiSecret(t, slurpee.DB, "test-secret", "my-secret-value", "*")

	req := httptest.NewRequest("GET", "/api/events/not-a-uuid", nil)
	req.Header.Set("X-Slurpee-Secret-ID", app.UuidToString(secret.ID))
	req.Header.Set("X-Slurpee-Secret", plaintext)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestGetEvent_MissingSecret(t *testing.T) {
	truncateAll(t)
	slurpee := newTestApp(t)
	router := newTestRouter(t, slurpee)

	fakeID := uuid.Must(uuid.NewV7()).String()
	req := httptest.NewRequest("GET", "/api/events/"+fakeID, nil)
	// No auth headers
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestGetEvent_InvalidSecret(t *testing.T) {
	truncateAll(t)
	slurpee := newTestApp(t)
	router := newTestRouter(t, slurpee)

	secret, _ := seedApiSecret(t, slurpee.DB, "test-secret", "my-secret-value", "*")

	fakeID := uuid.Must(uuid.NewV7()).String()
	req := httptest.NewRequest("GET", "/api/events/"+fakeID, nil)
	req.Header.Set("X-Slurpee-Secret-ID", app.UuidToString(secret.ID))
	req.Header.Set("X-Slurpee-Secret", "wrong-secret-value")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rr.Code, rr.Body.String())
	}
}
