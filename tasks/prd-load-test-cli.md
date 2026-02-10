# PRD: Slurpee Load Test CLI (`slurpit`)

## Introduction

A command-line load testing tool for the Slurpee event broker. `slurpit` has two modes: **send** (publish events at a configurable rate) and **receive** (spin up a webhook endpoint, register as a subscriber, and measure delivery latency). Together they enable end-to-end load testing with real-time stats displayed in the terminal.

## Goals

- Provide a simple CLI to generate sustained event load against a Slurpee instance
- Measure end-to-end delivery latency (event creation time to webhook receipt)
- Report live throughput and latency statistics (min, max, mean, p50, p95, p99)
- Keep the tool self-contained with minimal setup — receiver auto-registers as a subscriber

## User Stories

### US-001: Add DELETE /api/subscribers/{id} endpoint
**Description:** As the slurpit receiver, I need an API endpoint to delete a subscriber so the tool can clean up after itself on shutdown.

**Acceptance Criteria:**
- [ ] `DELETE /api/subscribers/{id}` endpoint added to the API router in `api/subscribers.go`
- [ ] Requires `X-Slurpee-Admin-Secret` header (same auth as other subscriber endpoints)
- [ ] Deletes the subscriber's subscriptions first (via `DeleteSubscriptionsForSubscriber`), then the subscriber (via `DeleteSubscriber`)
- [ ] Returns HTTP 204 No Content on success
- [ ] Returns HTTP 404 if subscriber ID not found
- [ ] Returns HTTP 401/403 for missing/invalid admin secret
- [ ] `go build ./...` compiles successfully

### US-002: CLI scaffolding with go-arg subcommands

**Description:** As a developer, I want a `slurpit` binary with `send` and `receive` subcommands so I can run either mode independently.

**Acceptance Criteria:**

- [ ] New `slurpit/` directory at the project root with `main.go`
- [ ] Uses `go-arg` with subcommand pattern: `slurpit send [flags]` and `slurpit receive [flags]`
- [ ] Running with no subcommand prints usage/help
- [ ] Running with `--help` on each subcommand shows available flags
- [ ] `go build ./slurpit` compiles successfully

### US-003: Send mode — publish events at a configurable rate

**Description:** As a tester, I want to send events to Slurpee at a specified rate so I can simulate production load.

**Acceptance Criteria:**

- [ ] `send` subcommand accepts flags: `--url` (Slurpee base URL), `--secret-id` (API secret UUID), `--secret` (API secret value), `--subject` (event subject, default `loadtest.event`), `--rate` (events per second, default 10), `--count` (total events to send, default 100)
- [ ] Events are sent via `POST /api/events` with proper `X-Slurpee-Secret-ID` and `X-Slurpee-Secret` headers
- [ ] Each event's `data` payload includes a `sent_at` field with the timestamp (RFC 3339 / nanosecond precision) the event was generated
- [ ] Events are sent at approximately the configured rate using a ticker or rate limiter
- [ ] Sends exactly `--count` events then exits
- [ ] Prints a running count of events sent (updated in real-time on a single line)
- [ ] On completion, prints a summary: total sent, elapsed time, actual rate achieved
- [ ] Handles errors gracefully — logs failures without stopping the run, reports error count in summary
- [ ] `go build ./slurpit` compiles successfully

### US-004: Receive mode — webhook endpoint with auto-registration

**Description:** As a tester, I want the receiver to start an HTTP server and register itself as a Slurpee subscriber so I can measure delivery performance with zero manual setup.

**Acceptance Criteria:**

- [ ] `receive` subcommand accepts flags: `--url` (Slurpee base URL), `--admin-secret` (admin secret for subscriber registration), `--listen` (local listen address, default `:9090`), `--endpoint-url` (the publicly reachable URL for this receiver, required — e.g. `http://host.docker.internal:9090` or ngrok URL), `--subject` (subject pattern to subscribe to, default `loadtest.*`), `--duration` (how long to listen, default `30s`)
- [ ] On startup, registers a subscriber via `POST /api/subscribers` with the configured endpoint URL and subject pattern
- [ ] Starts an HTTP server that accepts webhook deliveries at `POST /` (or a specific path)
- [ ] Validates incoming webhooks have `X-Event-ID` and `X-Event-Subject` headers
- [ ] Responds with HTTP 200 to acknowledge delivery
- [ ] Runs for `--duration` then shuts down gracefully
- [ ] On shutdown, deregisters the subscriber via `DELETE /api/subscribers/{id}` (or equivalent admin API)
- [ ] On shutdown, prints a message indicating the run is complete
- [ ] `go build ./slurpit` compiles successfully

### US-005: Latency calculation from sent_at timestamp

**Description:** As a tester, I want the receiver to calculate delivery latency by comparing the `sent_at` timestamp in the event data to the time the webhook was received.

**Acceptance Criteria:**

- [ ] Parses `sent_at` from the incoming event data JSON payload
- [ ] Calculates latency as `time.Now() - sent_at` for each received event
- [ ] Stores latency values for statistical aggregation
- [ ] Gracefully handles missing or unparseable `sent_at` (logs warning, skips that event for latency stats)
- [ ] `go build ./slurpit` compiles successfully

### US-006: Live terminal statistics display

**Description:** As a tester, I want to see throughput and latency statistics updating live in my terminal so I can observe system behavior in real-time.

**Acceptance Criteria:**

- [ ] Receiver prints a stats line that updates in place (e.g., using `\r` carriage return) every 1 second
- [ ] Live stats include: total events received, events/sec (last 1s window), latency min/max/mean (rolling)
- [ ] On completion (after `--duration` expires), prints a final summary block with: total events received, overall events/sec, and latency statistics: min, max, mean, p50, p95, p99
- [ ] Percentiles are calculated from all received events over the full run
- [ ] Output is human-readable with aligned columns and units (e.g., `ms` for latency)
- [ ] `go build ./slurpit` compiles successfully

## Functional Requirements

- FR-1: The tool is built as a standalone Go binary in the `slurpit/` directory, using `go-arg` for CLI parsing
- FR-2: The `send` subcommand publishes events to `POST /api/events` with authentication headers and a `sent_at` timestamp in the event data
- FR-3: The `send` subcommand rate-limits event publishing to the configured `--rate` events per second
- FR-4: The `receive` subcommand starts a local HTTP server and auto-registers as a Slurpee subscriber via the admin API
- FR-5: The `receive` subcommand calculates per-event latency from `sent_at` in the event data payload
- FR-6: The `receive` subcommand displays live-updating stats every second and a final summary with percentiles on exit
- FR-7: Both subcommands require Slurpee connection details (`--url`, auth headers) as required flags

## Non-Goals

- No distributed load generation (single machine only)
- No persistent result storage or export to file
- No TLS/mTLS support for the receiver's webhook server
- No support for custom event data beyond `sent_at`

## Technical Considerations

- Use `time.NewTicker` or `golang.org/x/time/rate` for send rate limiting
- Latency calculation assumes sender and receiver clocks are synchronized (same machine or NTP-synced)
- For percentile calculation, store all latency values in a slice and sort on completion — sufficient for load test durations
- The receiver's subscriber name should include a random suffix to avoid conflicts (e.g., `slurpit-receiver-a1b2c3`)
- Use `encoding/json` for event data parsing — no need for a database or sqlc

## Success Metrics

- Can sustain 100+ events/sec send rate without dropping events
- Latency statistics are accurate within clock skew tolerance
- Live stats update smoothly without terminal flicker

## Open Questions

None — all resolved.
