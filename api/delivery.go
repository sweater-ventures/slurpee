package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/sweater-ventures/slurpee/app"
	"github.com/sweater-ventures/slurpee/db"
)

// StartDispatcher launches the centralized event delivery dispatcher.
// It reads events from app.DeliveryChan and delivers them to matching subscribers,
// enforcing per-subscriber concurrency limits via persistent semaphores.
func StartDispatcher(a *app.Application) {
	var globalWg sync.WaitGroup

	// Persistent per-subscriber semaphores keyed by UUID bytes
	var semMu sync.Mutex
	semaphores := make(map[[16]byte]chan struct{})

	getSemaphore := func(subscriberID [16]byte, maxParallel int32) chan struct{} {
		semMu.Lock()
		defer semMu.Unlock()

		sem, ok := semaphores[subscriberID]
		if !ok || int32(cap(sem)) != maxParallel {
			semaphores[subscriberID] = make(chan struct{}, maxParallel)
			return semaphores[subscriberID]
		}
		return sem
	}

	done := make(chan struct{})

	go func() {
		defer close(done)

		for event := range a.DeliveryChan {
			dispatchEvent(a, event, getSemaphore, &globalWg)
		}

		// Channel closed — wait for all in-flight deliveries
		slog.Info("Delivery channel closed, waiting for in-flight deliveries")
		globalWg.Wait()
		slog.Info("All deliveries complete")
	}()

	a.SetStopDelivery(func() {
		close(a.DeliveryChan)
		<-done
	})
}

// dispatchEvent finds matching subscriptions for an event and spawns delivery workers.
func dispatchEvent(a *app.Application, event db.Event, getSemaphore func([16]byte, int32) chan struct{}, globalWg *sync.WaitGroup) {
	ctx := context.Background()
	logger := slog.Default().With("event_id", uuidToString(event.ID), "subject", event.Subject)

	// Find all subscriptions whose subject_pattern matches the event subject
	subscriptions, err := a.DB.GetSubscriptionsMatchingSubject(ctx, event.Subject)
	if err != nil {
		logger.Error("Failed to find matching subscriptions", "error", err)
		updateEventStatus(ctx, a, event, "failed")
		return
	}

	if len(subscriptions) == 0 {
		logger.Info("No matching subscriptions for event")
		updateEventStatus(ctx, a, event, "delivered")
		return
	}

	// Group subscriptions by subscriber
	subscriberSubs := make(map[pgtype.UUID][]db.Subscription)
	for _, sub := range subscriptions {
		subscriberSubs[sub.SubscriberID] = append(subscriberSubs[sub.SubscriberID], sub)
	}

	// Load subscriber details for each unique subscriber
	subscribers := make(map[pgtype.UUID]db.Subscriber)
	for subID := range subscriberSubs {
		subscriber, err := a.DB.GetSubscriberByID(ctx, subID)
		if err != nil {
			logger.Error("Failed to get subscriber", "error", err, "subscriber_id", uuidToString(subID))
			continue
		}
		subscribers[subID] = subscriber
	}

	// Track results across all deliveries for this event
	var mu sync.Mutex
	allSucceeded := true
	anyAttempted := false

	var eventWg sync.WaitGroup

	for subID, subs := range subscriberSubs {
		subscriber, ok := subscribers[subID]
		if !ok {
			mu.Lock()
			allSucceeded = false
			mu.Unlock()
			continue
		}

		sem := getSemaphore(subscriber.ID.Bytes, subscriber.MaxParallel)

		for range subs {
			eventWg.Add(1)
			globalWg.Add(1)
			anyAttempted = true

			go func(sub db.Subscriber, sem chan struct{}) {
				defer eventWg.Done()
				defer globalWg.Done()

				sem <- struct{}{}
				defer func() { <-sem }()

				succeeded := deliverToSubscriber(ctx, a, event, sub, logger)

				mu.Lock()
				if !succeeded {
					allSucceeded = false
				}
				mu.Unlock()
			}(subscriber, sem)
		}
	}

	// Wait for all deliveries for this event in a separate goroutine so dispatcher can continue
	globalWg.Add(1)
	go func() {
		defer globalWg.Done()
		eventWg.Wait()

		var finalStatus string
		if !anyAttempted {
			finalStatus = "failed"
		} else if allSucceeded {
			finalStatus = "delivered"
		} else {
			finalStatus = "failed"
		}

		updateEventStatus(ctx, a, event, finalStatus)
		logger.Info("Event delivery complete", "status", finalStatus)
	}()
}

// deliverToSubscriber sends the event to a single subscriber endpoint and records the delivery attempt.
func deliverToSubscriber(ctx context.Context, a *app.Application, event db.Event, subscriber db.Subscriber, logger *slog.Logger) bool {
	attemptID := pgtype.UUID{Bytes: uuid.Must(uuid.NewV7()), Valid: true}
	now := time.Now().UTC()

	// Build the request body (event data as JSON)
	body := event.Data

	// Build request headers
	reqHeaders := map[string]string{
		"Content-Type":      "application/json",
		"X-Slurpee-Secret":  subscriber.AuthSecret,
		"X-Event-ID":        uuidToString(event.ID),
		"X-Event-Subject":   event.Subject,
	}
	reqHeadersJSON, _ := json.Marshal(reqHeaders)

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, subscriber.EndpointUrl, bytes.NewReader(body))
	if err != nil {
		logger.Error("Failed to create delivery request",
			"error", err,
			"subscriber_id", uuidToString(subscriber.ID),
			"endpoint_url", subscriber.EndpointUrl,
		)
		recordFailedAttempt(ctx, a, attemptID, event, subscriber, reqHeadersJSON, now, fmt.Sprintf("request creation failed: %v", err))
		return false
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Slurpee-Secret", subscriber.AuthSecret)
	req.Header.Set("X-Event-ID", uuidToString(event.ID))
	req.Header.Set("X-Event-Subject", event.Subject)

	// Send the request
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		logger.Error("Delivery request failed",
			"error", err,
			"subscriber_id", uuidToString(subscriber.ID),
			"endpoint_url", subscriber.EndpointUrl,
		)
		recordFailedAttempt(ctx, a, attemptID, event, subscriber, reqHeadersJSON, now, fmt.Sprintf("request failed: %v", err))
		return false
	}
	defer resp.Body.Close()

	// Read response
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024)) // Limit to 1MB
	respHeadersJSON, _ := json.Marshal(resp.Header)

	// Determine status based on response code
	status := "succeeded"
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		status = "failed"
	}

	// Record the delivery attempt
	_, err = a.DB.InsertDeliveryAttempt(ctx, db.InsertDeliveryAttemptParams{
		ID:                 attemptID,
		EventID:            event.ID,
		SubscriberID:       subscriber.ID,
		EndpointUrl:        subscriber.EndpointUrl,
		AttemptedAt:        pgtype.Timestamptz{Time: now, Valid: true},
		RequestHeaders:     reqHeadersJSON,
		ResponseStatusCode: pgtype.Int4{Int32: int32(resp.StatusCode), Valid: true},
		ResponseHeaders:    respHeadersJSON,
		ResponseBody:       string(respBody),
		Status:             status,
	})
	if err != nil {
		logger.Error("Failed to record delivery attempt", "error", err, "subscriber_id", uuidToString(subscriber.ID))
	}

	if status == "succeeded" {
		logger.Info("Delivery succeeded",
			"subscriber_id", uuidToString(subscriber.ID),
			"endpoint_url", subscriber.EndpointUrl,
			"status_code", resp.StatusCode,
		)
	} else {
		logger.Warn("Delivery failed",
			"subscriber_id", uuidToString(subscriber.ID),
			"endpoint_url", subscriber.EndpointUrl,
			"status_code", resp.StatusCode,
		)
	}

	return status == "succeeded"
}

// recordFailedAttempt records a delivery attempt that failed before getting a response.
func recordFailedAttempt(ctx context.Context, a *app.Application, attemptID pgtype.UUID, event db.Event, subscriber db.Subscriber, reqHeadersJSON []byte, attemptedAt time.Time, errMsg string) {
	_, err := a.DB.InsertDeliveryAttempt(ctx, db.InsertDeliveryAttemptParams{
		ID:             attemptID,
		EventID:        event.ID,
		SubscriberID:   subscriber.ID,
		EndpointUrl:    subscriber.EndpointUrl,
		AttemptedAt:    pgtype.Timestamptz{Time: attemptedAt, Valid: true},
		RequestHeaders: reqHeadersJSON,
		ResponseBody:   errMsg,
		Status:         "failed",
	})
	if err != nil {
		slog.Error("Failed to record failed delivery attempt", "error", err, "subscriber_id", uuidToString(subscriber.ID))
	}
}

// updateEventStatus updates the delivery_status and status_updated_at on an event.
func updateEventStatus(ctx context.Context, a *app.Application, event db.Event, status string) {
	_, err := a.DB.UpdateEventDeliveryStatus(ctx, db.UpdateEventDeliveryStatusParams{
		DeliveryStatus:  status,
		RetryCount:      event.RetryCount,
		StatusUpdatedAt: pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
		ID:              event.ID,
	})
	if err != nil {
		slog.Error("Failed to update event delivery status", "error", err, "event_id", uuidToString(event.ID))
	}
}
