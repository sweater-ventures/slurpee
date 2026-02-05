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

// deliverEvent triggers asynchronous delivery of an event to all matching subscribers.
// It runs in a background goroutine and should be called after the event is persisted.
func deliverEvent(app *app.Application, event db.Event) {
	go func() {
		ctx := context.Background()
		logger := slog.Default().With("event_id", uuidToString(event.ID), "subject", event.Subject)

		// Find all subscriptions whose subject_pattern matches the event subject
		subscriptions, err := app.DB.GetSubscriptionsMatchingSubject(ctx, event.Subject)
		if err != nil {
			logger.Error("Failed to find matching subscriptions", "error", err)
			updateEventStatus(ctx, app, event, "failed")
			return
		}

		if len(subscriptions) == 0 {
			logger.Info("No matching subscriptions for event")
			updateEventStatus(ctx, app, event, "delivered")
			return
		}

		// Group subscriptions by subscriber
		subscriberSubs := make(map[pgtype.UUID][]db.Subscription)
		for _, sub := range subscriptions {
			key := sub.SubscriberID
			subscriberSubs[key] = append(subscriberSubs[key], sub)
		}

		// Load subscriber details for each unique subscriber
		subscribers := make(map[pgtype.UUID]db.Subscriber)
		for subID := range subscriberSubs {
			subscriber, err := app.DB.GetSubscriberByID(ctx, subID)
			if err != nil {
				logger.Error("Failed to get subscriber", "error", err, "subscriber_id", uuidToString(subID))
				continue
			}
			subscribers[subID] = subscriber
		}

		// Track results across all deliveries
		var mu sync.Mutex
		allSucceeded := true
		anyAttempted := false

		var wg sync.WaitGroup

		for subID, subs := range subscriberSubs {
			subscriber, ok := subscribers[subID]
			if !ok {
				mu.Lock()
				allSucceeded = false
				mu.Unlock()
				continue
			}

			// Respect max_parallel per subscriber using a semaphore channel
			sem := make(chan struct{}, subscriber.MaxParallel)

			for range subs {
				wg.Add(1)
				anyAttempted = true

				go func(sub db.Subscriber) {
					defer wg.Done()

					sem <- struct{}{}
					defer func() { <-sem }()

					succeeded := deliverToSubscriber(ctx, app, event, sub, logger)

					mu.Lock()
					if !succeeded {
						allSucceeded = false
					}
					mu.Unlock()
				}(subscriber)
			}
		}

		wg.Wait()

		// Determine final delivery status
		var finalStatus string
		if !anyAttempted {
			finalStatus = "failed"
		} else if allSucceeded {
			finalStatus = "delivered"
		} else {
			finalStatus = "failed"
		}

		updateEventStatus(ctx, app, event, finalStatus)
		logger.Info("Event delivery complete", "status", finalStatus)
	}()
}

// deliverToSubscriber sends the event to a single subscriber endpoint and records the delivery attempt.
func deliverToSubscriber(ctx context.Context, app *app.Application, event db.Event, subscriber db.Subscriber, logger *slog.Logger) bool {
	attemptID := pgtype.UUID{Bytes: uuid.Must(uuid.NewV7()), Valid: true}
	now := time.Now().UTC()

	// Build the request body (event data as JSON)
	body := event.Data

	// Build request headers
	reqHeaders := map[string]string{
		"Content-Type":    "application/json",
		"X-Slurpee-Secret": subscriber.AuthSecret,
		"X-Event-ID":     uuidToString(event.ID),
		"X-Event-Subject": event.Subject,
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
		recordFailedAttempt(ctx, app, attemptID, event, subscriber, reqHeadersJSON, now, fmt.Sprintf("request creation failed: %v", err))
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
		recordFailedAttempt(ctx, app, attemptID, event, subscriber, reqHeadersJSON, now, fmt.Sprintf("request failed: %v", err))
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
	_, err = app.DB.InsertDeliveryAttempt(ctx, db.InsertDeliveryAttemptParams{
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
func recordFailedAttempt(ctx context.Context, app *app.Application, attemptID pgtype.UUID, event db.Event, subscriber db.Subscriber, reqHeadersJSON []byte, attemptedAt time.Time, errMsg string) {
	_, err := app.DB.InsertDeliveryAttempt(ctx, db.InsertDeliveryAttemptParams{
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
func updateEventStatus(ctx context.Context, app *app.Application, event db.Event, status string) {
	_, err := app.DB.UpdateEventDeliveryStatus(ctx, db.UpdateEventDeliveryStatusParams{
		DeliveryStatus:  status,
		RetryCount:      event.RetryCount,
		StatusUpdatedAt: pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
		ID:              event.ID,
	})
	if err != nil {
		slog.Error("Failed to update event delivery status", "error", err, "event_id", uuidToString(event.ID))
	}
}
