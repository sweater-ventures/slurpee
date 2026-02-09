package e2e

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sweater-ventures/slurpee/api"
)

func TestCreateSubscriber_HappyPath(t *testing.T) {
	truncateAll(t)
	slurpee := newTestApp(t)
	router := newTestRouter(t, slurpee)

	body := `{
		"name": "order-service",
		"endpoint_url": "https://example.com/webhooks/orders",
		"auth_secret": "webhook-secret-123",
		"subscriptions": [
			{"subject_pattern": "order.created"},
			{"subject_pattern": "order.updated"}
		]
	}`
	req := httptest.NewRequest("POST", "/api/subscribers", strings.NewReader(body))
	req.Header.Set("X-Slurpee-Admin-Secret", "test-admin-secret")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp api.SubscriberResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.Name != "order-service" {
		t.Errorf("expected name order-service, got %s", resp.Name)
	}
	if resp.EndpointURL != "https://example.com/webhooks/orders" {
		t.Errorf("expected endpoint_url https://example.com/webhooks/orders, got %s", resp.EndpointURL)
	}
	if resp.ID == "" {
		t.Error("expected non-empty subscriber ID")
	}
	if len(resp.Subscriptions) != 2 {
		t.Fatalf("expected 2 subscriptions, got %d", len(resp.Subscriptions))
	}

	// Verify persisted in database
	dbSub, err := slurpee.DB.GetSubscriberByEndpointURL(context.Background(), "https://example.com/webhooks/orders")
	if err != nil {
		t.Fatalf("GetSubscriberByEndpointURL: %v", err)
	}
	if dbSub.Name != "order-service" {
		t.Errorf("DB name: expected order-service, got %s", dbSub.Name)
	}
	if dbSub.AuthSecret != "webhook-secret-123" {
		t.Errorf("DB auth_secret: expected webhook-secret-123, got %s", dbSub.AuthSecret)
	}

	// Verify subscriptions persisted
	subs, err := slurpee.DB.ListSubscriptionsForSubscriber(context.Background(), dbSub.ID)
	if err != nil {
		t.Fatalf("ListSubscriptionsForSubscriber: %v", err)
	}
	if len(subs) != 2 {
		t.Fatalf("expected 2 subscriptions in DB, got %d", len(subs))
	}

	patterns := map[string]bool{}
	for _, s := range subs {
		patterns[s.SubjectPattern] = true
	}
	if !patterns["order.created"] || !patterns["order.updated"] {
		t.Errorf("expected patterns order.created and order.updated, got %v", patterns)
	}
}

func TestCreateSubscriber_Upsert(t *testing.T) {
	truncateAll(t)
	slurpee := newTestApp(t)
	router := newTestRouter(t, slurpee)

	// Create initial subscriber
	body1 := `{
		"name": "order-service",
		"endpoint_url": "https://example.com/webhooks",
		"auth_secret": "secret-1",
		"subscriptions": [
			{"subject_pattern": "order.created"},
			{"subject_pattern": "order.updated"}
		]
	}`
	req := httptest.NewRequest("POST", "/api/subscribers", strings.NewReader(body1))
	req.Header.Set("X-Slurpee-Admin-Secret", "test-admin-secret")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("first create: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp1 api.SubscriberResponse
	json.NewDecoder(rr.Body).Decode(&resp1)

	// Upsert with same endpoint_url but different subscriptions
	body2 := `{
		"name": "order-service-v2",
		"endpoint_url": "https://example.com/webhooks",
		"auth_secret": "secret-2",
		"subscriptions": [
			{"subject_pattern": "order.*"},
			{"subject_pattern": "user.created"},
			{"subject_pattern": "payment.completed"}
		]
	}`
	req = httptest.NewRequest("POST", "/api/subscribers", strings.NewReader(body2))
	req.Header.Set("X-Slurpee-Admin-Secret", "test-admin-secret")
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("upsert: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp2 api.SubscriberResponse
	json.NewDecoder(rr.Body).Decode(&resp2)

	// Same subscriber ID (upserted, not created new)
	if resp2.ID != resp1.ID {
		t.Errorf("expected same subscriber ID after upsert, got %s vs %s", resp1.ID, resp2.ID)
	}

	// Old subscriptions replaced with new ones
	if len(resp2.Subscriptions) != 3 {
		t.Fatalf("expected 3 subscriptions after upsert, got %d", len(resp2.Subscriptions))
	}

	// Verify in DB: only new subscriptions exist
	dbSub, err := slurpee.DB.GetSubscriberByEndpointURL(context.Background(), "https://example.com/webhooks")
	if err != nil {
		t.Fatalf("GetSubscriberByEndpointURL: %v", err)
	}

	subs, err := slurpee.DB.ListSubscriptionsForSubscriber(context.Background(), dbSub.ID)
	if err != nil {
		t.Fatalf("ListSubscriptionsForSubscriber: %v", err)
	}
	if len(subs) != 3 {
		t.Fatalf("expected 3 subscriptions in DB after upsert, got %d", len(subs))
	}

	patterns := map[string]bool{}
	for _, s := range subs {
		patterns[s.SubjectPattern] = true
	}
	if !patterns["order.*"] || !patterns["user.created"] || !patterns["payment.completed"] {
		t.Errorf("expected new patterns after upsert, got %v", patterns)
	}
}

func TestCreateSubscriber_WithFilter(t *testing.T) {
	truncateAll(t)
	slurpee := newTestApp(t)
	router := newTestRouter(t, slurpee)

	body := `{
		"name": "filtered-service",
		"endpoint_url": "https://example.com/webhooks/filtered",
		"auth_secret": "secret-123",
		"subscriptions": [
			{"subject_pattern": "order.created", "filter": {"type": "premium", "region": "us-west"}}
		]
	}`
	req := httptest.NewRequest("POST", "/api/subscribers", strings.NewReader(body))
	req.Header.Set("X-Slurpee-Admin-Secret", "test-admin-secret")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp api.SubscriberResponse
	json.NewDecoder(rr.Body).Decode(&resp)

	if len(resp.Subscriptions) != 1 {
		t.Fatalf("expected 1 subscription, got %d", len(resp.Subscriptions))
	}

	// Verify filter in response
	if resp.Subscriptions[0].Filter == nil {
		t.Fatal("expected filter in response")
	}
	var filter map[string]any
	if err := json.Unmarshal(resp.Subscriptions[0].Filter, &filter); err != nil {
		t.Fatalf("unmarshal filter: %v", err)
	}
	if filter["type"] != "premium" {
		t.Errorf("expected filter type=premium, got %v", filter["type"])
	}
	if filter["region"] != "us-west" {
		t.Errorf("expected filter region=us-west, got %v", filter["region"])
	}

	// Verify filter persisted in DB
	dbSub, err := slurpee.DB.GetSubscriberByEndpointURL(context.Background(), "https://example.com/webhooks/filtered")
	if err != nil {
		t.Fatalf("GetSubscriberByEndpointURL: %v", err)
	}
	subs, err := slurpee.DB.ListSubscriptionsForSubscriber(context.Background(), dbSub.ID)
	if err != nil {
		t.Fatalf("ListSubscriptionsForSubscriber: %v", err)
	}
	if len(subs) != 1 {
		t.Fatalf("expected 1 subscription in DB, got %d", len(subs))
	}
	if len(subs[0].Filter) == 0 {
		t.Fatal("expected filter persisted in DB")
	}
	var dbFilter map[string]any
	if err := json.Unmarshal(subs[0].Filter, &dbFilter); err != nil {
		t.Fatalf("unmarshal DB filter: %v", err)
	}
	if dbFilter["type"] != "premium" {
		t.Errorf("DB filter type: expected premium, got %v", dbFilter["type"])
	}
}

func TestCreateSubscriber_WithMaxRetries(t *testing.T) {
	truncateAll(t)
	slurpee := newTestApp(t)
	router := newTestRouter(t, slurpee)

	body := `{
		"name": "retry-service",
		"endpoint_url": "https://example.com/webhooks/retry",
		"auth_secret": "secret-123",
		"subscriptions": [
			{"subject_pattern": "order.created", "max_retries": 5}
		]
	}`
	req := httptest.NewRequest("POST", "/api/subscribers", strings.NewReader(body))
	req.Header.Set("X-Slurpee-Admin-Secret", "test-admin-secret")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp api.SubscriberResponse
	json.NewDecoder(rr.Body).Decode(&resp)

	if len(resp.Subscriptions) != 1 {
		t.Fatalf("expected 1 subscription, got %d", len(resp.Subscriptions))
	}
	if resp.Subscriptions[0].MaxRetries == nil || *resp.Subscriptions[0].MaxRetries != 5 {
		t.Errorf("expected max_retries=5, got %v", resp.Subscriptions[0].MaxRetries)
	}

	// Verify persisted in DB
	dbSub, err := slurpee.DB.GetSubscriberByEndpointURL(context.Background(), "https://example.com/webhooks/retry")
	if err != nil {
		t.Fatalf("GetSubscriberByEndpointURL: %v", err)
	}
	subs, err := slurpee.DB.ListSubscriptionsForSubscriber(context.Background(), dbSub.ID)
	if err != nil {
		t.Fatalf("ListSubscriptionsForSubscriber: %v", err)
	}
	if len(subs) != 1 {
		t.Fatalf("expected 1 subscription in DB, got %d", len(subs))
	}
	if !subs[0].MaxRetries.Valid || subs[0].MaxRetries.Int32 != 5 {
		t.Errorf("DB max_retries: expected 5, got %v", subs[0].MaxRetries)
	}
}

func TestCreateSubscriber_MissingAdminSecret(t *testing.T) {
	truncateAll(t)
	slurpee := newTestApp(t)
	router := newTestRouter(t, slurpee)

	body := `{
		"name": "test-service",
		"endpoint_url": "https://example.com/webhooks",
		"auth_secret": "secret",
		"subscriptions": [{"subject_pattern": "order.created"}]
	}`
	req := httptest.NewRequest("POST", "/api/subscribers", strings.NewReader(body))
	// No X-Slurpee-Admin-Secret header
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestCreateSubscriber_WrongAdminSecret(t *testing.T) {
	truncateAll(t)
	slurpee := newTestApp(t)
	router := newTestRouter(t, slurpee)

	body := `{
		"name": "test-service",
		"endpoint_url": "https://example.com/webhooks",
		"auth_secret": "secret",
		"subscriptions": [{"subject_pattern": "order.created"}]
	}`
	req := httptest.NewRequest("POST", "/api/subscribers", strings.NewReader(body))
	req.Header.Set("X-Slurpee-Admin-Secret", "wrong-admin-secret")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestCreateSubscriber_MissingName(t *testing.T) {
	truncateAll(t)
	slurpee := newTestApp(t)
	router := newTestRouter(t, slurpee)

	body := `{
		"endpoint_url": "https://example.com/webhooks",
		"auth_secret": "secret",
		"subscriptions": [{"subject_pattern": "order.created"}]
	}`
	req := httptest.NewRequest("POST", "/api/subscribers", strings.NewReader(body))
	req.Header.Set("X-Slurpee-Admin-Secret", "test-admin-secret")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestCreateSubscriber_MissingEndpointURL(t *testing.T) {
	truncateAll(t)
	slurpee := newTestApp(t)
	router := newTestRouter(t, slurpee)

	body := `{
		"name": "test-service",
		"auth_secret": "secret",
		"subscriptions": [{"subject_pattern": "order.created"}]
	}`
	req := httptest.NewRequest("POST", "/api/subscribers", strings.NewReader(body))
	req.Header.Set("X-Slurpee-Admin-Secret", "test-admin-secret")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestCreateSubscriber_MissingAuthSecret(t *testing.T) {
	truncateAll(t)
	slurpee := newTestApp(t)
	router := newTestRouter(t, slurpee)

	body := `{
		"name": "test-service",
		"endpoint_url": "https://example.com/webhooks",
		"subscriptions": [{"subject_pattern": "order.created"}]
	}`
	req := httptest.NewRequest("POST", "/api/subscribers", strings.NewReader(body))
	req.Header.Set("X-Slurpee-Admin-Secret", "test-admin-secret")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestCreateSubscriber_MissingSubscriptions(t *testing.T) {
	truncateAll(t)
	slurpee := newTestApp(t)
	router := newTestRouter(t, slurpee)

	body := `{
		"name": "test-service",
		"endpoint_url": "https://example.com/webhooks",
		"auth_secret": "secret"
	}`
	req := httptest.NewRequest("POST", "/api/subscribers", strings.NewReader(body))
	req.Header.Set("X-Slurpee-Admin-Secret", "test-admin-secret")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestCreateSubscriber_MissingSubjectPattern(t *testing.T) {
	truncateAll(t)
	slurpee := newTestApp(t)
	router := newTestRouter(t, slurpee)

	body := `{
		"name": "test-service",
		"endpoint_url": "https://example.com/webhooks",
		"auth_secret": "secret",
		"subscriptions": [{"filter": {"type": "premium"}}]
	}`
	req := httptest.NewRequest("POST", "/api/subscribers", strings.NewReader(body))
	req.Header.Set("X-Slurpee-Admin-Secret", "test-admin-secret")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}
