package e2e

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sweater-ventures/slurpee/api"
)

// createSubscriberViaAPI is a helper to create a subscriber via POST /api/subscribers.
func createSubscriberViaAPI(t *testing.T, router *http.ServeMux, name, endpointURL, authSecret string, subscriptions string) api.SubscriberResponse {
	t.Helper()
	body := `{
		"name": "` + name + `",
		"endpoint_url": "` + endpointURL + `",
		"auth_secret": "` + authSecret + `",
		"subscriptions": ` + subscriptions + `
	}`
	req := httptest.NewRequest("POST", "/api/subscribers", strings.NewReader(body))
	req.Header.Set("X-Slurpee-Admin-Secret", "test-admin-secret")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("createSubscriberViaAPI: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp api.SubscriberResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("createSubscriberViaAPI decode: %v", err)
	}
	return resp
}

func TestListSubscribers_HappyPath(t *testing.T) {
	truncateAll(t)
	slurpee := newTestApp(t)
	router := newTestRouter(t, slurpee)

	// Create two subscribers with different subscriptions
	sub1 := createSubscriberViaAPI(t, router, "order-service", "https://example.com/webhooks/orders", "secret-1",
		`[{"subject_pattern": "order.created"}, {"subject_pattern": "order.updated"}]`)
	sub2 := createSubscriberViaAPI(t, router, "user-service", "https://example.com/webhooks/users", "secret-2",
		`[{"subject_pattern": "user.created"}]`)

	// GET /subscribers
	req := httptest.NewRequest("GET", "/api/subscribers", nil)
	req.Header.Set("X-Slurpee-Admin-Secret", "test-admin-secret")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp []api.SubscriberResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(resp) != 2 {
		t.Fatalf("expected 2 subscribers, got %d", len(resp))
	}

	// Build map by ID for easier assertion
	byID := map[string]api.SubscriberResponse{}
	for _, s := range resp {
		byID[s.ID] = s
	}

	// Verify first subscriber
	s1, ok := byID[sub1.ID]
	if !ok {
		t.Fatalf("subscriber %s not found in response", sub1.ID)
	}
	if s1.Name != "order-service" {
		t.Errorf("sub1 name: expected order-service, got %s", s1.Name)
	}
	if s1.EndpointURL != "https://example.com/webhooks/orders" {
		t.Errorf("sub1 endpoint_url: expected https://example.com/webhooks/orders, got %s", s1.EndpointURL)
	}
	if len(s1.Subscriptions) != 2 {
		t.Errorf("sub1 subscriptions: expected 2, got %d", len(s1.Subscriptions))
	}

	// Verify second subscriber
	s2, ok := byID[sub2.ID]
	if !ok {
		t.Fatalf("subscriber %s not found in response", sub2.ID)
	}
	if s2.Name != "user-service" {
		t.Errorf("sub2 name: expected user-service, got %s", s2.Name)
	}
	if s2.EndpointURL != "https://example.com/webhooks/users" {
		t.Errorf("sub2 endpoint_url: expected https://example.com/webhooks/users, got %s", s2.EndpointURL)
	}
	if len(s2.Subscriptions) != 1 {
		t.Errorf("sub2 subscriptions: expected 1, got %d", len(s2.Subscriptions))
	}
	if len(s2.Subscriptions) > 0 && s2.Subscriptions[0].SubjectPattern != "user.created" {
		t.Errorf("sub2 subscription pattern: expected user.created, got %s", s2.Subscriptions[0].SubjectPattern)
	}
}

func TestListSubscribers_EmptyList(t *testing.T) {
	truncateAll(t)
	slurpee := newTestApp(t)
	router := newTestRouter(t, slurpee)

	req := httptest.NewRequest("GET", "/api/subscribers", nil)
	req.Header.Set("X-Slurpee-Admin-Secret", "test-admin-secret")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// Capture raw body before decoding
	raw := strings.TrimSpace(rr.Body.String())

	var resp []api.SubscriberResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(resp) != 0 {
		t.Fatalf("expected empty array, got %d subscribers", len(resp))
	}

	// Verify it's an empty array [], not null
	if raw != "[]" {
		t.Errorf("expected JSON to be [], got %s", raw)
	}
}

func TestListSubscribers_MissingAdminSecret(t *testing.T) {
	truncateAll(t)
	slurpee := newTestApp(t)
	router := newTestRouter(t, slurpee)

	req := httptest.NewRequest("GET", "/api/subscribers", nil)
	// No X-Slurpee-Admin-Secret header
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rr.Code, rr.Body.String())
	}
}
