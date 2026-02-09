# PRD: End-to-End Regression Tests with Embedded PostgreSQL

## Introduction

Add end-to-end regression tests that exercise the full Slurpee API stack against a real PostgreSQL database using `embedded-postgres-go`. The existing unit tests use mocked database queries (`MockQuerier`), which cannot catch SQL correctness issues, constraint violations, migration drift, or query edge cases. These E2E tests validate the complete request→database→response cycle and serve as a regression safety net before deployments.

## Goals

- Validate that API endpoints work correctly against a real PostgreSQL database with real sqlc-generated queries
- Catch SQL bugs, constraint violations, and migration issues that mock-based tests cannot
- Cover the full delivery pipeline: event creation → subscription matching → HTTP delivery → delivery attempt recording
- Verify deduplication and retry logic with real database state
- Run fast enough to be part of the regular `go test` workflow (embedded-postgres, no external dependencies)

## User Stories

### US-001: Set up embedded-postgres test infrastructure

**Description:** As a developer, I need reusable test infrastructure that starts an embedded PostgreSQL instance, runs migrations, and provides helpers for creating test fixtures against the real database, so that all E2E tests can share this foundation.

**Acceptance Criteria:**

- [ ] Add `github.com/fergusstrange/embedded-postgres` dependency
- [ ] Create `tests/e2e/` package with `TestMain` that starts embedded-postgres once for the package
- [ ] `TestMain` runs all `schema/*.sql` migration files (sorted by filename) against the embedded database on startup
- [ ] Provide a `truncateAll(t *testing.T)` helper that truncates all tables between tests (respecting FK order: `delivery_attempts`, `api_secret_subscribers`, `subscriptions`, `subscribers`, `api_secrets`, `events`, `log_config`)
- [ ] Provide a `newTestApp(t *testing.T)` helper that returns an `*app.Application` wired to the real database (real `db.Queries`, not mock) with sensible config defaults (AdminSecret, MaxRetries, DeliveryWorkers, etc.)
- [ ] Provide a `newTestRouter(t *testing.T, slurpee *app.Application)` helper that returns an `*http.ServeMux` with API routes registered
- [ ] Provide seed helpers: `seedSubscriber`, `seedSubscription`, `seedApiSecret` that insert test data directly via the real `db.Queries` interface
- [ ] `go build ./...` passes
- [ ] `go test ./tests/e2e/...` passes (even if only a placeholder test exists)

### US-002: E2E tests for POST /events endpoint

**Description:** As a developer, I want E2E tests for event creation that verify the full request→database→response cycle including authentication, authorization, validation, and persistence.

**Acceptance Criteria:**

- [ ] Test happy path: create event with valid API secret, verify 201 response with correct fields, verify event persisted in database via `GetEventByID`
- [ ] Test with client-provided UUID: verify the event is created with the exact ID provided
- [ ] Test with client-provided timestamp: verify the event stores the provided timestamp
- [ ] Test with optional trace_id: verify trace_id is persisted and returned
- [ ] Test missing `X-Slurpee-Secret-ID` header returns 401
- [ ] Test invalid/wrong API secret returns 401
- [ ] Test subject outside secret's scope returns 403
- [ ] Test missing subject returns 400
- [ ] Test missing data returns 400
- [ ] Test invalid JSON data returns 400
- [ ] All tests truncate tables before running (clean state)
- [ ] `go test ./tests/e2e/...` passes

### US-003: E2E tests for GET /events/{id} endpoint

**Description:** As a developer, I want E2E tests for event retrieval that verify reading events back from the real database with proper authentication.

**Acceptance Criteria:**

- [ ] Test happy path: create event, then GET it by ID, verify all fields match
- [ ] Test event not found returns 404
- [ ] Test invalid UUID format returns 400
- [ ] Test missing API secret returns 401
- [ ] Test invalid API secret returns 401
- [ ] All tests truncate tables before running
- [ ] `go test ./tests/e2e/...` passes

### US-004: E2E tests for POST /subscribers endpoint

**Description:** As a developer, I want E2E tests for subscriber creation/upsert that verify the full cycle including subscription creation and idempotent upsert behavior against the real database.

**Acceptance Criteria:**

- [ ] Test happy path: create subscriber with subscriptions, verify 200 response, verify subscriber and subscriptions persisted in database
- [ ] Test upsert: create subscriber, then POST again with same name/endpoint but different subscriptions, verify old subscriptions replaced
- [ ] Test subscription with filter: verify filter JSON persisted correctly
- [ ] Test subscription with max_retries override: verify persisted
- [ ] Test missing admin secret returns 401
- [ ] Test wrong admin secret returns 401
- [ ] Test missing required fields (name, endpoint_url, auth_secret, subscriptions) each return 400
- [ ] Test missing subject_pattern in subscription returns 400
- [ ] All tests truncate tables before running
- [ ] `go test ./tests/e2e/...` passes

### US-005: E2E tests for GET /subscribers endpoint

**Description:** As a developer, I want E2E tests for listing subscribers that verify the real database query returns correct data with subscriptions included.

**Acceptance Criteria:**

- [ ] Test happy path: seed multiple subscribers with subscriptions, verify all returned with correct subscription details
- [ ] Test empty list: no subscribers returns empty array `[]`
- [ ] Test missing admin secret returns 401
- [ ] All tests truncate tables before running
- [ ] `go test ./tests/e2e/...` passes

### US-006: E2E tests for delivery pipeline

**Description:** As a developer, I want E2E tests that verify the complete delivery pipeline: posting an event causes it to be delivered to matching subscribers' HTTP endpoints, with delivery attempts recorded in the database.

**Acceptance Criteria:**

- [ ] Start an `httptest.Server` as a mock subscriber endpoint that records received requests
- [ ] Seed a subscriber pointing at the test server, with a subscription matching the event subject
- [ ] POST an event via the API, wait for delivery to complete
- [ ] Verify the mock endpoint received exactly one HTTP POST with correct headers (`X-Event-ID`, `X-Event-Subject`, `X-Slurpee-Secret`) and the event data as the body
- [ ] Verify a `delivery_attempts` row was recorded in the database with status `succeeded` and correct response_status_code
- [ ] Verify the event's `delivery_status` was updated to `delivered` in the database
- [ ] Test with no matching subscriptions: event status becomes `delivered` (no subscribers to deliver to)
- [ ] All tests truncate tables before running
- [ ] `go test ./tests/e2e/...` passes

### US-007: E2E tests for subscription pattern matching and filters

**Description:** As a developer, I want E2E tests that verify glob-style subject pattern matching and JSON filter logic work correctly with the real database query (`GetSubscriptionsMatchingSubject`).

**Acceptance Criteria:**

- [ ] Test exact subject match: subscription with pattern `order.created` matches event with subject `order.created`
- [ ] Test glob wildcard: subscription with pattern `order.*` matches event with subject `order.created`
- [ ] Test wildcard does NOT match unrelated subjects: `order.*` does not match `user.created`
- [ ] Test subscription filter: subscriber with filter `{"type": "premium"}` only receives events where `data.type == "premium"`
- [ ] Test filter mismatch: event with `{"type": "basic"}` is NOT delivered to subscriber with filter `{"type": "premium"}`
- [ ] Verify delivery attempts are only recorded for matched subscriptions
- [ ] All tests truncate tables before running
- [ ] `go test ./tests/e2e/...` passes

### US-008: E2E tests for subscriber deduplication

**Description:** As a developer, I want E2E tests that verify when a subscriber has multiple overlapping subscriptions matching the same event, only one delivery is made (the one with the highest max_retries).

**Acceptance Criteria:**

- [ ] Seed a subscriber with two subscriptions: `order.*` (max_retries=3) and `order.created` (max_retries=5)
- [ ] POST an event with subject `order.created`
- [ ] Verify the mock endpoint received exactly ONE request (not two)
- [ ] Verify exactly one `delivery_attempts` row in the database for this event+subscriber pair
- [ ] Test with three overlapping patterns: verify still only one delivery
- [ ] All tests truncate tables before running
- [ ] `go test ./tests/e2e/...` passes

### US-009: E2E tests for delivery retries

**Description:** As a developer, I want E2E tests that verify the retry mechanism works end-to-end: failed deliveries are retried up to max_retries, and delivery attempts are recorded for each attempt.

**Acceptance Criteria:**

- [ ] Start a mock endpoint that returns 500 for the first N requests, then 200
- [ ] Seed subscriber and subscription with known max_retries
- [ ] POST an event and wait for delivery to complete (use short backoff config for fast tests: `MaxBackoffSeconds=1`)
- [ ] Verify multiple `delivery_attempts` rows recorded (one per attempt)
- [ ] Verify the final event status is `delivered` after the successful retry
- [ ] Test max retries exhausted: endpoint always returns 500, verify event status becomes `failed`, verify exactly max_retries+1 delivery attempts recorded
- [ ] All tests truncate tables before running
- [ ] `go test ./tests/e2e/...` passes

## Functional Requirements

- FR-1: The `tests/e2e` package must use `embedded-postgres-go` to start a real PostgreSQL instance in `TestMain`
- FR-9: `TestMain` must check `testing.Short()` and skip all E2E tests when `-short` flag is passed, allowing fast CI runs
- FR-2: All schema migrations in `schema/*.sql` must be applied automatically at test startup, in filename-sorted order, extracting only the `-- +migrate Up` section
- FR-3: Tables must be truncated (not dropped/recreated) between tests for speed, in correct FK dependency order
- FR-4: The test `Application` must use real `db.Queries` (not mocks) wired to the embedded database
- FR-5: Delivery pipeline tests must use `httptest.Server` as mock subscriber endpoints
- FR-6: Delivery pipeline tests must start the dispatcher (`app.StartDispatcher`) and properly shut it down after each test
- FR-7: Tests must use short timeouts and minimal backoff (`MaxBackoffSeconds=1`) to keep execution fast
- FR-8: All tests must be independent — no ordering dependencies between test functions

## Non-Goals

- No UI/browser testing (this is API-only)
- No load/performance testing
- No testing of the SSE/EventBus streaming (that's an in-memory pub/sub, already covered by architecture)
- No testing of the session-based web UI authentication
- No replacement of existing mock-based unit tests (these E2E tests complement them)

## Technical Considerations

- **embedded-postgres-go** (`github.com/fergusstrange/embedded-postgres`) downloads and runs a real PostgreSQL binary — first run will take longer due to download
- Migration files use `-- +migrate Up` / `-- +migrate Down` format; the test runner must parse and apply only the `Up` section
- The `db.New(conn)` function returns a `*db.Queries` that satisfies the `db.Querier` interface — this is what the test app should use
- The delivery pipeline runs goroutines (`StartDispatcher`); tests must properly start/stop the dispatcher and drain channels
- `GetSubscriptionsMatchingSubject` uses database-side pattern matching — this is a key thing E2E tests validate that mocks cannot
- API secret validation uses bcrypt hashing — seed helpers must create secrets with real bcrypt hashes
- Use `t.Parallel()` cautiously: since all tests share one database instance with truncation, parallel tests within this package could conflict. Safest to run tests sequentially within the `e2e` package.

## Success Metrics

- All E2E tests pass in `go test ./tests/e2e/...` with no external dependencies (no Docker, no running PostgreSQL)
- `go test -short ./...` skips E2E tests for fast CI feedback loops
- Tests complete in under 30 seconds total (after initial embedded-postgres download)
- Tests catch real SQL/migration issues that mock-based tests miss
- Zero flaky tests — deterministic, isolated, repeatable

## Resolved Questions

- **`-short` flag:** Yes — `TestMain` skips all E2E tests when `go test -short` is used
- **Package location:** `tests/e2e/` (not project root)
