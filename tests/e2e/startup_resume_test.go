package e2e

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/sweater-ventures/slurpee/app"
	"github.com/sweater-ventures/slurpee/db"
)

func TestStartupResume_PendingEvent(t *testing.T) {
	truncateAll(t)
	slurpee := newTestApp(t)

	// Start a mock subscriber endpoint that records received requests
	var requestCount atomic.Int32
	var receivedBody []byte

	mockEndpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		receivedBody = body
		requestCount.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer mockEndpoint.Close()

	// Seed subscriber and subscription
	subscriber := seedSubscriber(t, slurpee.DB, "resume-sub", mockEndpoint.URL, "auth-secret")
	seedSubscription(t, slurpee.DB, subscriber.ID, "order.*", nil, nil)

	// Insert event directly into the database with delivery_status='pending'
	// (simulating an event that was created but never dispatched before shutdown)
	eventID := newUUID()
	eventData := []byte(`{"item":"widget","qty":5}`)
	now := time.Now().UTC()
	_, err := slurpee.DB.InsertEvent(context.Background(), db.InsertEventParams{
		ID:              eventID,
		Subject:         "order.created",
		Timestamp:       pgtype.Timestamptz{Time: now, Valid: true},
		TraceID:         newUUID(),
		Data:            eventData,
		RetryCount:      0,
		DeliveryStatus:  "pending",
		StatusUpdatedAt: pgtype.Timestamptz{Time: now, Valid: true},
	})
	if err != nil {
		t.Fatalf("InsertEvent: %v", err)
	}

	// Start the dispatcher (required for delivery workers)
	ds := app.StartDispatcher(slurpee)

	// Call ResumeUnfinishedDeliveries to trigger re-delivery
	app.ResumeUnfinishedDeliveries(slurpee, ds)

	// Wait for the event to be delivered
	eventIDStr := app.UuidToString(eventID)
	event := waitForEventStatus(t, slurpee.DB, eventIDStr, "delivered", 10*time.Second)

	// Verify the event was delivered
	if event.DeliveryStatus != "delivered" {
		t.Errorf("expected delivery_status='delivered', got %q", event.DeliveryStatus)
	}

	// Verify the mock endpoint received exactly one request
	if got := requestCount.Load(); got != 1 {
		t.Errorf("expected 1 request to mock endpoint, got %d", got)
	}

	// Verify the body matches
	var sentData, receivedData map[string]interface{}
	json.Unmarshal(eventData, &sentData)
	json.Unmarshal(receivedBody, &receivedData)
	if sentData["item"] != receivedData["item"] {
		t.Errorf("body mismatch: sent %v, received %v", sentData, receivedData)
	}

	// Verify a delivery_attempts row was recorded with status 'succeeded'
	attempts, err := slurpee.DB.ListDeliveryAttemptsForEvent(context.Background(), eventID)
	if err != nil {
		t.Fatalf("ListDeliveryAttemptsForEvent: %v", err)
	}
	if len(attempts) == 0 {
		t.Fatal("expected at least one delivery attempt")
	}
	found := false
	for _, a := range attempts {
		if a.Status == "succeeded" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected at least one delivery_attempt with status='succeeded'")
	}
}

func TestStartupResume_PartialEvent(t *testing.T) {
	truncateAll(t)
	slurpee := newTestApp(t)

	// Subscriber A: already succeeded — should NOT receive another delivery
	var subARequestCount atomic.Int32
	mockEndpointA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		subARequestCount.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer mockEndpointA.Close()

	// Subscriber B: had 2 prior failures — should receive a new delivery attempt
	var subBRequestCount atomic.Int32
	mockEndpointB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		subBRequestCount.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer mockEndpointB.Close()

	// Seed subscribers
	subscriberA := seedSubscriber(t, slurpee.DB, "sub-a-succeeded", mockEndpointA.URL, "auth-a")
	subscriberB := seedSubscriber(t, slurpee.DB, "sub-b-failing", mockEndpointB.URL, "auth-b")

	// Seed subscriptions — give subscriber B enough max_retries to allow the 3rd attempt
	maxRetries := int32(5)
	seedSubscription(t, slurpee.DB, subscriberA.ID, "order.*", nil, nil)
	seedSubscription(t, slurpee.DB, subscriberB.ID, "order.*", nil, &maxRetries)

	// Insert event with delivery_status='partial'
	eventID := newUUID()
	eventData := []byte(`{"item":"gadget","qty":3}`)
	now := time.Now().UTC()
	_, err := slurpee.DB.InsertEvent(context.Background(), db.InsertEventParams{
		ID:              eventID,
		Subject:         "order.created",
		Timestamp:       pgtype.Timestamptz{Time: now, Valid: true},
		TraceID:         newUUID(),
		Data:            eventData,
		RetryCount:      2,
		DeliveryStatus:  "partial",
		StatusUpdatedAt: pgtype.Timestamptz{Time: now, Valid: true},
	})
	if err != nil {
		t.Fatalf("InsertEvent: %v", err)
	}

	// Insert delivery_attempts: one succeeded for subscriber A
	_, err = slurpee.DB.InsertDeliveryAttempt(context.Background(), db.InsertDeliveryAttemptParams{
		ID:                 newUUID(),
		EventID:            eventID,
		SubscriberID:       subscriberA.ID,
		EndpointUrl:        subscriberA.EndpointUrl,
		AttemptedAt:        pgtype.Timestamptz{Time: now.Add(-2 * time.Minute), Valid: true},
		RequestHeaders:     []byte(`{}`),
		ResponseStatusCode: pgtype.Int4{Int32: 200, Valid: true},
		ResponseHeaders:    []byte(`{}`),
		ResponseBody:       "ok",
		Status:             "succeeded",
	})
	if err != nil {
		t.Fatalf("InsertDeliveryAttempt (A succeeded): %v", err)
	}

	// Insert delivery_attempts: two failed for subscriber B
	for i := 0; i < 2; i++ {
		_, err = slurpee.DB.InsertDeliveryAttempt(context.Background(), db.InsertDeliveryAttemptParams{
			ID:                 newUUID(),
			EventID:            eventID,
			SubscriberID:       subscriberB.ID,
			EndpointUrl:        subscriberB.EndpointUrl,
			AttemptedAt:        pgtype.Timestamptz{Time: now.Add(-time.Duration(2-i) * time.Minute), Valid: true},
			RequestHeaders:     []byte(`{}`),
			ResponseStatusCode: pgtype.Int4{Int32: 500, Valid: true},
			ResponseHeaders:    []byte(`{}`),
			ResponseBody:       "error",
			Status:             "failed",
		})
		if err != nil {
			t.Fatalf("InsertDeliveryAttempt (B failed #%d): %v", i+1, err)
		}
	}

	// Start the dispatcher
	ds := app.StartDispatcher(slurpee)

	// Call ResumeUnfinishedDeliveries
	app.ResumeUnfinishedDeliveries(slurpee, ds)

	// Wait for the event to reach a final status
	eventIDStr := app.UuidToString(eventID)
	event := waitForEventStatus(t, slurpee.DB, eventIDStr, "delivered", 10*time.Second)

	if event.DeliveryStatus != "delivered" {
		t.Errorf("expected delivery_status='delivered', got %q", event.DeliveryStatus)
	}

	// Verify subscriber A's endpoint was NOT called (already succeeded)
	if got := subARequestCount.Load(); got != 0 {
		t.Errorf("subscriber A should not receive any requests, got %d", got)
	}

	// Verify subscriber B's endpoint received exactly one new delivery
	if got := subBRequestCount.Load(); got != 1 {
		t.Errorf("subscriber B should receive exactly 1 request, got %d", got)
	}

	// Verify subscriber B's delivery_attempts: should be 3 total (2 prior failed + 1 new succeeded)
	attempts, err := slurpee.DB.ListDeliveryAttemptsForEvent(context.Background(), eventID)
	if err != nil {
		t.Fatalf("ListDeliveryAttemptsForEvent: %v", err)
	}

	var subBAttempts []db.DeliveryAttempt
	for _, a := range attempts {
		if a.SubscriberID == subscriberB.ID {
			subBAttempts = append(subBAttempts, a)
		}
	}

	if len(subBAttempts) != 3 {
		t.Errorf("expected 3 delivery_attempts for subscriber B (2 prior + 1 new), got %d", len(subBAttempts))
	}

	// Verify the newest attempt for subscriber B succeeded
	var newestAttempt db.DeliveryAttempt
	for _, a := range subBAttempts {
		if !newestAttempt.AttemptedAt.Valid || a.AttemptedAt.Time.After(newestAttempt.AttemptedAt.Time) {
			newestAttempt = a
		}
	}
	if newestAttempt.Status != "succeeded" {
		t.Errorf("newest attempt for subscriber B should be 'succeeded', got %q", newestAttempt.Status)
	}
}
