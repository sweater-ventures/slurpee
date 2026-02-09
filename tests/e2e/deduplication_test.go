package e2e

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sweater-ventures/slurpee/api"
	"github.com/sweater-ventures/slurpee/app"
)

func TestDeduplication_TwoOverlappingSubscriptions(t *testing.T) {
	truncateAll(t)
	slurpee := newTestApp(t)
	router := newTestRouter(t, slurpee)

	// Track requests to the mock endpoint
	var mu sync.Mutex
	var receivedRequests []*http.Request
	var receivedBodies []string

	mockEndpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		receivedRequests = append(receivedRequests, r)
		receivedBodies = append(receivedBodies, string(body))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer mockEndpoint.Close()

	// Seed API secret
	secret, plaintext := seedApiSecret(t, slurpee.DB, "test-secret", "my-secret-value", "*")

	// Seed ONE subscriber with TWO overlapping subscriptions:
	//   order.* (max_retries=3) and order.created (max_retries=5)
	subscriber := seedSubscriber(t, slurpee.DB, "test-subscriber", mockEndpoint.URL, "webhook-auth-secret")
	maxRetries3 := int32(3)
	maxRetries5 := int32(5)
	seedSubscription(t, slurpee.DB, subscriber.ID, "order.*", nil, &maxRetries3)
	seedSubscription(t, slurpee.DB, subscriber.ID, "order.created", nil, &maxRetries5)

	// Start the delivery dispatcher
	app.StartDispatcher(slurpee)

	// POST an event with subject order.created — both subscriptions match
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

	// Wait for delivery to complete
	waitForEventStatus(t, slurpee.DB, resp.ID, "delivered", 10*time.Second)

	// Verify the mock endpoint received exactly ONE request (not two)
	mu.Lock()
	reqCount := len(receivedRequests)
	mu.Unlock()

	if reqCount != 1 {
		t.Fatalf("expected mock endpoint to receive 1 request (deduplicated), got %d", reqCount)
	}

	// Verify exactly one delivery_attempts row in the database
	eventUUID := parseUUID(t, resp.ID)
	attempts, err := slurpee.DB.ListDeliveryAttemptsForEvent(context.Background(), eventUUID)
	if err != nil {
		t.Fatalf("ListDeliveryAttemptsForEvent: %v", err)
	}
	if len(attempts) != 1 {
		t.Fatalf("expected 1 delivery attempt (deduplicated), got %d", len(attempts))
	}

	if attempts[0].Status != "succeeded" {
		t.Errorf("expected delivery attempt status %q, got %q", "succeeded", attempts[0].Status)
	}
}

func TestDeduplication_ThreeOverlappingSubscriptions(t *testing.T) {
	truncateAll(t)
	slurpee := newTestApp(t)
	router := newTestRouter(t, slurpee)

	// Track requests to the mock endpoint
	var mu sync.Mutex
	var receivedRequests []*http.Request

	mockEndpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		mu.Lock()
		receivedRequests = append(receivedRequests, r)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer mockEndpoint.Close()

	// Seed API secret
	secret, plaintext := seedApiSecret(t, slurpee.DB, "test-secret", "my-secret-value", "*")

	// Seed ONE subscriber with THREE overlapping subscriptions:
	//   * (max_retries=1), order.* (max_retries=3), order.created (max_retries=5)
	subscriber := seedSubscriber(t, slurpee.DB, "test-subscriber", mockEndpoint.URL, "webhook-auth-secret")
	maxRetries1 := int32(1)
	maxRetries3 := int32(3)
	maxRetries5 := int32(5)
	seedSubscription(t, slurpee.DB, subscriber.ID, "*", nil, &maxRetries1)
	seedSubscription(t, slurpee.DB, subscriber.ID, "order.*", nil, &maxRetries3)
	seedSubscription(t, slurpee.DB, subscriber.ID, "order.created", nil, &maxRetries5)

	// Start the delivery dispatcher
	app.StartDispatcher(slurpee)

	// POST an event with subject order.created — all three subscriptions match
	body := `{"subject":"order.created","data":{"item":"widget"}}`
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

	// Wait for delivery to complete
	waitForEventStatus(t, slurpee.DB, resp.ID, "delivered", 10*time.Second)

	// Verify the mock endpoint received exactly ONE request (not three)
	mu.Lock()
	reqCount := len(receivedRequests)
	mu.Unlock()

	if reqCount != 1 {
		t.Fatalf("expected mock endpoint to receive 1 request (deduplicated from 3 subscriptions), got %d", reqCount)
	}

	// Verify exactly one delivery_attempts row in the database
	eventUUID := parseUUID(t, resp.ID)
	attempts, err := slurpee.DB.ListDeliveryAttemptsForEvent(context.Background(), eventUUID)
	if err != nil {
		t.Fatalf("ListDeliveryAttemptsForEvent: %v", err)
	}
	if len(attempts) != 1 {
		t.Fatalf("expected 1 delivery attempt (deduplicated from 3 subscriptions), got %d", len(attempts))
	}

	if attempts[0].Status != "succeeded" {
		t.Errorf("expected delivery attempt status %q, got %q", "succeeded", attempts[0].Status)
	}
}
