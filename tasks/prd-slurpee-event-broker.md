# PRD: Slurpee Event Broker — Remaining Features

## Introduction

Slurpee is a REST-based event broker that accepts events via HTTP, stores them in Postgres, and delivers them asynchronously to registered subscribers via HTTP POST. It includes a web interface for managing events, subscribers, delivery, and logging configuration.

The project scaffolding is complete — config loading, database connection pooling, HTTP routing infrastructure, middleware (logging, request context), base layout Templ components (SimplePage, HeaderBar, icons), and a version API endpoint are all in place. This PRD covers everything that still needs to be built to reach the feature set described in the README.

## Goals

- Store events durably in Postgres with UUID v7 IDs, timestamps, subjects, and arbitrary JSON data
- Deliver events asynchronously to subscriber endpoints via HTTP POST with shared-secret authentication
- Retry failed deliveries using exponential backoff
- Provide a REST API for publishing events and registering subscribers
- Provide a web UI for viewing events, managing subscribers, replaying deliveries, searching/filtering, and configuring per-subject logging
- Use existing project patterns: sqlc for queries, sql-migrate for schema, Templ templates, HTMX for interactivity, DaisyUI for styling

## User Stories

---

### US-001: Create events database table

**Description:** As a developer, I need an events table so that published events can be stored durably.

**Acceptance Criteria:**

- [ ] Migration file in `schema/` creates `events` table
- [ ] Columns: `id` (UUID v7, primary key), `subject` (text, not null), `timestamp` (timestamptz, not null), `trace_id` (UUID, nullable), `data` (jsonb, not null), `retry_count` (integer, default 0), `delivery_status` (text, default 'pending'), `status_updated_at` (timestamptz)
- [ ] Index on `subject`
- [ ] Index on `timestamp`
- [ ] Index on `delivery_status`
- [ ] Migration applies cleanly via `sql-migrate up`

---

### US-002: Create subscribers database table

**Description:** As a developer, I need a subscribers table so that subscriber registrations can be stored.

**Acceptance Criteria:**

- [ ] Migration file in `schema/` creates `subscribers` table
- [ ] Columns: `id` (UUID, primary key, auto-generated), `name` (text, not null), `endpoint_url` (text, not null, unique), `auth_secret` (text, not null), `max_parallel` (integer, default 1), `created_at` (timestamptz, default now), `updated_at` (timestamptz, default now)
- [ ] Unique constraint on `endpoint_url`
- [ ] Migration applies cleanly via `sql-migrate up`

---

### US-003: Create subscriptions database table

**Description:** As a developer, I need a subscriptions table to store which subjects a subscriber listens to and optional filtering rules.

**Acceptance Criteria:**

- [ ] Migration file in `schema/` creates `subscriptions` table
- [ ] Columns: `id` (UUID, primary key, auto-generated), `subscriber_id` (UUID, foreign key to subscribers), `subject_pattern` (text, not null), `filter` (jsonb, nullable), `max_retries` (integer, nullable — per-subscription override of global max retries), `created_at` (timestamptz, default now)
- [ ] Foreign key constraint with cascade delete on subscriber removal
- [ ] Index on `subject_pattern`
- [ ] Migration applies cleanly via `sql-migrate up`

---

### US-004: Create delivery_attempts database table

**Description:** As a developer, I need a delivery_attempts table to record each attempt to deliver an event to a subscriber.

**Acceptance Criteria:**

- [ ] Migration file in `schema/` creates `delivery_attempts` table
- [ ] Columns: `id` (UUID, primary key, auto-generated), `event_id` (UUID, foreign key to events), `subscriber_id` (UUID, foreign key to subscribers), `endpoint_url` (text, not null), `attempted_at` (timestamptz, not null, default now), `request_headers` (jsonb), `response_status_code` (integer, nullable), `response_headers` (jsonb, nullable), `response_body` (text, nullable), `status` (text, not null — 'succeeded', 'failed', 'pending')
- [ ] Foreign key constraints to events and subscribers
- [ ] Index on `event_id`
- [ ] Index on `subscriber_id`
- [ ] Index on `status`
- [ ] Migration applies cleanly via `sql-migrate up`

---

### US-005: Create log_config database table

**Description:** As a developer, I need a log_config table to store which event data properties should be logged for each subject.

**Acceptance Criteria:**

- [ ] Migration file in `schema/` creates `log_config` table
- [ ] Columns: `id` (UUID, primary key, auto-generated), `subject` (text, not null, unique), `log_properties` (text array, not null — list of JSON property paths to log from event data)
- [ ] Unique constraint on `subject`
- [ ] Migration applies cleanly via `sql-migrate up`

---

### US-006: Write sqlc queries for events

**Description:** As a developer, I need sqlc query definitions for event CRUD operations so that Go code is generated for database access.

**Acceptance Criteria:**

- [ ] Query file in `queries/` with: insert event, get event by ID, list events (paginated), search events by subject, search events by date range, search events by delivery status, search events by data content (jsonb), update event delivery status and retry count
- [ ] `sqlc generate` runs without errors
- [ ] Generated code appears in `db/`

---

### US-007: Write sqlc queries for subscribers and subscriptions

**Description:** As a developer, I need sqlc query definitions for subscriber and subscription CRUD operations.

**Acceptance Criteria:**

- [ ] Query file in `queries/` with: upsert subscriber by endpoint_url, get subscriber by ID, get subscriber by endpoint_url, list all subscribers, delete subscriber, create subscription, list subscriptions for subscriber, delete subscriptions for subscriber, get all subscriptions matching a subject (for delivery routing)
- [ ] `sqlc generate` runs without errors
- [ ] Generated code appears in `db/`

---

### US-008: Write sqlc queries for delivery attempts

**Description:** As a developer, I need sqlc query definitions for delivery attempt operations.

**Acceptance Criteria:**

- [ ] Query file in `queries/` with: insert delivery attempt, list delivery attempts for an event (ordered by attempted_at), list delivery attempts for a subscriber, update delivery attempt status
- [ ] `sqlc generate` runs without errors
- [ ] Generated code appears in `db/`

---

### US-009: Write sqlc queries for log config

**Description:** As a developer, I need sqlc query definitions for log configuration operations.

**Acceptance Criteria:**

- [ ] Query file in `queries/` with: upsert log config for subject, get log config by subject, list all log configs, delete log config for subject
- [ ] `sqlc generate` runs without errors
- [ ] Generated code appears in `db/`

---

### US-010: Implement POST /api/events endpoint

**Description:** As an API client, I want to publish events via HTTP POST so they are stored and delivered to subscribers.

**Acceptance Criteria:**

- [ ] `POST /api/events` accepts JSON body with fields: `subject` (required), `data` (required, JSON object), `id` (optional UUID v7 — auto-generated if not provided), `timestamp` (optional — auto-assigned to now if not provided), `trace_id` (optional UUID)
- [ ] Returns 201 with the created event as JSON (including the assigned `id` and `timestamp`)
- [ ] Returns 400 with error details if `subject` or `data` is missing
- [ ] Returns 400 if `data` is not a valid JSON object
- [ ] Event is persisted to the database before returning the response
- [ ] After persisting, event delivery is triggered asynchronously (does not block the response)
- [ ] Event subject and ID are logged on receipt, plus any additional properties configured via log_config for that subject
- [ ] Follows existing API handler pattern (`routeHandler`, `writeJsonResponse`)
- [ ] Typecheck passes

---

### US-011: Implement POST /api/subscribers endpoint

**Description:** As an API client, I want to register or update a subscriber so it receives events matching its subscriptions.

**Acceptance Criteria:**

- [ ] `POST /api/subscribers` accepts JSON body with fields: `name` (required), `endpoint_url` (required), `auth_secret` (required), `max_parallel` (optional, default 1), `subscriptions` (required, array of objects with `subject_pattern` (required) and `filter` (optional jsonb))
- [ ] Request must include the global preshared secret in the `X-Slurpee-Admin-Secret` header; returns 401 if missing or incorrect
- [ ] If a subscriber with the same `endpoint_url` already exists, update its name, auth_secret, max_parallel, and replace its subscriptions (idempotent registration)
- [ ] If the subscriber is new, create it along with its subscriptions
- [ ] Returns 200 with the subscriber and its subscriptions as JSON
- [ ] Returns 400 with error details for missing required fields
- [ ] Follows existing API handler pattern
- [ ] Typecheck passes

---

### US-012: Implement event delivery engine

**Description:** As the system, I want to deliver events to matching subscribers asynchronously so that subscribers receive data without blocking publishers.

**Acceptance Criteria:**

- [ ] After an event is stored, find all subscriptions whose `subject_pattern` matches the event's `subject` using glob/wildcard matching (e.g., `order.*` matches `order.created` and `order.shipped`; `*` matches all subjects)
- [ ] For each matching subscriber, send an HTTP POST to the subscriber's `endpoint_url` with the event data as the JSON body
- [ ] Include the subscriber's `auth_secret` in a request header (e.g., `X-Slurpee-Secret`)
- [ ] Respect each subscriber's `max_parallel` setting — do not exceed that many concurrent deliveries to a single subscriber
- [ ] Record each delivery attempt in the `delivery_attempts` table (request headers, response status, response headers, response body, status)
- [ ] Mark delivery attempt as 'succeeded' on 2xx response, 'failed' otherwise
- [ ] Delivery runs in a background goroutine — does not block the POST /api/events response
- [ ] Typecheck passes

---

### US-013: Implement delivery retry with exponential backoff

**Description:** As the system, I want to retry failed deliveries with exponential backoff so transient failures are recovered automatically.

**Acceptance Criteria:**

- [ ] When a delivery attempt fails (non-2xx or network error), schedule a retry
- [ ] Retry delay uses exponential backoff (e.g., 1s, 2s, 4s, 8s, 16s, ... capped at a max interval)
- [ ] Increment `retry_count` on the event for each retry cycle
- [ ] Global maximum retry count and backoff cap are configured via environment variables (e.g., `MAX_RETRIES`, `MAX_BACKOFF_SECONDS`)
- [ ] Per-subscription `max_retries` override takes precedence over the global default when set
- [ ] Stop retrying after the applicable maximum number of attempts is reached
- [ ] Update event `delivery_status` to reflect current state: 'pending', 'partial' (some succeeded, some still retrying), 'delivered' (all succeeded), 'failed' (max retries exhausted)
- [ ] Update `status_updated_at` on each status change
- [ ] Each retry attempt is recorded as a new row in `delivery_attempts`
- [ ] Typecheck passes

---

### US-014: Apply subscription filters to event delivery

**Description:** As a subscriber, I want to define optional JSON filters on my subscriptions so I only receive events whose data matches my criteria.

**Acceptance Criteria:**

- [ ] When a subscription has a non-null `filter` field, evaluate it against the event's `data` before delivering
- [ ] Filter is a JSON object of key-value pairs; all pairs must match (AND logic) against top-level keys in event data
- [ ] If the filter does not match, skip delivery for that subscription (no delivery attempt recorded)
- [ ] If the filter is null, deliver all events matching the subject pattern
- [ ] Typecheck passes

---

### US-015: Build sidebar navigation component

**Description:** As a user, I want a sidebar navigation so I can move between the main sections of the web interface.

**Acceptance Criteria:**

- [ ] Templ component in `components/` renders a sidebar with navigation links
- [ ] Links to: Events, Subscribers, Logging Config
- [ ] Current page is visually highlighted
- [ ] Uses DaisyUI menu/sidebar styling
- [ ] Integrated into `SimplePage` layout so it appears on all pages
- [ ] Typecheck passes
- [ ] Verify in browser using dev-browser skill

---

### US-016: Build events list page

**Description:** As a user, I want to see a paginated list of events so I can browse what has been published.

**Acceptance Criteria:**

- [ ] View at `/events` renders a table of events showing: subject, event ID (truncated), timestamp, delivery status
- [ ] Paginated (e.g., 25 per page) with next/previous controls
- [ ] Rows are clickable, linking to the event detail page
- [ ] Delivery status shown as a colored badge (DaisyUI badge component)
- [ ] Most recent events shown first
- [ ] Templ template in `views/`, handler follows existing pattern
- [ ] Typecheck passes
- [ ] Verify in browser using dev-browser skill

---

### US-017: Build event search and filtering

**Description:** As a user, I want to search and filter events by subject, date range, delivery status, and content so I can find specific events.

**Acceptance Criteria:**

- [ ] Filter bar above the events table with inputs for: subject (text), date range (start/end), delivery status (dropdown), content search (text — searches within event JSON data)
- [ ] Filters update the event list via HTMX (no full page reload)
- [ ] Multiple filters can be combined (AND logic)
- [ ] Clearing filters returns to the full event list
- [ ] Typecheck passes
- [ ] Verify in browser using dev-browser skill

---

### US-018: Build event detail page

**Description:** As a user, I want to view full details of a single event including its data and delivery history.

**Acceptance Criteria:**

- [ ] View at `/events/{id}` shows: event ID, subject, timestamp, trace ID, delivery status, retry count, full JSON data (formatted/pretty-printed)
- [ ] Below the event details, show a list of delivery attempts: subscriber endpoint, attempted at, response status code, status (succeeded/failed/pending)
- [ ] Each delivery attempt row is expandable to show full request/response details (headers, body)
- [ ] Templ template in `views/`, handler follows existing pattern
- [ ] Typecheck passes
- [ ] Verify in browser using dev-browser skill

---

### US-019: Build event creation page

**Description:** As a user, I want to create events from the web interface for testing and manual publishing.

**Acceptance Criteria:**

- [ ] View at `/events/new` with a form: subject (text input, required), data (textarea for JSON, required), trace ID (text input, optional)
- [ ] Submit button posts to `POST /api/events` and redirects to the newly created event's detail page on success
- [ ] Displays validation errors inline if subject or data is missing, or if data is not valid JSON
- [ ] Uses DaisyUI form components
- [ ] Typecheck passes
- [ ] Verify in browser using dev-browser skill

---

### US-020: Build event delivery replay

**Description:** As a user, I want to replay delivery of a specific event so that failed deliveries can be retried manually.

**Acceptance Criteria:**

- [ ] "Replay All" button on the event detail page (US-018) re-triggers delivery to all matching subscribers
- [ ] Per-subscriber "Replay" button next to each delivery attempt in the event detail view re-triggers delivery to that single subscriber only
- [ ] Replay resets the event's `delivery_status` to 'pending' and records new delivery attempts
- [ ] Buttons use HTMX to trigger replay without full page reload
- [ ] Shows confirmation dialog before replaying
- [ ] Typecheck passes
- [ ] Verify in browser using dev-browser skill

---

### US-021: Build subscribers list page

**Description:** As a user, I want to see a list of all registered subscribers so I can manage them.

**Acceptance Criteria:**

- [ ] View at `/subscribers` renders a table of subscribers: name, endpoint URL, max parallel, number of subscriptions, created at
- [ ] Rows are clickable, linking to the subscriber detail page
- [ ] Templ template in `views/`, handler follows existing pattern
- [ ] Typecheck passes
- [ ] Verify in browser using dev-browser skill

---

### US-022: Build subscriber detail and edit page

**Description:** As a user, I want to view and edit a subscriber's configuration and subscriptions.

**Acceptance Criteria:**

- [ ] View at `/subscribers/{id}` shows subscriber details: name, endpoint URL, auth secret (masked with reveal toggle), max parallel, created/updated timestamps
- [ ] Lists all subscriptions for the subscriber: subject pattern, filter (if any)
- [ ] Editable fields: name, auth secret, max parallel
- [ ] Can add new subscriptions (subject pattern + optional filter JSON)
- [ ] Can remove existing subscriptions
- [ ] Save button persists changes
- [ ] Uses DaisyUI form components
- [ ] Typecheck passes
- [ ] Verify in browser using dev-browser skill

---

### US-023: Build logging configuration page

**Description:** As a user, I want to configure which event data properties are logged for each subject so I can control log verbosity.

**Acceptance Criteria:**

- [ ] View at `/logging` shows a table of log configurations: subject, logged properties
- [ ] Can add a new log config: subject (text input), properties (comma-separated or multi-input for JSON property paths)
- [ ] Can edit existing log config to change the logged properties
- [ ] Can delete a log config
- [ ] Changes saved via HTMX (no full page reload)
- [ ] Uses DaisyUI form/table components
- [ ] Typecheck passes
- [ ] Verify in browser using dev-browser skill

---

### US-024: Implement configurable event data logging

**Description:** As the system, I want to log configured properties from event data when events are received, so operators can see relevant data in logs without logging everything.

**Acceptance Criteria:**

- [ ] When an event is received via `POST /api/events`, look up the log_config for the event's subject
- [ ] If a log_config exists, extract the listed property paths from the event data and include them as structured log fields (slog attributes)
- [ ] Event ID and subject are always logged regardless of log_config
- [ ] If no log_config exists for the subject, only log event ID and subject
- [ ] Typecheck passes

---

### US-025: Implement GET /api/events/{id} endpoint

**Description:** As an API client, I want to retrieve a single event by ID so I can inspect its details programmatically.

**Acceptance Criteria:**

- [ ] `GET /api/events/{id}` returns the event as JSON including all fields: id, subject, timestamp, trace_id, data, retry_count, delivery_status, status_updated_at
- [ ] Returns 404 with error message if the event ID does not exist
- [ ] Returns 400 if the ID is not a valid UUID
- [ ] Follows existing API handler pattern (`routeHandler`, `writeJsonResponse`)
- [ ] Typecheck passes

---

### US-026: Implement GET /api/subscribers endpoint

**Description:** As an API client, I want to list all registered subscribers so I can inspect the current subscriber state programmatically.

**Acceptance Criteria:**

- [ ] `GET /api/subscribers` returns a JSON array of all subscribers, each including: id, name, endpoint_url, max_parallel, created_at, updated_at, and their subscriptions (subject_pattern, filter, max_retries)
- [ ] Request must include the global preshared secret in the `X-Slurpee-Admin-Secret` header; returns 401 if missing or incorrect
- [ ] Returns an empty array if no subscribers are registered
- [ ] Follows existing API handler pattern (`routeHandler`, `writeJsonResponse`)
- [ ] Typecheck passes

---

### US-027: Update root redirect and welcome page

**Description:** As a user, I want the root URL to redirect to the events list instead of the welcome page, since that is the primary view.

**Acceptance Criteria:**

- [ ] `GET /` redirects to `/events` instead of `/welcome`
- [ ] Welcome page can be removed or kept as-is (not critical)
- [ ] Typecheck passes

## Functional Requirements

- FR-1: Events are stored in Postgres with UUID v7 IDs, subjects, timestamps, optional trace IDs, and arbitrary JSON data
- FR-2: Events are accepted via `POST /api/events`; missing `id` and `timestamp` are auto-assigned
- FR-3: Subscribers are registered or updated via `POST /api/subscribers`, keyed by `endpoint_url`
- FR-4: Subscribers define subscriptions with subject patterns and optional JSON filters
- FR-5: After an event is stored, the system asynchronously delivers it to all matching subscribers via HTTP POST
- FR-6: Delivery includes the subscriber's shared secret in request headers (`X-Slurpee-Secret`)
- FR-7: Each subscriber's `max_parallel` setting limits concurrent deliveries to that subscriber
- FR-8: Failed deliveries are retried with exponential backoff, up to a configurable max retry count
- FR-9: Each delivery attempt is recorded with request/response details and a status ('succeeded', 'failed', 'pending')
- FR-10: Event `delivery_status` is a rollup of attempt statuses: 'pending', 'partial', 'delivered', 'failed'
- FR-11: The web interface allows viewing and creating events
- FR-12: The web interface allows searching events by subject, date range, delivery status, and data content
- FR-13: The web interface allows replaying event delivery
- FR-14: The web interface allows viewing and modifying subscribers and their subscriptions
- FR-15: The web interface allows configuring which event data properties are logged per subject
- FR-16: All search and filtering uses HTMX for responsiveness (no full page reloads)
- FR-17: Per-subject logging configuration controls which event data properties appear in structured logs
- FR-18: Subject pattern matching uses glob/wildcard syntax (e.g., `order.*` matches `order.created`)
- FR-19: Maximum retry count is configurable globally via environment variable, with per-subscription override
- FR-20: Events are immutable once published — no delete or update via API or web UI
- FR-21: `GET /api/events/{id}` returns a single event by ID
- FR-22: `GET /api/subscribers` returns the list of all registered subscribers with their subscriptions
- FR-23: Delivery replay supports both all-subscribers and individual-subscriber modes
- FR-24: The subscribers API (`POST /api/subscribers`, `GET /api/subscribers`) is protected by a global preshared secret sent via `X-Slurpee-Admin-Secret` header, configured via environment variable

## Non-Goals

- Authentication on the web interface (intended to run behind a firewall)
- Event schema validation or enforcement (Slurpee is schema-agnostic)
- Event ordering guarantees beyond best-effort
- Subscriber health checks or heartbeats
- Event expiration or TTL
- Batch event publishing (one event per API call)
- WebSocket or SSE-based real-time updates in the web UI
- Multi-tenancy or user accounts
- Rate limiting on the API
- Event deletion or mutation — events are immutable once published

## Design Considerations

- Use DaisyUI 5 components throughout: tables, badges, forms, buttons, modals, menus
- Dark theme (`data-theme="dark"`) is already set in SimplePage — maintain consistency
- HTMX 2.0.8 is already included — use `hx-get`, `hx-post`, `hx-target`, `hx-swap` for dynamic UI updates
- Reuse the existing `SimplePage` and `HeaderBar` layout components
- Add a sidebar nav component that integrates into SimplePage
- Existing SVG icon components can be used in navigation

## Technical Considerations

- **sqlc v1.30** is configured with pgx/v5 and prepared queries — all queries in `queries/` generate Go code in `db/`
- **sql-migrate v1.8.1** manages schema — migrations go in `schema/` directory
- **UUID v7** for event IDs — use a Go library like `github.com/google/uuid` (v7 support) or `github.com/gofrs/uuid`
- **Admin secret** — global preshared secret configured via environment variable (`ADMIN_SECRET`); used to authenticate subscriber API requests via `X-Slurpee-Admin-Secret` header
- **Exponential backoff** — implement with a goroutine-based worker or use a simple ticker; no external job queue; global max retries and backoff cap configured via environment variables (`MAX_RETRIES`, `MAX_BACKOFF_SECONDS`), with per-subscription override via `subscriptions.max_retries`
- **Delivery concurrency** — use a semaphore or worker pool per subscriber, bounded by `max_parallel`
- **Subject pattern matching** — use glob/wildcard syntax (e.g., `order.*` matches `order.created`); Go's `path.Match` or a similar glob library can be used
- **JSON filter evaluation** — implement simple key-value matching against top-level event data fields
- All API responses follow the existing pattern: `writeJsonResponse()` for JSON, standard HTTP status codes
- All views follow the existing pattern: `init()` route registration, `routeHandler()` wrapper, Templ rendering

## Success Metrics

- Events published via API are stored and retrievable within the same request cycle
- Delivery to healthy subscribers completes within seconds of event publication
- Failed deliveries are retried automatically and recover when the subscriber becomes healthy
- Web UI allows finding any event by subject or date within a few clicks
- Subscribers can be registered, updated, and inspected without direct database access

## Resolved Decisions

- **Subject pattern matching:** Yes — glob/wildcard syntax (e.g., `order.*` matches `order.created`)
- **Retry configuration:** Global max retries and backoff cap via environment variables; per-subscription `max_retries` override
- **Delivery replay:** Both individual-subscriber and all-subscribers replay supported
- **Event mutability:** Events are immutable once published — no delete or update
- **GET /api/events/{id}:** Yes — added as US-025
- **GET /api/subscribers:** Yes — added as US-026
