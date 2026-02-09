package e2e

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sweater-ventures/slurpee/api"
	"github.com/sweater-ventures/slurpee/app"
)

func TestDeliveryRetries_SuccessAfterFailures(t *testing.T) {
	truncateAll(t)
	slurpee := newTestApp(t)
	router := newTestRouter(t, slurpee)

	// Config: MaxRetries=2, MaxBackoffSeconds=1
	// So total attempts allowed = maxRetries + 1 = 3 (attempt 0, 1, 2)
	// We'll fail the first 2 attempts (return 500), then succeed on the 3rd.

	var requestCount atomic.Int32

	mockEndpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := requestCount.Add(1)
		if count <= 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer mockEndpoint.Close()

	// Seed API secret and subscriber
	secret, plaintext := seedApiSecret(t, slurpee.DB, "test-secret", "my-secret-value", "*")
	subscriber := seedSubscriber(t, slurpee.DB, "test-subscriber", mockEndpoint.URL, "webhook-auth-secret")
	seedSubscription(t, slurpee.DB, subscriber.ID, "order.*", nil, nil)

	// Start the delivery dispatcher
	app.StartDispatcher(slurpee)

	// POST an event
	body := `{"subject":"order.created","data":{"amount":42.00}}`
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

	// Wait for the event to be delivered (after retries succeed)
	event := waitForEventStatus(t, slurpee.DB, resp.ID, "delivered", 30*time.Second)

	if event.DeliveryStatus != "delivered" {
		t.Errorf("expected event delivery_status %q, got %q", "delivered", event.DeliveryStatus)
	}

	// Verify the mock endpoint received exactly 3 requests (2 failures + 1 success)
	finalCount := requestCount.Load()
	if finalCount != 3 {
		t.Fatalf("expected mock endpoint to receive 3 requests, got %d", finalCount)
	}

	// Verify multiple delivery_attempts rows recorded (one per attempt)
	eventUUID := parseUUID(t, resp.ID)
	attempts, err := slurpee.DB.ListDeliveryAttemptsForEvent(context.Background(), eventUUID)
	if err != nil {
		t.Fatalf("ListDeliveryAttemptsForEvent: %v", err)
	}
	if len(attempts) != 3 {
		t.Fatalf("expected 3 delivery attempts, got %d", len(attempts))
	}

	// Verify attempt statuses: first 2 failed, last succeeded
	// Attempts are ordered by attempted_at, check that we have 2 failed and 1 succeeded
	failedCount := 0
	succeededCount := 0
	for _, a := range attempts {
		switch a.Status {
		case "failed":
			failedCount++
			if !a.ResponseStatusCode.Valid || a.ResponseStatusCode.Int32 != 500 {
				t.Errorf("expected failed attempt response_status_code 500, got %v", a.ResponseStatusCode)
			}
		case "succeeded":
			succeededCount++
			if !a.ResponseStatusCode.Valid || a.ResponseStatusCode.Int32 != 200 {
				t.Errorf("expected succeeded attempt response_status_code 200, got %v", a.ResponseStatusCode)
			}
		default:
			t.Errorf("unexpected delivery attempt status: %q", a.Status)
		}
	}
	if failedCount != 2 {
		t.Errorf("expected 2 failed attempts, got %d", failedCount)
	}
	if succeededCount != 1 {
		t.Errorf("expected 1 succeeded attempt, got %d", succeededCount)
	}
}

func TestDeliveryRetries_MaxRetriesExhausted(t *testing.T) {
	truncateAll(t)
	slurpee := newTestApp(t)
	router := newTestRouter(t, slurpee)

	// Config: MaxRetries=2, MaxBackoffSeconds=1
	// Total attempts = maxRetries + 1 = 3
	// Endpoint always returns 500 — all attempts should fail

	var mu sync.Mutex
	var requestCount int

	mockEndpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestCount++
		mu.Unlock()
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer mockEndpoint.Close()

	// Seed API secret and subscriber
	secret, plaintext := seedApiSecret(t, slurpee.DB, "test-secret", "my-secret-value", "*")
	subscriber := seedSubscriber(t, slurpee.DB, "test-subscriber", mockEndpoint.URL, "webhook-auth-secret")
	seedSubscription(t, slurpee.DB, subscriber.ID, "order.*", nil, nil)

	// Start the delivery dispatcher
	app.StartDispatcher(slurpee)

	// POST an event
	body := `{"subject":"order.created","data":{"amount":42.00}}`
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

	// Wait for the event to become failed (all retries exhausted)
	event := waitForEventStatus(t, slurpee.DB, resp.ID, "failed", 30*time.Second)

	if event.DeliveryStatus != "failed" {
		t.Errorf("expected event delivery_status %q, got %q", "failed", event.DeliveryStatus)
	}

	// Verify the mock endpoint received exactly max_retries + 1 = 3 requests
	mu.Lock()
	finalCount := requestCount
	mu.Unlock()

	if finalCount != 3 {
		t.Fatalf("expected mock endpoint to receive 3 requests (1 initial + 2 retries), got %d", finalCount)
	}

	// Verify exactly max_retries + 1 = 3 delivery attempts recorded
	eventUUID := parseUUID(t, resp.ID)
	attempts, err := slurpee.DB.ListDeliveryAttemptsForEvent(context.Background(), eventUUID)
	if err != nil {
		t.Fatalf("ListDeliveryAttemptsForEvent: %v", err)
	}
	if len(attempts) != 3 {
		t.Fatalf("expected 3 delivery attempts (max_retries+1), got %d", len(attempts))
	}

	// Verify all attempts are failed with 500 status
	for i, a := range attempts {
		if a.Status != "failed" {
			t.Errorf("attempt %d: expected status %q, got %q", i, "failed", a.Status)
		}
		if !a.ResponseStatusCode.Valid || a.ResponseStatusCode.Int32 != 500 {
			t.Errorf("attempt %d: expected response_status_code 500, got %v", i, a.ResponseStatusCode)
		}
	}
}
