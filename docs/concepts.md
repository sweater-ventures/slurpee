# Core Concepts

This page explains the key abstractions in Slurpee and how they fit together.

## Events

An event is a JSON message published to Slurpee for delivery to subscribers.

Every event has:

| Field | Type | Description |
|-------|------|-------------|
| `id` | UUID v7 | Auto-generated if not provided. Time-ordered for natural sorting. |
| `subject` | string | A topic string used for routing (e.g., `order.created`, `user.signup`). |
| `data` | JSON object | The event payload. Slurpee does not enforce any schema. |
| `timestamp` | RFC 3339 | When the event occurred. Defaults to the current time. |
| `trace_id` | UUID (optional) | For correlating events across distributed systems. |

Events are immutable once created. Slurpee tracks delivery metadata (retry count, delivery status, status timestamps) separately.

## Subjects and Patterns

Subjects are free-form strings that categorize events. By convention, dots are used as separators (e.g., `order.created`, `payment.failed`), but Slurpee does not enforce any format.

Subscriptions and API secrets use **patterns** to match subjects. Patterns support two wildcards:

| Wildcard | Meaning | Example |
|----------|---------|---------|
| `*` | Matches any sequence of characters (including empty) | `order.*` matches `order.created`, `order.updated` |
| `_` | Matches exactly one character | `user._` matches `user.a` but not `user.ab` |

Patterns use the same semantics as SQL `LIKE`, with `*` in place of `%`.

## Subscribers

A subscriber is an HTTP endpoint that receives events via webhook POST requests.

| Field | Description |
|-------|-------------|
| `name` | Human-readable label for the subscriber. |
| `endpoint_url` | The URL that Slurpee will POST events to. Must be unique across all subscribers. |
| `auth_secret` | A shared secret sent in the `X-Slurpee-Secret` header on every delivery, so the subscriber can verify requests came from Slurpee. |
| `max_parallel` | Maximum concurrent deliveries to this endpoint. Defaults to the server's `MAX_PARALLEL` setting. |

Subscribers are upserted by `endpoint_url` — calling the API with the same URL updates the existing subscriber rather than creating a duplicate.

## Subscriptions

A subscription connects a subscriber to a set of events. Each subscriber can have multiple subscriptions.

| Field | Description |
|-------|-------------|
| `subject_pattern` | A pattern (see above) that determines which events this subscription matches. |
| `filter` | Optional JSON object. If present, only events whose `data` contains all the specified key-value pairs (AND logic, top-level keys only) are delivered. |
| `max_retries` | Optional override for the server's global `MAX_RETRIES` setting. |

When an event is published, Slurpee evaluates all subscriptions. If multiple subscriptions for the same subscriber match, the event is delivered once — using the subscription with the highest effective `max_retries`.

## Delivery

Slurpee delivers events asynchronously through a worker pool.

### Webhook format

When delivering an event, Slurpee sends an HTTP POST to the subscriber's `endpoint_url`:

**Headers:**

| Header | Value |
|--------|-------|
| `Content-Type` | `application/json` |
| `X-Slurpee-Secret` | The subscriber's `auth_secret` |
| `X-Event-ID` | The event UUID |
| `X-Event-Subject` | The event subject string |

**Body:** The event's `data` JSON object (not the full event envelope).

A delivery is considered successful if the subscriber responds with an HTTP 2xx status code.

### Retry logic

Failed deliveries are retried with exponential backoff:

- Delays: 1s, 2s, 4s, 8s, 16s, ... doubling each attempt
- Capped at `MAX_BACKOFF_SECONDS` (default: 300s / 5 minutes)
- Maximum attempts controlled by `MAX_RETRIES` (default: 5), overridable per subscription

### Delivery statuses

| Status | Meaning |
|--------|---------|
| `pending` | Event received, delivery not yet attempted. |
| `delivered` | All matching subscribers received the event successfully. |
| `partial` | Some subscribers succeeded, others are still being retried. |
| `failed` | At least one subscriber exhausted all retries without success. |
| `recorded` | No matching subscriptions exist for this event's subject. The event is stored but was not delivered. |

Every delivery attempt (successful or not) is recorded with full request/response details for auditing.

### Resume on restart

On startup, Slurpee queries for events in `pending` or `partial` status and resumes delivery. Pending events are re-dispatched normally. Partial events skip subscribers that already received the event successfully and continue retries from where they left off.

## API Secrets

API secrets authenticate clients that publish events via the REST API.

| Field | Description |
|-------|-------------|
| `name` | Human-readable label (e.g., "payment-service-prod"). |
| `subject_pattern` | A pattern restricting which subjects this secret can publish to. |
| `secret_hash` | bcrypt hash of the secret value. The plaintext is shown once at creation and cannot be retrieved later. |
| `subscribers` | Optional association with specific subscribers (scoped by host:port). |

API secrets provide two dimensions of access control:

1. **Subject scope** — the secret can only publish events matching its `subject_pattern`
2. **Subscriber scope** — optionally limits which subscribers can receive events from this secret

See the [API Reference](api-reference.md) for authentication header details.

## Admin Secret

The admin secret is a server-level configuration value (`ADMIN_SECRET` environment variable) used to:

- Log in to the web interface
- Authenticate subscriber management API calls (`X-Slurpee-Admin-Secret` header)

Unlike API secrets (which are stored in the database and managed per-client), the admin secret is a single value set at deployment time.
