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

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/sweater-ventures/slurpee/api"
	"github.com/sweater-ventures/slurpee/app"
	"github.com/sweater-ventures/slurpee/db"
)

// waitForEventStatus polls the database until the event's delivery_status matches
// the expected value or the timeout is reached.
func waitForEventStatus(t *testing.T, queries db.Querier, eventID string, expected string, timeout time.Duration) db.Event {
	t.Helper()

	// Parse the event ID to pgtype.UUID
	uid := parseUUID(t, eventID)

	deadline := time.Now().Add(timeout)
	for {
		event, err := queries.GetEventByID(context.Background(), uid)
		if err != nil {
			t.Fatalf("GetEventByID: %v", err)
		}
		if event.DeliveryStatus == expected {
			return event
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for event %s delivery_status to become %q (currently %q)", eventID, expected, event.DeliveryStatus)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// parseUUID converts a string UUID to pgtype.UUID.
func parseUUID(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	var uid pgtype.UUID
	if err := uid.Scan(s); err != nil {
		t.Fatalf("parseUUID(%q): %v", s, err)
	}
	return uid
}

func TestDeliveryPipeline_HappyPath(t *testing.T) {
	truncateAll(t)
	slurpee := newTestApp(t)
	router := newTestRouter(t, slurpee)

	// Start a mock subscriber endpoint that records received requests
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

	// Seed API secret for event creation
	secret, plaintext := seedApiSecret(t, slurpee.DB, "test-secret", "my-secret-value", "*")

	// Seed subscriber pointing at the mock endpoint
	subscriber := seedSubscriber(t, slurpee.DB, "test-subscriber", mockEndpoint.URL, "webhook-auth-secret")
	seedSubscription(t, slurpee.DB, subscriber.ID, "order.*", nil, nil)

	// Start the delivery dispatcher
	app.StartDispatcher(slurpee)

	// POST an event via the API
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

	// Wait for the event to be delivered
	event := waitForEventStatus(t, slurpee.DB, resp.ID, "delivered", 10*time.Second)

	// Verify the mock endpoint received exactly one request
	mu.Lock()
	reqCount := len(receivedRequests)
	mu.Unlock()

	if reqCount != 1 {
		t.Fatalf("expected mock endpoint to receive 1 request, got %d", reqCount)
	}

	mu.Lock()
	receivedReq := receivedRequests[0]
	receivedBody := receivedBodies[0]
	mu.Unlock()

	// Verify correct HTTP headers
	if got := receivedReq.Header.Get("X-Event-ID"); got != resp.ID {
		t.Errorf("expected X-Event-ID %q, got %q", resp.ID, got)
	}
	if got := receivedReq.Header.Get("X-Event-Subject"); got != "order.created" {
		t.Errorf("expected X-Event-Subject %q, got %q", "order.created", got)
	}
	if got := receivedReq.Header.Get("X-Slurpee-Secret"); got != "webhook-auth-secret" {
		t.Errorf("expected X-Slurpee-Secret %q, got %q", "webhook-auth-secret", got)
	}
	if got := receivedReq.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("expected Content-Type %q, got %q", "application/json", got)
	}

	// Verify the body is the event data
	var bodyData map[string]any
	if err := json.Unmarshal([]byte(receivedBody), &bodyData); err != nil {
		t.Fatalf("decode received body: %v", err)
	}
	if bodyData["amount"] != 99.99 {
		t.Errorf("expected body amount 99.99, got %v", bodyData["amount"])
	}

	// Verify delivery_attempts row in the database
	eventUUID := parseUUID(t, resp.ID)
	attempts, err := slurpee.DB.ListDeliveryAttemptsForEvent(context.Background(), eventUUID)
	if err != nil {
		t.Fatalf("ListDeliveryAttemptsForEvent: %v", err)
	}
	if len(attempts) != 1 {
		t.Fatalf("expected 1 delivery attempt, got %d", len(attempts))
	}

	attempt := attempts[0]
	if attempt.Status != "succeeded" {
		t.Errorf("expected delivery attempt status %q, got %q", "succeeded", attempt.Status)
	}
	if !attempt.ResponseStatusCode.Valid || attempt.ResponseStatusCode.Int32 != 200 {
		t.Errorf("expected response_status_code 200, got %v", attempt.ResponseStatusCode)
	}

	// Verify event delivery_status is 'delivered'
	if event.DeliveryStatus != "delivered" {
		t.Errorf("expected event delivery_status %q, got %q", "delivered", event.DeliveryStatus)
	}
}

func TestDeliveryPipeline_NoMatchingSubscriptions(t *testing.T) {
	truncateAll(t)
	slurpee := newTestApp(t)
	router := newTestRouter(t, slurpee)

	// Seed API secret for event creation
	secret, plaintext := seedApiSecret(t, slurpee.DB, "test-secret", "my-secret-value", "*")

	// No subscribers seeded — event has no matching subscriptions

	// Start the delivery dispatcher
	app.StartDispatcher(slurpee)

	// POST an event via the API
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

	// Wait for the event status to become 'recorded' (no subscribers to deliver to)
	event := waitForEventStatus(t, slurpee.DB, resp.ID, "recorded", 10*time.Second)

	if event.DeliveryStatus != "recorded" {
		t.Errorf("expected event delivery_status %q, got %q", "recorded", event.DeliveryStatus)
	}

	// Verify no delivery attempts were recorded
	eventUUID := parseUUID(t, resp.ID)
	attempts, err := slurpee.DB.ListDeliveryAttemptsForEvent(context.Background(), eventUUID)
	if err != nil {
		t.Fatalf("ListDeliveryAttemptsForEvent: %v", err)
	}
	if len(attempts) != 0 {
		t.Errorf("expected 0 delivery attempts, got %d", len(attempts))
	}
}
