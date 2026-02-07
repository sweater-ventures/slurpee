# PRD: Unit Tests for Core Functionality

## Introduction

Add unit tests for the core functionality of the Slurpee event broker: the `/api/events` and `/api/subscribers` API endpoints, and the event dispatch/delivery pipeline. Tests should focus on observable behavior rather than implementation details, using mocked database dependencies and real HTTP test servers where appropriate. Refactoring is acceptable to improve testability.

## Goals

- Establish a test infrastructure with interfaces and mocks that makes writing future tests easy
- Achieve high behavioral coverage of the API endpoints (authentication, authorization, validation, happy paths, error paths)
- Achieve high behavioral coverage of the event dispatch and delivery pipeline (subscriber matching, filtering, webhook delivery, retries, status tracking)
- Keep tests focused on "what the system does" not "how it does it internally"

## User Stories

### US-001: Enable sqlc Querier Interface and Refactor Application

**Description:** As a developer, I need a mockable interface for database operations so that handlers and business logic can be tested without a real database.

**Acceptance Criteria:**

- [ ] Add `emit_interface: true` to `sqlc.yaml` under `gen.go`
- [ ] Regenerate sqlc code (`go tool sqlc generate`) — the `Querier` interface in `db/querier.go` is now populated with all query methods
- [ ] Change `Application.DB` field type from `*db.Queries` to `db.Querier`
- [ ] Update `NewApp()` and any code that assigns to `Application.DB` to work with the interface
- [ ] All existing code compiles without errors (`go build ./...`)
- [ ] Application runs correctly (manual smoke test)

### US-002: Set Up Test Infrastructure

**Description:** As a developer, I need test dependencies, mock implementations, and helper functions so that writing tests is straightforward and consistent.

**Acceptance Criteria:**

- [ ] Add `github.com/stretchr/testify` as a test dependency
- [ ] Create `testutil/mock_querier.go` with a testify mock implementation of `db.Querier`
- [ ] Create `testutil/factories.go` with factory functions that build test instances of `db.Event`, `db.Subscriber`, `db.Subscription`, `db.ApiSecret`, and `app.Application` (with mock DB, config, channels, etc.)
- [ ] Factory functions use sensible defaults but allow overrides
- [ ] Create `testutil/helpers.go` with HTTP test helpers: functions to build `*http.Request` with headers (secret ID, secret value, admin secret, JSON body) and to assert JSON response bodies
- [ ] All test utilities compile (`go test ./testutil/...`)

### US-003: Unit Tests for Pure Business Logic

**Description:** As a developer, I want tests for the pure functions that don't require database mocks, so that core logic is verified in isolation.

**Acceptance Criteria:**

- [ ] Tests for `app.CheckSendScope` — verifies SQL LIKE-style pattern matching: exact match, `%` wildcard, `_` single-char wildcard, no match, edge cases (empty pattern, empty subject)
- [ ] Tests for `app.MatchLikePattern` — covers `%` at start/end/middle, `_` matching, escaped characters, multiple wildcards
- [ ] Tests for `matchesFilter` in `app/delivery.go` — matching filter, non-matching filter, empty/nil filter matches all, partial key match, nested JSON values
- [ ] Tests for `calculateBackoff` in `app/delivery.go` — verifies exponential growth, respects max backoff cap, first attempt backoff
- [ ] All tests pass (`go test ./app/...`)
- [ ] Tests are table-driven where appropriate

### US-004: Unit Tests for POST /api/events

**Description:** As a developer, I want tests that verify the behavior of creating events through the API, covering authentication, authorization, validation, and the happy path.

**Acceptance Criteria:**

- [ ] Test: request without secret headers returns 401
- [ ] Test: request with invalid secret ID (not a UUID) returns 400
- [ ] Test: request with valid secret ID but wrong secret value returns 401
- [ ] Test: request with valid credentials but subject outside secret's scope returns 403
- [ ] Test: request with missing required fields (subject, data) returns 400
- [ ] Test: request with invalid JSON body returns 400
- [ ] Test: successful event creation returns 201 with event ID, subject, timestamp, and data in response
- [ ] Test: successful event creation sends event to delivery channel
- [ ] Tests use mock Querier, `httptest.NewRecorder`, and `httptest.NewRequest`
- [ ] All tests pass (`go test ./api/...`)

### US-005: Unit Tests for GET /api/events/{id}

**Description:** As a developer, I want tests that verify the behavior of retrieving events by ID through the API.

**Acceptance Criteria:**

- [ ] Test: request without secret headers returns 401
- [ ] Test: request with valid credentials for nonexistent event returns 404
- [ ] Test: request with valid credentials for existing event returns 200 with correct event data
- [ ] Test: response JSON structure matches the documented `EventResponse` format
- [ ] Tests use mock Querier
- [ ] All tests pass (`go test ./api/...`)

### US-006: Unit Tests for /api/subscribers Endpoints

**Description:** As a developer, I want tests that verify the behavior of subscriber management API endpoints: creating/updating subscribers and listing them.

**Acceptance Criteria:**

- [ ] Test POST: request without admin secret returns 401
- [ ] Test POST: request with wrong admin secret returns 401
- [ ] Test POST: request with missing required fields (name, endpoint_url) returns 400
- [ ] Test POST: request with invalid endpoint URL returns 400
- [ ] Test POST: successful subscriber creation returns 201 with subscriber details and subscriptions
- [ ] Test POST: subscriber creation with subscriptions creates both subscriber and subscription records
- [ ] Test GET: request without admin secret returns 401
- [ ] Test GET: successful request returns 200 with list of subscribers and their subscriptions
- [ ] Test GET: empty subscriber list returns 200 with empty array
- [ ] Tests use mock Querier
- [ ] All tests pass (`go test ./api/...`)

### US-007: Unit Tests for Event Dispatch and Delivery

**Description:** As a developer, I want tests that verify the full delivery pipeline: matching events to subscribers, building and sending webhook requests, handling responses, and retry behavior.

**Acceptance Criteria:**

- [ ] Test: `deliverToSubscriber` sends POST to subscriber endpoint with correct headers (`Content-Type`, `X-Slurpee-Secret`, `X-Event-ID`, `X-Event-Subject`) and body (event data)
- [ ] Test: `deliverToSubscriber` returns true for 2xx responses and false for non-2xx
- [ ] Test: `deliverToSubscriber` records delivery attempt in database with request/response details
- [ ] Test: `dispatchEvent` finds matching subscriptions and enqueues delivery tasks
- [ ] Test: `dispatchEvent` skips subscriptions whose filters don't match the event data
- [ ] Test: delivery retries on failure up to the configured max retries
- [ ] Test: event status is updated to "delivered" when all deliveries succeed
- [ ] Test: event status is updated to "failed" when all deliveries fail after retries
- [ ] Tests use `httptest.Server` for webhook endpoints and mock Querier for database
- [ ] All tests pass (`go test ./app/...`)

## Functional Requirements

- FR-1: The `db.Querier` interface must include all methods currently on `*db.Queries`, generated by sqlc with `emit_interface: true`
- FR-2: `Application.DB` must be typed as `db.Querier` (interface) instead of `*db.Queries` (concrete)
- FR-3: A testify mock of `db.Querier` must be available in a `testutil` package
- FR-4: Test factory functions must produce valid test instances with minimal boilerplate
- FR-5: Pure business logic tests must not depend on any mock or database
- FR-6: API handler tests must use `httptest` and mock Querier, testing through the HTTP layer
- FR-7: Delivery tests must use `httptest.Server` for webhook endpoints
- FR-8: All tests must be runnable with `go test ./...` without external dependencies (no database, no network)

## Non-Goals

- No integration tests against a real database (those may come later)
- No tests for the UI/views layer
- No tests for session management or login
- No performance benchmarks
- No test coverage thresholds or CI pipeline changes
- No changes to the existing API behavior — refactoring for testability only

## Technical Considerations

- **sqlc `emit_interface: true`**: This is the cleanest way to get a full `Querier` interface. It's a one-line config change plus regeneration. The generated interface will include all query methods with the same signatures.
- **testify mocks**: Use `testify/mock` for the Querier mock. This allows flexible expectation setup and keeps tests behavior-focused (expect specific method calls with specific args, return specific results).
- **Unexported functions**: `matchesFilter`, `calculateBackoff`, `deliverToSubscriber`, and `dispatchEvent` are unexported. Test files in the `app` package (e.g., `app/delivery_test.go` with `package app`) can access them directly. This avoids exporting internal functions just for tests.
- **Handler testing pattern**: API handlers take `(slurpee *app.Application, w, r)`. Tests can construct an `Application` with a mock DB and call handlers directly, or use the `routeHandler` wrapper with `httptest`.
- **Delivery channel**: Tests for event creation should verify the event appears on the `DeliveryChan` by reading from the channel with a timeout.
- **Context with logger**: Some functions expect a logger in context. Test helpers should provide a no-op or buffer logger via `config.LoggerContextKey`.

## Success Metrics

- All tests pass with `go test ./...`
- Tests run in under 5 seconds total (no real I/O, no sleeps)
- Adding a new API endpoint test requires minimal boilerplate (factory + a few mock expectations + HTTP assertion)
- Tests catch real regressions: changing handler behavior breaks a test

## Open Questions

- Should we use `testify/suite` for grouping related tests, or keep it simple with subtests (`t.Run`)?
  keep it simple
- Should the mock Querier live in `testutil/` or in `db/mocks/`? (PRD assumes `testutil/` for co-location with other test helpers)
  testutil is fine
