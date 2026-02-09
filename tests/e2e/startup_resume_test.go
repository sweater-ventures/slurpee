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
