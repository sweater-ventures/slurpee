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

func TestPatternMatching_ExactMatch(t *testing.T) {
	truncateAll(t)
	slurpee := newTestApp(t)
	router := newTestRouter(t, slurpee)

	var mu sync.Mutex
	var receivedCount int

	mockEndpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		receivedCount++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer mockEndpoint.Close()

	secret, plaintext := seedApiSecret(t, slurpee.DB, "test-secret", "my-secret-value", "*")
	subscriber := seedSubscriber(t, slurpee.DB, "test-subscriber", mockEndpoint.URL, "auth-secret")
	seedSubscription(t, slurpee.DB, subscriber.ID, "order.created", nil, nil)

	app.StartDispatcher(slurpee)

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

	event := waitForEventStatus(t, slurpee.DB, resp.ID, "delivered", 10*time.Second)

	mu.Lock()
	count := receivedCount
	mu.Unlock()

	if count != 1 {
		t.Fatalf("expected 1 delivery, got %d", count)
	}

	if event.DeliveryStatus != "delivered" {
		t.Errorf("expected delivery_status %q, got %q", "delivered", event.DeliveryStatus)
	}

	// Verify delivery attempt recorded
	eventUUID := parseUUID(t, resp.ID)
	attempts, err := slurpee.DB.ListDeliveryAttemptsForEvent(context.Background(), eventUUID)
	if err != nil {
		t.Fatalf("ListDeliveryAttemptsForEvent: %v", err)
	}
	if len(attempts) != 1 {
		t.Fatalf("expected 1 delivery attempt, got %d", len(attempts))
	}
}

func TestPatternMatching_GlobWildcard(t *testing.T) {
	truncateAll(t)
	slurpee := newTestApp(t)
	router := newTestRouter(t, slurpee)

	var mu sync.Mutex
	var receivedCount int

	mockEndpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		receivedCount++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer mockEndpoint.Close()

	secret, plaintext := seedApiSecret(t, slurpee.DB, "test-secret", "my-secret-value", "*")
	subscriber := seedSubscriber(t, slurpee.DB, "test-subscriber", mockEndpoint.URL, "auth-secret")
	seedSubscription(t, slurpee.DB, subscriber.ID, "order.*", nil, nil)

	app.StartDispatcher(slurpee)

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

	event := waitForEventStatus(t, slurpee.DB, resp.ID, "delivered", 10*time.Second)

	mu.Lock()
	count := receivedCount
	mu.Unlock()

	if count != 1 {
		t.Fatalf("expected 1 delivery, got %d", count)
	}

	if event.DeliveryStatus != "delivered" {
		t.Errorf("expected delivery_status %q, got %q", "delivered", event.DeliveryStatus)
	}

	eventUUID := parseUUID(t, resp.ID)
	attempts, err := slurpee.DB.ListDeliveryAttemptsForEvent(context.Background(), eventUUID)
	if err != nil {
		t.Fatalf("ListDeliveryAttemptsForEvent: %v", err)
	}
	if len(attempts) != 1 {
		t.Fatalf("expected 1 delivery attempt, got %d", len(attempts))
	}
}

func TestPatternMatching_WildcardDoesNotMatchUnrelated(t *testing.T) {
	truncateAll(t)
	slurpee := newTestApp(t)
	router := newTestRouter(t, slurpee)

	var mu sync.Mutex
	var receivedCount int

	mockEndpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		receivedCount++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer mockEndpoint.Close()

	secret, plaintext := seedApiSecret(t, slurpee.DB, "test-secret", "my-secret-value", "*")
	subscriber := seedSubscriber(t, slurpee.DB, "test-subscriber", mockEndpoint.URL, "auth-secret")
	// Subscription pattern is order.* — should NOT match user.created
	seedSubscription(t, slurpee.DB, subscriber.ID, "order.*", nil, nil)

	app.StartDispatcher(slurpee)

	// Post an event with subject "user.created" — should NOT match "order.*"
	body := `{"subject":"user.created","data":{"name":"test"}}`
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

	// Event should be recorded (no matching subscriptions means it's marked recorded with no attempts)
	event := waitForEventStatus(t, slurpee.DB, resp.ID, "recorded", 10*time.Second)

	mu.Lock()
	count := receivedCount
	mu.Unlock()

	if count != 0 {
		t.Fatalf("expected 0 deliveries (wildcard should not match unrelated subject), got %d", count)
	}

	if event.DeliveryStatus != "recorded" {
		t.Errorf("expected delivery_status %q, got %q", "recorded", event.DeliveryStatus)
	}

	// Verify no delivery attempts were recorded
	eventUUID := parseUUID(t, resp.ID)
	attempts, err := slurpee.DB.ListDeliveryAttemptsForEvent(context.Background(), eventUUID)
	if err != nil {
		t.Fatalf("ListDeliveryAttemptsForEvent: %v", err)
	}
	if len(attempts) != 0 {
		t.Fatalf("expected 0 delivery attempts, got %d", len(attempts))
	}
}

func TestPatternMatching_FilterMatch(t *testing.T) {
	truncateAll(t)
	slurpee := newTestApp(t)
	router := newTestRouter(t, slurpee)

	var mu sync.Mutex
	var receivedBodies []string

	mockEndpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, _ := io.ReadAll(r.Body)
		mu.Lock()
		receivedBodies = append(receivedBodies, string(bodyBytes))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer mockEndpoint.Close()

	secret, plaintext := seedApiSecret(t, slurpee.DB, "test-secret", "my-secret-value", "*")
	subscriber := seedSubscriber(t, slurpee.DB, "test-subscriber", mockEndpoint.URL, "auth-secret")
	// Subscription with filter: only deliver events where data.type == "premium"
	filter := []byte(`{"type":"premium"}`)
	seedSubscription(t, slurpee.DB, subscriber.ID, "order.*", filter, nil)

	app.StartDispatcher(slurpee)

	// Post event with matching filter data
	body := `{"subject":"order.created","data":{"type":"premium","amount":99.99}}`
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

	event := waitForEventStatus(t, slurpee.DB, resp.ID, "delivered", 10*time.Second)

	mu.Lock()
	bodyCount := len(receivedBodies)
	mu.Unlock()

	if bodyCount != 1 {
		t.Fatalf("expected 1 delivery (filter matches), got %d", bodyCount)
	}

	if event.DeliveryStatus != "delivered" {
		t.Errorf("expected delivery_status %q, got %q", "delivered", event.DeliveryStatus)
	}

	// Verify delivery attempt recorded
	eventUUID := parseUUID(t, resp.ID)
	attempts, err := slurpee.DB.ListDeliveryAttemptsForEvent(context.Background(), eventUUID)
	if err != nil {
		t.Fatalf("ListDeliveryAttemptsForEvent: %v", err)
	}
	if len(attempts) != 1 {
		t.Fatalf("expected 1 delivery attempt, got %d", len(attempts))
	}
}

func TestPatternMatching_FilterMismatch(t *testing.T) {
	truncateAll(t)
	slurpee := newTestApp(t)
	router := newTestRouter(t, slurpee)

	var mu sync.Mutex
	var receivedCount int

	mockEndpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		receivedCount++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer mockEndpoint.Close()

	secret, plaintext := seedApiSecret(t, slurpee.DB, "test-secret", "my-secret-value", "*")
	subscriber := seedSubscriber(t, slurpee.DB, "test-subscriber", mockEndpoint.URL, "auth-secret")
	// Subscription with filter: only deliver events where data.type == "premium"
	filter := []byte(`{"type":"premium"}`)
	seedSubscription(t, slurpee.DB, subscriber.ID, "order.*", filter, nil)

	app.StartDispatcher(slurpee)

	// Post event with NON-matching filter data (type is "basic", not "premium")
	body := `{"subject":"order.created","data":{"type":"basic","amount":49.99}}`
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

	// When filters don't match any subscriptions, dispatchEvent marks event as "failed"
	// because subscriptions were found (subject matched) but none passed filtering.
	// Wait for a terminal status.
	event := waitForEventStatus(t, slurpee.DB, resp.ID, "failed", 10*time.Second)

	mu.Lock()
	count := receivedCount
	mu.Unlock()

	if count != 0 {
		t.Fatalf("expected 0 deliveries (filter mismatch), got %d", count)
	}

	if event.DeliveryStatus != "failed" {
		t.Errorf("expected delivery_status %q, got %q", "failed", event.DeliveryStatus)
	}

	// Verify NO delivery attempts were recorded (filter mismatch means no delivery was attempted)
	eventUUID := parseUUID(t, resp.ID)
	attempts, err := slurpee.DB.ListDeliveryAttemptsForEvent(context.Background(), eventUUID)
	if err != nil {
		t.Fatalf("ListDeliveryAttemptsForEvent: %v", err)
	}
	if len(attempts) != 0 {
		t.Fatalf("expected 0 delivery attempts, got %d", len(attempts))
	}
}

func TestPatternMatching_OnlyMatchedSubscriptionsGetDelivered(t *testing.T) {
	truncateAll(t)
	slurpee := newTestApp(t)
	router := newTestRouter(t, slurpee)

	var mu sync.Mutex
	var receivedBodies []string

	mockEndpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, _ := io.ReadAll(r.Body)
		mu.Lock()
		receivedBodies = append(receivedBodies, string(bodyBytes))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer mockEndpoint.Close()

	secret, plaintext := seedApiSecret(t, slurpee.DB, "test-secret", "my-secret-value", "*")

	// Subscriber A: subscribes to order.* (matches)
	subA := seedSubscriber(t, slurpee.DB, "sub-order", mockEndpoint.URL+"/a", "auth-a")
	seedSubscription(t, slurpee.DB, subA.ID, "order.*", nil, nil)

	// Subscriber B: subscribes to user.* (does NOT match order.created)
	mockEndpointB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		receivedBodies = append(receivedBodies, "UNEXPECTED-B")
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer mockEndpointB.Close()
	subB := seedSubscriber(t, slurpee.DB, "sub-user", mockEndpointB.URL, "auth-b")
	seedSubscription(t, slurpee.DB, subB.ID, "user.*", nil, nil)

	app.StartDispatcher(slurpee)

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

	event := waitForEventStatus(t, slurpee.DB, resp.ID, "delivered", 10*time.Second)

	if event.DeliveryStatus != "delivered" {
		t.Errorf("expected delivery_status %q, got %q", "delivered", event.DeliveryStatus)
	}

	// Verify exactly 1 delivery attempt (only subscriber A matched)
	eventUUID := parseUUID(t, resp.ID)
	attempts, err := slurpee.DB.ListDeliveryAttemptsForEvent(context.Background(), eventUUID)
	if err != nil {
		t.Fatalf("ListDeliveryAttemptsForEvent: %v", err)
	}
	if len(attempts) != 1 {
		t.Fatalf("expected 1 delivery attempt (only order.* subscriber), got %d", len(attempts))
	}

	// Check no "UNEXPECTED-B" bodies were received
	mu.Lock()
	for _, b := range receivedBodies {
		if b == "UNEXPECTED-B" {
			t.Error("subscriber B (user.*) should NOT have received the order.created event")
		}
	}
	mu.Unlock()
}
