package app

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
	"github.com/sweater-ventures/slurpee/db"
)

// deliveryTask represents a pending delivery with retry state.
type deliveryTask struct {
	event        db.Event
	subscription db.Subscription
	subscriber   db.Subscriber
	attemptNum   int // 0-indexed attempt number (0 = first attempt)
	maxRetries   int // effective max retries for this subscription
	tracker      *eventTracker
}

// deliveryResult represents the final outcome of a delivery to a subscription.
type deliveryResult struct {
	subscriptionID pgtype.UUID
	succeeded      bool
	exhausted      bool // true if max retries exhausted without success
}

// eventTracker collects delivery results for a single event.
// Workers write results directly; no collector goroutine needed.
type eventTracker struct {
	event    db.Event
	expected int
	results  map[[16]byte]deliveryResult
	mu       sync.Mutex
	logger   *slog.Logger
}

// record stores a delivery result and returns true exactly once —
// when all expected results have been collected.
func (t *eventTracker) record(r deliveryResult) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.results[r.subscriptionID.Bytes] = r
	return len(t.results) == t.expected
}

// eventRegistry is a concurrency-safe map of in-flight event trackers.
type eventRegistry struct {
	mu       sync.Mutex
	trackers map[[16]byte]*eventTracker
}

func newEventRegistry() *eventRegistry {
	return &eventRegistry{trackers: make(map[[16]byte]*eventTracker)}
}

func (r *eventRegistry) register(id [16]byte, t *eventTracker) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.trackers[id] = t
}

func (r *eventRegistry) remove(id [16]byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.trackers, id)
}

// DispatcherState holds references to the internal dispatcher state needed by
// ResumeUnfinishedDeliveries to enqueue partial-event delivery tasks directly.
type DispatcherState struct {
	inflightWg *sync.WaitGroup
	taskQueue  chan<- deliveryTask
	registry   *eventRegistry
}

// StartDispatcher launches the centralized event delivery dispatcher.
// It reads events from app.DeliveryChan and delivers them to matching subscribers
// using a fixed-size worker pool with non-blocking retries.
// Returns a DispatcherState for use by ResumeUnfinishedDeliveries.
func StartDispatcher(slurpee *Application) *DispatcherState {
	shutdownCtx, shutdownCancel := context.WithCancel(context.Background())

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

	taskQueue := make(chan deliveryTask, slurpee.Config.DeliveryQueueSize)
	registry := newEventRegistry()

	var inflightWg sync.WaitGroup
	var workerWg sync.WaitGroup

	// Start worker goroutines
	numWorkers := slurpee.Config.DeliveryWorkers
	workerWg.Add(numWorkers)
	for i := 0; i < numWorkers; i++ {
		go func() {
			defer workerWg.Done()
			for task := range taskQueue {
				processDeliveryTask(shutdownCtx, slurpee, task, getSemaphore, &inflightWg, taskQueue, registry)
			}
		}()
	}

	done := make(chan struct{})

	// Dispatcher goroutine: reads events from DeliveryChan and dispatches them
	go func() {
		defer close(done)

		for event := range slurpee.DeliveryChan {
			dispatchEvent(slurpee, event, &inflightWg, taskQueue, registry)
		}

		// Channel closed — wait for all in-flight tasks (including pending retry timers)
		slog.Info("Delivery channel closed, waiting for in-flight deliveries")
		inflightWg.Wait()

		// All sends are done — safe to close the queue
		close(taskQueue)

		// Wait for workers to drain any remaining queued tasks
		workerWg.Wait()
		slog.Info("All deliveries complete")
	}()

	slurpee.SetStopDelivery(func() {
		shutdownCancel() // signal retry timers to abandon
		close(slurpee.DeliveryChan)
		<-done
	})

	return &DispatcherState{
		inflightWg: &inflightWg,
		taskQueue:  taskQueue,
		registry:   registry,
	}
}

// dispatchEvent finds matching subscriptions for an event and enqueues delivery tasks.
func dispatchEvent(slurpee *Application, event db.Event, inflightWg *sync.WaitGroup, taskQueue chan<- deliveryTask, registry *eventRegistry) {
	ctx := context.Background()
	logger := slog.Default().With("event_id", UuidToString(event.ID), "subject", event.Subject)

	// Find all subscriptions whose subject_pattern matches the event subject
	subscriptions, err := slurpee.DB.GetSubscriptionsMatchingSubject(ctx, event.Subject)
	if err != nil {
		logger.Error("Failed to find matching subscriptions", "error", err)
		updateEventStatus(ctx, slurpee, event.ID, event.RetryCount, "failed")
		return
	}

	if len(subscriptions) == 0 {
		logger.Info("No matching subscriptions for event")
		updateEventStatus(ctx, slurpee, event.ID, event.RetryCount, "delivered")
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
		subscriber, err := slurpee.DB.GetSubscriberByID(ctx, subID)
		if err != nil {
			logger.Error("Failed to get subscriber", "error", err, "subscriber_id", UuidToString(subID))
			continue
		}
		subscribers[subID] = subscriber
	}

	// Build initial delivery tasks, deduplicated to one per subscriber.
	// When multiple subscriptions match for the same subscriber, we pick the
	// one with the highest effective max retries.
	var tasks []deliveryTask
	for subID, subs := range subscriberSubs {
		subscriber, ok := subscribers[subID]
		if !ok {
			continue
		}

		var bestSub *db.Subscription
		bestMaxRetries := -1
		for i, sub := range subs {
			if !matchesFilter(sub.Filter, event.Data) {
				logger.Debug("Subscription filter did not match event data",
					"subscriber_id", UuidToString(subscriber.ID),
					"subscription_id", UuidToString(sub.ID),
					"subject_pattern", sub.SubjectPattern,
				)
				continue
			}

			maxRetries := slurpee.Config.MaxRetries
			if sub.MaxRetries.Valid {
				maxRetries = int(sub.MaxRetries.Int32)
			}

			if maxRetries > bestMaxRetries {
				bestSub = &subs[i]
				bestMaxRetries = maxRetries
			}
		}

		if bestSub == nil {
			continue
		}

		if len(subs) > 1 {
			logger.Debug("Deduplicated overlapping subscriptions for subscriber",
				"subscriber_id", UuidToString(subscriber.ID),
				"matching_subscriptions", len(subs),
				"selected_subscription_id", UuidToString(bestSub.ID),
			)
		}

		tasks = append(tasks, deliveryTask{
			event:        event,
			subscription: *bestSub,
			subscriber:   subscriber,
			attemptNum:   0,
			maxRetries:   bestMaxRetries,
		})
	}

	if len(tasks) == 0 {
		logger.Warn("No valid subscribers found for matching subscriptions")
		updateEventStatus(ctx, slurpee, event.ID, event.RetryCount, "failed")
		return
	}

	// Create tracker for this event
	tracker := &eventTracker{
		event:    event,
		expected: len(tasks),
		results:  make(map[[16]byte]deliveryResult),
		logger:   logger,
	}
	registry.register(event.ID.Bytes, tracker)

	// Enqueue all tasks
	for i := range tasks {
		tasks[i].tracker = tracker
		inflightWg.Add(1)
		taskQueue <- tasks[i]
	}
}

// processDeliveryTask handles a single delivery attempt with retry logic.
// Called by worker goroutines — not spawned in its own goroutine.
func processDeliveryTask(
	shutdownCtx context.Context,
	slurpee *Application,
	task deliveryTask,
	getSemaphore func([16]byte, int32) chan struct{},
	inflightWg *sync.WaitGroup,
	taskQueue chan<- deliveryTask,
	registry *eventRegistry,
) {
	defer inflightWg.Done()

	ctx := context.Background()
	logger := task.tracker.logger

	sem := getSemaphore(task.subscriber.ID.Bytes, task.subscriber.MaxParallel)

	// Acquire semaphore
	sem <- struct{}{}

	succeeded := deliverToSubscriber(ctx, slurpee, task.event, task.subscriber, task.attemptNum, logger)

	// Release semaphore immediately — don't hold during queue operations
	<-sem

	if succeeded {
		result := deliveryResult{
			subscriptionID: task.subscription.ID,
			succeeded:      true,
		}
		if task.tracker.record(result) {
			finalizeEvent(ctx, slurpee, task.tracker, registry)
		}
		return
	}

	// Delivery failed — check if we should retry
	if task.attemptNum >= task.maxRetries {
		logger.Warn("Max retries exhausted",
			"subscriber_id", UuidToString(task.subscriber.ID),
			"endpoint_url", task.subscriber.EndpointUrl,
			"attempt", task.attemptNum+1,
			"max_retries", task.maxRetries,
		)
		result := deliveryResult{
			subscriptionID: task.subscription.ID,
			succeeded:      false,
			exhausted:      true,
		}
		if task.tracker.record(result) {
			finalizeEvent(ctx, slurpee, task.tracker, registry)
		}
		return
	}

	// Schedule retry with exponential backoff
	delay := calculateBackoff(task.attemptNum, slurpee.Config.MaxBackoffSeconds)
	logger.Info("Scheduling retry",
		"subscriber_id", UuidToString(task.subscriber.ID),
		"endpoint_url", task.subscriber.EndpointUrl,
		"attempt", task.attemptNum+1,
		"next_attempt", task.attemptNum+2,
		"delay_seconds", delay.Seconds(),
	)

	// Update event status to partial while retrying
	updateEventStatus(ctx, slurpee, task.event.ID, task.event.RetryCount+1, "partial")

	// Build next attempt task
	nextTask := deliveryTask{
		event:        task.event,
		subscription: task.subscription,
		subscriber:   task.subscriber,
		attemptNum:   task.attemptNum + 1,
		maxRetries:   task.maxRetries,
		tracker:      task.tracker,
	}
	nextTask.event.RetryCount++

	// Non-blocking retry: increment inflight before scheduling timer
	inflightWg.Add(1)
	time.AfterFunc(delay, func() {
		if shutdownCtx.Err() != nil {
			inflightWg.Done() // abandon retry on shutdown
			return
		}
		taskQueue <- nextTask
	})
}

// finalizeEvent determines the final event status from tracker results
// and updates the database.
func finalizeEvent(ctx context.Context, slurpee *Application, tracker *eventTracker, registry *eventRegistry) {
	tracker.mu.Lock()
	allSucceeded := true
	anyFailed := false
	for _, r := range tracker.results {
		if !r.succeeded {
			allSucceeded = false
			if r.exhausted {
				anyFailed = true
			}
		}
	}
	tracker.mu.Unlock()

	var finalStatus string
	if allSucceeded {
		finalStatus = "delivered"
	} else if anyFailed {
		finalStatus = "failed"
	} else {
		finalStatus = "failed"
	}

	updateEventStatus(ctx, slurpee, tracker.event.ID, tracker.event.RetryCount, finalStatus)
	tracker.logger.Info("Event delivery complete", "status", finalStatus)

	registry.remove(tracker.event.ID.Bytes)
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
func deliverToSubscriber(ctx context.Context, slurpee *Application, event db.Event, subscriber db.Subscriber, attemptNum int, logger *slog.Logger) bool {
	attemptID := pgtype.UUID{Bytes: uuid.Must(uuid.NewV7()), Valid: true}
	now := time.Now().UTC()

	// Build the request body (event data as JSON)
	body := event.Data

	// Build request headers
	reqHeaders := map[string]string{
		"Content-Type":     "application/json",
		"X-Slurpee-Secret": subscriber.AuthSecret,
		"X-Event-ID":       UuidToString(event.ID),
		"X-Event-Subject":  event.Subject,
	}
	reqHeadersJSON, _ := json.Marshal(reqHeaders)

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, subscriber.EndpointUrl, bytes.NewReader(body))
	if err != nil {
		logger.Error("Failed to create delivery request",
			"error", err,
			"subscriber_id", UuidToString(subscriber.ID),
			"endpoint_url", subscriber.EndpointUrl,
			"attempt", attemptNum+1,
		)
		recordFailedAttempt(ctx, slurpee, attemptID, event, subscriber, reqHeadersJSON, now, fmt.Sprintf("request creation failed: %v", err))
		return false
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Slurpee-Secret", subscriber.AuthSecret)
	req.Header.Set("X-Event-ID", UuidToString(event.ID))
	req.Header.Set("X-Event-Subject", event.Subject)

	// Send the request
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		logger.Error("Delivery request failed",
			"error", err,
			"subscriber_id", UuidToString(subscriber.ID),
			"endpoint_url", subscriber.EndpointUrl,
			"attempt", attemptNum+1,
		)
		recordFailedAttempt(ctx, slurpee, attemptID, event, subscriber, reqHeadersJSON, now, fmt.Sprintf("request failed: %v", err))
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
	_, err = slurpee.DB.InsertDeliveryAttempt(ctx, db.InsertDeliveryAttemptParams{
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
		logger.Error("Failed to record delivery attempt", "error", err, "subscriber_id", UuidToString(subscriber.ID))
	}

	// Publish 'delivery_attempt' message to the event bus for SSE clients
	slurpee.EventBus.Publish(BusMessage{
		Type:               BusMessageDeliveryAttempt,
		EventID:            UuidToString(event.ID),
		Subject:            event.Subject,
		DeliveryStatus:     event.DeliveryStatus,
		Timestamp:          now,
		SubscriberEndpoint: subscriber.EndpointUrl,
		AttemptStatus:      status,
		ResponseStatusCode: resp.StatusCode,
	})

	if status == "succeeded" {
		logger.Info("Delivery succeeded",
			"subscriber_id", UuidToString(subscriber.ID),
			"endpoint_url", subscriber.EndpointUrl,
			"status_code", resp.StatusCode,
			"attempt", attemptNum+1,
		)
	} else {
		logger.Warn("Delivery failed",
			"subscriber_id", UuidToString(subscriber.ID),
			"endpoint_url", subscriber.EndpointUrl,
			"status_code", resp.StatusCode,
			"attempt", attemptNum+1,
		)
	}

	return status == "succeeded"
}

// recordFailedAttempt records a delivery attempt that failed before getting a response.
func recordFailedAttempt(ctx context.Context, slurpee *Application, attemptID pgtype.UUID, event db.Event, subscriber db.Subscriber, reqHeadersJSON []byte, attemptedAt time.Time, errMsg string) {
	_, err := slurpee.DB.InsertDeliveryAttempt(ctx, db.InsertDeliveryAttemptParams{
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
		slog.Error("Failed to record failed delivery attempt", "error", err, "subscriber_id", UuidToString(subscriber.ID))
	}
	// Publish 'delivery_attempt' message to the event bus for SSE clients
	slurpee.EventBus.Publish(BusMessage{
		Type:               BusMessageDeliveryAttempt,
		EventID:            UuidToString(event.ID),
		Subject:            event.Subject,
		DeliveryStatus:     event.DeliveryStatus,
		Timestamp:          attemptedAt,
		SubscriberEndpoint: subscriber.EndpointUrl,
		AttemptStatus:      "failed",
		ResponseStatusCode: 0,
	})
}

// ResumeUnfinishedDeliveries queries for events with 'pending' or 'partial' status
// and feeds them back through the delivery pipeline. Pending events are sent to
// DeliveryChan for normal dispatchEvent processing. Partial events are enqueued
// directly into the dispatcher's task queue, skipping already-succeeded subscribers
// and continuing retry counts. Call this after StartDispatcher.
func ResumeUnfinishedDeliveries(slurpee *Application, ds *DispatcherState) {
	ctx := context.Background()
	events, err := slurpee.DB.GetResumableEvents(ctx)
	if err != nil {
		slog.Error("Failed to query resumable events", "error", err)
		return
	}

	if len(events) == 0 {
		slog.Debug("No events to resume on startup")
		return
	}

	var pendingCount, partialCount int
	for _, event := range events {
		switch event.DeliveryStatus {
		case "pending":
			pendingCount++
		case "partial":
			partialCount++
		}
	}

	slog.Info("Resuming unfinished deliveries on startup",
		"pending", pendingCount, "partial", partialCount, "total", len(events))

	// Feed pending events into DeliveryChan in a goroutine to avoid blocking
	// if the channel buffer is smaller than the number of events.
	go func() {
		for _, event := range events {
			if event.DeliveryStatus == "pending" {
				slurpee.DeliveryChan <- event
			}
		}
	}()

	// Resume partial events directly via the dispatcher's task queue
	for _, event := range events {
		if event.DeliveryStatus == "partial" {
			resumePartialEvent(ctx, slurpee, event, ds)
		}
	}
}

// resumePartialEvent handles resumption of a single partial event by checking
// delivery history, re-running subscription matching, and enqueuing tasks for
// subscribers that still need delivery.
func resumePartialEvent(ctx context.Context, slurpee *Application, event db.Event, ds *DispatcherState) {
	logger := slog.Default().With("event_id", UuidToString(event.ID), "subject", event.Subject, "resume", true)

	// Get per-subscriber delivery summary
	summary, err := slurpee.DB.GetDeliverySummaryForEvent(ctx, event.ID)
	if err != nil {
		logger.Error("Failed to get delivery summary for partial event", "error", err)
		return
	}

	// Build lookup: subscriber_id -> {failed_count, succeeded_count}
	type subSummary struct {
		failedCount    int64
		succeededCount int64
	}
	summaryMap := make(map[[16]byte]subSummary)
	for _, s := range summary {
		summaryMap[s.SubscriberID.Bytes] = subSummary{
			failedCount:    s.FailedCount,
			succeededCount: s.SucceededCount,
		}
	}

	// Re-run subscription matching (same logic as dispatchEvent)
	subscriptions, err := slurpee.DB.GetSubscriptionsMatchingSubject(ctx, event.Subject)
	if err != nil {
		logger.Error("Failed to find matching subscriptions for partial event", "error", err)
		return
	}

	if len(subscriptions) == 0 {
		logger.Warn("No matching subscriptions for partial event, marking as delivered")
		updateEventStatus(ctx, slurpee, event.ID, event.RetryCount, "delivered")
		return
	}

	// Group subscriptions by subscriber
	subscriberSubs := make(map[pgtype.UUID][]db.Subscription)
	for _, sub := range subscriptions {
		subscriberSubs[sub.SubscriberID] = append(subscriberSubs[sub.SubscriberID], sub)
	}

	// Load subscriber details
	subscribers := make(map[pgtype.UUID]db.Subscriber)
	for subID := range subscriberSubs {
		subscriber, err := slurpee.DB.GetSubscriberByID(ctx, subID)
		if err != nil {
			logger.Error("Failed to get subscriber", "error", err, "subscriber_id", UuidToString(subID))
			continue
		}
		subscribers[subID] = subscriber
	}

	// Build delivery tasks, deduplicated per subscriber, skipping already-succeeded
	var tasks []deliveryTask
	for subID, subs := range subscriberSubs {
		subscriber, ok := subscribers[subID]
		if !ok {
			continue
		}

		// Check if this subscriber already succeeded
		if ss, found := summaryMap[subID.Bytes]; found && ss.succeededCount > 0 {
			logger.Debug("Skipping already-succeeded subscriber",
				"subscriber_id", UuidToString(subID))
			continue
		}

		// Pick best subscription (same deduplication logic as dispatchEvent)
		var bestSub *db.Subscription
		bestMaxRetries := -1
		for i, sub := range subs {
			if !matchesFilter(sub.Filter, event.Data) {
				continue
			}
			maxRetries := slurpee.Config.MaxRetries
			if sub.MaxRetries.Valid {
				maxRetries = int(sub.MaxRetries.Int32)
			}
			if maxRetries > bestMaxRetries {
				bestSub = &subs[i]
				bestMaxRetries = maxRetries
			}
		}

		if bestSub == nil {
			continue
		}

		// Continue retry count from prior failed attempts
		attemptNum := 0
		if ss, found := summaryMap[subID.Bytes]; found {
			attemptNum = int(ss.failedCount)
		}

		tasks = append(tasks, deliveryTask{
			event:        event,
			subscription: *bestSub,
			subscriber:   subscriber,
			attemptNum:   attemptNum,
			maxRetries:   bestMaxRetries,
		})
	}

	if len(tasks) == 0 {
		logger.Info("No subscribers need delivery for partial event, marking as delivered")
		updateEventStatus(ctx, slurpee, event.ID, event.RetryCount, "delivered")
		return
	}

	logger.Info("Resuming partial event", "subscribers_remaining", len(tasks))

	// Create tracker and enqueue tasks directly into the dispatcher
	tracker := &eventTracker{
		event:    event,
		expected: len(tasks),
		results:  make(map[[16]byte]deliveryResult),
		logger:   logger,
	}
	ds.registry.register(event.ID.Bytes, tracker)

	for i := range tasks {
		tasks[i].tracker = tracker
		ds.inflightWg.Add(1)
		ds.taskQueue <- tasks[i]
	}
}

// PublishCreatedEvent publishes a 'created' bus message for SSE clients.
func PublishCreatedEvent(slurpee *Application, event db.Event, props map[string]string) {
	slurpee.EventBus.Publish(BusMessage{
		Type:           BusMessageCreated,
		EventID:        UuidToString(event.ID),
		Subject:        event.Subject,
		DeliveryStatus: event.DeliveryStatus,
		Timestamp:      event.Timestamp.Time,
		Properties:     props,
	})
}

// ReplayToSubscriber delivers an event to a single subscriber as a replay.
// It resets the event status to pending, performs a single delivery attempt, and updates the event status.
func ReplayToSubscriber(slurpee *Application, event db.Event, subscriber db.Subscriber) {
	ctx := context.Background()
	logger := slog.Default().With("event_id", UuidToString(event.ID), "subject", event.Subject, "replay", true)

	updateEventStatus(ctx, slurpee, event.ID, event.RetryCount, "pending")

	succeeded := deliverToSubscriber(ctx, slurpee, event, subscriber, 0, logger)

	if succeeded {
		updateEventStatus(ctx, slurpee, event.ID, event.RetryCount, "delivered")
		logger.Info("Replay delivery succeeded", "subscriber_id", UuidToString(subscriber.ID))
	} else {
		updateEventStatus(ctx, slurpee, event.ID, event.RetryCount, "failed")
		logger.Warn("Replay delivery failed", "subscriber_id", UuidToString(subscriber.ID))
	}
}

// updateEventStatus updates the delivery_status, retry_count, and status_updated_at on an event.
func updateEventStatus(ctx context.Context, slurpee *Application, eventID pgtype.UUID, retryCount int32, status string) {
	_, err := slurpee.DB.UpdateEventDeliveryStatus(ctx, db.UpdateEventDeliveryStatusParams{
		DeliveryStatus:  status,
		RetryCount:      retryCount,
		StatusUpdatedAt: pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
		ID:              eventID,
	})
	if err != nil {
		slog.Error("Failed to update event delivery status", "error", err, "event_id", UuidToString(eventID))
		return
	}
	// Publish 'status_changed' message to the event bus for SSE clients
	slurpee.EventBus.Publish(BusMessage{
		Type:           BusMessageStatusChanged,
		EventID:        UuidToString(eventID),
		DeliveryStatus: status,
		Timestamp:      time.Now().UTC(),
	})
}
