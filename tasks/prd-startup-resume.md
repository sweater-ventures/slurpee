# PRD: Startup Event Resumption

## Introduction

When Slurpee shuts down, in-flight retry timers are abandoned and events in "pending" or "partial" delivery status are left unfinished. This feature adds automatic resumption of unfinished event deliveries on startup, ensuring no events are lost due to process restarts.

Currently, pending events (never dispatched) and partial events (mid-retry with some deliveries still failing) remain stuck in the database with no mechanism to pick them back up. This feature queries for these events on startup and feeds them back through the delivery pipeline, continuing retry counts from where they left off.

## Goals

- Automatically resume delivery of all "pending" and "partial" events on startup
- For partial events, skip subscribers that already received successful delivery
- Continue retry counting from existing delivery attempt history (no reset)
- Log resumption activity for operator visibility
- No configuration needed — always runs on startup

## User Stories

### US-001: SQL Queries for Event Resumption

**Description:** As a developer, I need database queries to find resumable events and understand their delivery history so the resume logic can make informed decisions.

**Acceptance Criteria:**
- [ ] Add `GetResumableEvents` query: returns all events with `delivery_status IN ('pending', 'partial')` ordered by `timestamp ASC`
- [ ] Add `GetDeliverySummaryForEvent` query: for a given event_id, returns per-subscriber counts of failed and succeeded attempts (grouped by subscriber_id)
- [ ] Run `sqlc generate` successfully
- [ ] Go build passes

### US-002: Resume Pending Events on Startup

**Description:** As an operator, I want events that were never dispatched (status "pending") to be automatically delivered when the service restarts, so no events are lost.

**Acceptance Criteria:**
- [ ] Add `ResumeUnfinishedDeliveries` function in `app/delivery.go`
- [ ] Pending events are fed into `DeliveryChan` for normal dispatch processing
- [ ] Function is called from `main.go` after `StartDispatcher` but before the HTTP server starts
- [ ] Logs the count of pending events being resumed (e.g., "Resuming 3 pending events")
- [ ] If no events need resuming, logs "No events to resume" at debug level
- [ ] Go build passes
- [ ] Existing tests pass with `go test ./... -short`

### US-003: Resume Partial Events on Startup

**Description:** As an operator, I want events that were mid-retry (status "partial") to continue delivery only to subscribers that haven't succeeded yet, preserving retry counts from prior attempts.

**Acceptance Criteria:**
- [ ] Extend `ResumeUnfinishedDeliveries` to handle partial events
- [ ] For each partial event, query `GetDeliverySummaryForEvent` to check delivery history
- [ ] Skip subscribers that already have a successful delivery attempt
- [ ] Set `attemptNum` on the delivery task to the count of prior failed attempts for that subscriber (so retry counting continues, not resets)
- [ ] Create delivery tasks and event tracker directly (cannot reuse `dispatchEvent` since it starts fresh)
- [ ] Logs per-event detail: event ID, count of subscribers still needing delivery
- [ ] Go build passes
- [ ] Existing tests pass with `go test ./... -short`

### US-004: E2E Test — Resume Pending Events

**Description:** As a developer, I want an E2E test that verifies pending events are delivered after calling the resume function, simulating a restart scenario.

**Acceptance Criteria:**
- [ ] Create `tests/e2e/startup_resume_test.go`
- [ ] Test inserts an event directly into the database with `delivery_status = 'pending'` (bypassing DeliveryChan)
- [ ] Test creates a subscriber with a mock HTTP endpoint
- [ ] Test calls `ResumeUnfinishedDeliveries`
- [ ] Verify the mock endpoint receives the event
- [ ] Verify the event status transitions to "delivered"
- [ ] Test passes with `go test ./tests/e2e/ -v -count=1`
- [ ] Existing tests pass with `go test ./... -short`

### US-005: E2E Test — Resume Partial Events

**Description:** As a developer, I want an E2E test that verifies partial events resume correctly — skipping already-succeeded subscribers and continuing retry counts for failing ones.

**Acceptance Criteria:**
- [ ] Add test to `tests/e2e/startup_resume_test.go`
- [ ] Test inserts an event with `delivery_status = 'partial'` and existing delivery_attempts (one succeeded subscriber, one with 2 failed attempts)
- [ ] Test creates mock endpoints for both subscribers
- [ ] Test calls `ResumeUnfinishedDeliveries`
- [ ] Verify the already-succeeded subscriber does NOT receive another delivery
- [ ] Verify the failing subscriber receives a new attempt with correct attempt numbering (attempt 3, continuing from 2 prior failures)
- [ ] Verify final event status is correct based on outcomes
- [ ] Test passes with `go test ./tests/e2e/ -v -count=1`
- [ ] Existing tests pass with `go test ./... -short`

## Functional Requirements

- FR-1: On startup, query all events with `delivery_status` of "pending" or "partial"
- FR-2: For pending events, feed directly into DeliveryChan for standard dispatch processing
- FR-3: For partial events, query delivery_attempts to determine per-subscriber state
- FR-4: For partial events, skip subscribers with at least one successful delivery attempt
- FR-5: For partial events, set `attemptNum` to the count of prior failed attempts for the subscriber (continuing, not resetting)
- FR-6: For partial events, re-evaluate subscription matching (subscriptions may have changed since original dispatch)
- FR-7: Resume runs after the dispatcher worker pool is ready but before the HTTP server accepts traffic
- FR-8: Log summary of resumed events at INFO level; log "no events to resume" at DEBUG level

## Non-Goals

- No configuration flag to disable resume — it always runs
- No staleness cutoff — all pending/partial events are resumed regardless of age
- No retry count reset — always continues from prior history
- No resumption of "failed" events — those have exhausted retries and are final
- No manual API endpoint for triggering re-processing (could be a future feature)
- No UI changes

## Technical Considerations

- `dispatchEvent` cannot be reused directly for partial events because it starts from scratch (attemptNum=0, all subscribers). A separate `resumePartialEvent` function is needed that creates delivery tasks with correct state.
- The resume function needs access to the same `inflightWg`, `taskQueue`, and `registry` used by `StartDispatcher`. Consider passing these via a struct or having resume logic live inside `StartDispatcher`.
- Pending events can be safely re-dispatched through DeliveryChan since they have zero delivery attempts.
- The `GetDeliverySummaryForEvent` query should use `COUNT(*) FILTER (WHERE status = 'failed')` and `COUNT(*) FILTER (WHERE status = 'succeeded')` grouped by subscriber_id.
- Resume should process events in timestamp order (oldest first) to maintain fairness.
- If DeliveryChan buffer is smaller than the number of pending events, the resume function must not block indefinitely — consider feeding events asynchronously or logging a warning.

## Success Metrics

- Zero events left in "pending" or "partial" status after startup completes (assuming subscribers are reachable)
- Retry counts continue correctly — no duplicate deliveries to already-succeeded subscribers
- Startup time impact is minimal (single DB query + channel sends)

## Open Questions

- Should there be a startup delay before resuming (to let subscribers come online first)? Currently: no delay.
