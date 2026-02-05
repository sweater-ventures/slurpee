package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/sweater-ventures/slurpee/app"
	"github.com/sweater-ventures/slurpee/db"
)

// deliveryTask represents a pending delivery with retry state.
type deliveryTask struct {
	event        db.Event
	subscription db.Subscription
	subscriber   db.Subscriber
	attemptNum   int // 0-indexed attempt number (0 = first attempt)
	maxRetries   int // effective max retries for this subscription
}

// deliveryResult represents the final outcome of a delivery to a subscription.
type deliveryResult struct {
	subscriptionID pgtype.UUID
	succeeded      bool
	exhausted      bool // true if max retries exhausted without success
}

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
		updateEventStatus(ctx, a, event.ID, event.RetryCount, "failed")
		return
	}

	if len(subscriptions) == 0 {
		logger.Info("No matching subscriptions for event")
		updateEventStatus(ctx, a, event.ID, event.RetryCount, "delivered")
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

	// Build initial delivery tasks
	var tasks []deliveryTask
	for subID, subs := range subscriberSubs {
		subscriber, ok := subscribers[subID]
		if !ok {
			continue
		}

		for _, sub := range subs {
			// Check subscription filter against event data
			if !matchesFilter(sub.Filter, event.Data) {
				logger.Debug("Subscription filter did not match event data",
					"subscriber_id", uuidToString(subscriber.ID),
					"subscription_id", uuidToString(sub.ID),
					"subject_pattern", sub.SubjectPattern,
				)
				continue // Skip this subscription - filter doesn't match
			}

			// Determine effective max retries: per-subscription override or global default
			maxRetries := a.Config.MaxRetries
			if sub.MaxRetries.Valid {
				maxRetries = int(sub.MaxRetries.Int32)
			}

			tasks = append(tasks, deliveryTask{
				event:        event,
				subscription: sub,
				subscriber:   subscriber,
				attemptNum:   0,
				maxRetries:   maxRetries,
			})
		}
	}

	if len(tasks) == 0 {
		logger.Warn("No valid subscribers found for matching subscriptions")
		updateEventStatus(ctx, a, event.ID, event.RetryCount, "failed")
		return
	}

	resultsChan := make(chan deliveryResult, len(tasks))

	// Track all in-flight tasks for this event
	var eventWg sync.WaitGroup

	// Process initial deliveries
	for _, task := range tasks {
		eventWg.Add(1)
		globalWg.Add(1)

		go func(t deliveryTask) {
			defer eventWg.Done()
			defer globalWg.Done()

			processDeliveryTask(ctx, a, t, getSemaphore, globalWg, resultsChan, logger)
		}(task)
	}

	// Wait for all deliveries and determine final status
	globalWg.Add(1)
	go func() {
		defer globalWg.Done()
		eventWg.Wait()
		close(resultsChan)

		// Collect results
		results := make(map[[16]byte]deliveryResult)
		for r := range resultsChan {
			// Only keep the final result for each subscription
			results[r.subscriptionID.Bytes] = r
		}

		// Determine final status
		allSucceeded := true
		anyFailed := false
		for _, r := range results {
			if !r.succeeded {
				allSucceeded = false
				if r.exhausted {
					anyFailed = true
				}
			}
		}

		var finalStatus string
		if allSucceeded {
			finalStatus = "delivered"
		} else if anyFailed {
			finalStatus = "failed"
		} else {
			// This shouldn't happen in normal flow but handle it
			finalStatus = "failed"
		}

		updateEventStatus(ctx, a, event.ID, event.RetryCount, finalStatus)
		logger.Info("Event delivery complete", "status", finalStatus)
	}()
}

// processDeliveryTask handles a single delivery attempt with retry logic.
func processDeliveryTask(
	ctx context.Context,
	a *app.Application,
	task deliveryTask,
	getSemaphore func([16]byte, int32) chan struct{},
	globalWg *sync.WaitGroup,
	resultsChan chan<- deliveryResult,
	logger *slog.Logger,
) {
	sem := getSemaphore(task.subscriber.ID.Bytes, task.subscriber.MaxParallel)

	// Acquire semaphore
	sem <- struct{}{}
	defer func() { <-sem }()

	succeeded := deliverToSubscriber(ctx, a, task.event, task.subscriber, task.attemptNum, logger)

	if succeeded {
		resultsChan <- deliveryResult{
			subscriptionID: task.subscription.ID,
			succeeded:      true,
			exhausted:      false,
		}
		return
	}

	// Delivery failed - check if we should retry
	if task.attemptNum >= task.maxRetries {
		// Max retries exhausted
		logger.Warn("Max retries exhausted",
			"subscriber_id", uuidToString(task.subscriber.ID),
			"endpoint_url", task.subscriber.EndpointUrl,
			"attempt", task.attemptNum+1,
			"max_retries", task.maxRetries,
		)
		resultsChan <- deliveryResult{
			subscriptionID: task.subscription.ID,
			succeeded:      false,
			exhausted:      true,
		}
		return
	}

	// Schedule retry with exponential backoff
	delay := calculateBackoff(task.attemptNum, a.Config.MaxBackoffSeconds)
	logger.Info("Scheduling retry",
		"subscriber_id", uuidToString(task.subscriber.ID),
		"endpoint_url", task.subscriber.EndpointUrl,
		"attempt", task.attemptNum+1,
		"next_attempt", task.attemptNum+2,
		"delay_seconds", delay.Seconds(),
	)

	// Update event status to partial while retrying
	updateEventStatus(ctx, a, task.event.ID, task.event.RetryCount+1, "partial")

	// Increment retry count on the event
	task.event.RetryCount++

	// Schedule the retry in a new goroutine
	globalWg.Add(1)
	go func() {
		defer globalWg.Done()

		// Wait for backoff delay
		time.Sleep(delay)

		// Create next attempt task
		nextTask := deliveryTask{
			event:        task.event,
			subscription: task.subscription,
			subscriber:   task.subscriber,
			attemptNum:   task.attemptNum + 1,
			maxRetries:   task.maxRetries,
		}

		// Process the retry
		processDeliveryTask(ctx, a, nextTask, getSemaphore, globalWg, resultsChan, logger)
	}()
}

// matchesFilter evaluates whether an event's data matches a subscription's filter.
// Filter is a JSON object of key-value pairs; all pairs must match (AND logic) against top-level keys in event data.
// If filter is nil or empty, returns true (match all).
func matchesFilter(filter []byte, eventData []byte) bool {
	// If filter is nil or empty, match all events
	if len(filter) == 0 {
		return true
	}

	// Parse filter JSON
	var filterObj map[string]interface{}
	if err := json.Unmarshal(filter, &filterObj); err != nil {
		// Invalid filter JSON - treat as non-match for safety
		return false
	}

	// Empty filter object matches all
	if len(filterObj) == 0 {
		return true
	}

	// Parse event data JSON
	var dataObj map[string]interface{}
	if err := json.Unmarshal(eventData, &dataObj); err != nil {
		// Can't parse event data - no match
		return false
	}

	// Check each filter key-value pair (AND logic)
	for key, filterValue := range filterObj {
		dataValue, exists := dataObj[key]
		if !exists {
			return false
		}

		// Compare values - use JSON comparison for type-safe equality
		filterJSON, _ := json.Marshal(filterValue)
		dataJSON, _ := json.Marshal(dataValue)

		if string(filterJSON) != string(dataJSON) {
			return false
		}
	}

	return true
}

// calculateBackoff returns the delay duration for exponential backoff.
// Base delay is 1 second, doubling each attempt, capped at maxBackoffSeconds.
func calculateBackoff(attemptNum int, maxBackoffSeconds int) time.Duration {
	// Exponential backoff: 1s, 2s, 4s, 8s, 16s, ...
	delaySeconds := math.Pow(2, float64(attemptNum))
	if delaySeconds > float64(maxBackoffSeconds) {
		delaySeconds = float64(maxBackoffSeconds)
	}
	return time.Duration(delaySeconds) * time.Second
}

// deliverToSubscriber sends the event to a single subscriber endpoint and records the delivery attempt.
func deliverToSubscriber(ctx context.Context, a *app.Application, event db.Event, subscriber db.Subscriber, attemptNum int, logger *slog.Logger) bool {
	attemptID := pgtype.UUID{Bytes: uuid.Must(uuid.NewV7()), Valid: true}
	now := time.Now().UTC()

	// Build the request body (event data as JSON)
	body := event.Data

	// Build request headers
	reqHeaders := map[string]string{
		"Content-Type":     "application/json",
		"X-Slurpee-Secret": subscriber.AuthSecret,
		"X-Event-ID":       uuidToString(event.ID),
		"X-Event-Subject":  event.Subject,
	}
	reqHeadersJSON, _ := json.Marshal(reqHeaders)

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, subscriber.EndpointUrl, bytes.NewReader(body))
	if err != nil {
		logger.Error("Failed to create delivery request",
			"error", err,
			"subscriber_id", uuidToString(subscriber.ID),
			"endpoint_url", subscriber.EndpointUrl,
			"attempt", attemptNum+1,
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
			"attempt", attemptNum+1,
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
			"attempt", attemptNum+1,
		)
	} else {
		logger.Warn("Delivery failed",
			"subscriber_id", uuidToString(subscriber.ID),
			"endpoint_url", subscriber.EndpointUrl,
			"status_code", resp.StatusCode,
			"attempt", attemptNum+1,
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

// ReplayToSubscriber delivers an event to a single subscriber as a replay.
// It resets the event status to pending, performs a single delivery attempt, and updates the event status.
func ReplayToSubscriber(a *app.Application, event db.Event, subscriber db.Subscriber) {
	ctx := context.Background()
	logger := slog.Default().With("event_id", uuidToString(event.ID), "subject", event.Subject, "replay", true)

	updateEventStatus(ctx, a, event.ID, event.RetryCount, "pending")

	succeeded := deliverToSubscriber(ctx, a, event, subscriber, 0, logger)

	if succeeded {
		updateEventStatus(ctx, a, event.ID, event.RetryCount, "delivered")
		logger.Info("Replay delivery succeeded", "subscriber_id", uuidToString(subscriber.ID))
	} else {
		updateEventStatus(ctx, a, event.ID, event.RetryCount, "failed")
		logger.Warn("Replay delivery failed", "subscriber_id", uuidToString(subscriber.ID))
	}
}

// updateEventStatus updates the delivery_status, retry_count, and status_updated_at on an event.
func updateEventStatus(ctx context.Context, a *app.Application, eventID pgtype.UUID, retryCount int32, status string) {
	_, err := a.DB.UpdateEventDeliveryStatus(ctx, db.UpdateEventDeliveryStatusParams{
		DeliveryStatus:  status,
		RetryCount:      retryCount,
		StatusUpdatedAt: pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
		ID:              eventID,
	})
	if err != nil {
		slog.Error("Failed to update event delivery status", "error", err, "event_id", uuidToString(eventID))
	}
}
