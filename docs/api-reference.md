# REST API Reference

All API endpoints are prefixed with `/api/`. Responses use `Content-Type: application/json`.

## Authentication

Slurpee uses two authentication mechanisms:

| Mechanism | Used for | Headers |
|-----------|----------|---------|
| **API secret** | Publishing and reading events | `X-Slurpee-Secret-ID` (UUID) + `X-Slurpee-Secret` (plaintext) |
| **Admin secret** | Managing subscribers | `X-Slurpee-Admin-Secret` (plaintext, matches `ADMIN_SECRET` env var) |

API secrets are created in the web UI. Each secret has a UUID identifier and a plaintext value shown once at creation. The `X-Slurpee-Secret-ID` header tells Slurpee which secret to validate against (avoiding a full table scan of bcrypt hashes).

---

## Events

### POST /api/events

Publish a new event.

**Authentication:** API secret (`X-Slurpee-Secret-ID` + `X-Slurpee-Secret`)

The secret's `subject_pattern` must match the event's subject, or the request is rejected with 403.

**Request body:**

```json
{
  "subject": "order.created",
  "data": {
    "order_id": "12345",
    "amount": 99.99,
    "currency": "USD"
  },
  "id": "0193a5b0-7e1a-7000-8000-000000000001",
  "trace_id": "0193a5b0-7e1a-7000-8000-000000000099",
  "timestamp": "2026-02-11T20:00:00Z"
}
```

| Field | Required | Description |
|-------|----------|-------------|
| `subject` | Yes | Event subject string. |
| `data` | Yes | JSON object payload. |
| `id` | No | UUID for the event. Auto-generated (v7) if omitted. |
| `trace_id` | No | UUID for distributed tracing correlation. |
| `timestamp` | No | RFC 3339 timestamp. Defaults to current time. |

**Response (201 Created):**

```json
{
  "id": "0193a5b0-7e1a-7000-8000-000000000001",
  "subject": "order.created",
  "timestamp": "2026-02-11T20:00:00Z",
  "trace_id": "0193a5b0-7e1a-7000-8000-000000000099",
  "data": {
    "order_id": "12345",
    "amount": 99.99,
    "currency": "USD"
  },
  "retry_count": 0,
  "delivery_status": "pending",
  "status_updated_at": "2026-02-11T20:00:00.123Z"
}
```

**Example:**

```bash
curl -X POST http://localhost:8005/api/events \
  -H "Content-Type: application/json" \
  -H "X-Slurpee-Secret-ID: YOUR_SECRET_UUID" \
  -H "X-Slurpee-Secret: YOUR_SECRET_VALUE" \
  -d '{
    "subject": "order.created",
    "data": {"order_id": "12345", "amount": 99.99}
  }'
```

---

### GET /api/events/{id}

Retrieve a single event by ID.

**Authentication:** API secret (`X-Slurpee-Secret-ID` + `X-Slurpee-Secret`). Any valid secret works — subject scope is not checked for reads.

**Response (200 OK):**

```json
{
  "id": "0193a5b0-7e1a-7000-8000-000000000001",
  "subject": "order.created",
  "timestamp": "2026-02-11T20:00:00Z",
  "trace_id": null,
  "data": {
    "order_id": "12345",
    "amount": 99.99
  },
  "retry_count": 0,
  "delivery_status": "delivered",
  "status_updated_at": "2026-02-11T20:00:01.456Z"
}
```

**Example:**

```bash
curl http://localhost:8005/api/events/0193a5b0-7e1a-7000-8000-000000000001 \
  -H "X-Slurpee-Secret-ID: YOUR_SECRET_UUID" \
  -H "X-Slurpee-Secret: YOUR_SECRET_VALUE"
```

---

## Subscribers

### POST /api/subscribers

Register or update a subscriber. If a subscriber with the same `endpoint_url` already exists, it is updated (upsert).

**Authentication:** Admin secret (`X-Slurpee-Admin-Secret`)

**Request body:**

```json
{
  "name": "payment-service",
  "endpoint_url": "https://payments.example.com/webhooks/slurpee",
  "auth_secret": "my-webhook-verification-secret",
  "max_parallel": 5,
  "subscriptions": [
    {
      "subject_pattern": "order.*",
      "filter": {"currency": "USD"},
      "max_retries": 10
    },
    {
      "subject_pattern": "payment.*"
    }
  ]
}
```

| Field | Required | Description |
|-------|----------|-------------|
| `name` | Yes | Human-readable subscriber name. |
| `endpoint_url` | Yes | Webhook URL. Must be unique. Used as the upsert key. |
| `auth_secret` | Yes | Secret sent in `X-Slurpee-Secret` header on deliveries. |
| `max_parallel` | No | Max concurrent deliveries. Defaults to server `MAX_PARALLEL`. |
| `subscriptions` | Yes | Array of subscription objects (at least one). |

Each subscription object:

| Field | Required | Description |
|-------|----------|-------------|
| `subject_pattern` | Yes | Pattern to match event subjects. |
| `filter` | No | JSON object — all key-value pairs must match event data (AND logic). |
| `max_retries` | No | Override for the server's `MAX_RETRIES`. |

On upsert, subscriptions are synced: new patterns are added, existing patterns are updated, and patterns not in the request are deleted.

**Response (200 OK):**

```json
{
  "id": "0193a5b0-1234-7000-8000-000000000001",
  "name": "payment-service",
  "endpoint_url": "https://payments.example.com/webhooks/slurpee",
  "max_parallel": 5,
  "created_at": "2026-02-11T20:00:00Z",
  "updated_at": "2026-02-11T20:00:00Z",
  "subscriptions": [
    {
      "id": "0193a5b0-1234-7000-8000-000000000010",
      "subject_pattern": "order.*",
      "filter": {"currency": "USD"},
      "max_retries": 10,
      "created_at": "2026-02-11T20:00:00Z",
      "updated_at": "2026-02-11T20:00:00Z"
    },
    {
      "id": "0193a5b0-1234-7000-8000-000000000011",
      "subject_pattern": "payment.*",
      "filter": null,
      "max_retries": null,
      "created_at": "2026-02-11T20:00:00Z",
      "updated_at": "2026-02-11T20:00:00Z"
    }
  ]
}
```

**Example:**

```bash
curl -X POST http://localhost:8005/api/subscribers \
  -H "Content-Type: application/json" \
  -H "X-Slurpee-Admin-Secret: YOUR_ADMIN_SECRET" \
  -d '{
    "name": "my-service",
    "endpoint_url": "https://example.com/webhook",
    "auth_secret": "webhook-secret-123",
    "subscriptions": [
      {"subject_pattern": "order.*"}
    ]
  }'
```

---

### GET /api/subscribers

List all subscribers and their subscriptions.

**Authentication:** Admin secret (`X-Slurpee-Admin-Secret`)

**Response (200 OK):**

```json
[
  {
    "id": "0193a5b0-1234-7000-8000-000000000001",
    "name": "payment-service",
    "endpoint_url": "https://payments.example.com/webhooks/slurpee",
    "max_parallel": 5,
    "created_at": "2026-02-11T20:00:00Z",
    "updated_at": "2026-02-11T20:00:00Z",
    "subscriptions": [
      {
        "id": "0193a5b0-1234-7000-8000-000000000010",
        "subject_pattern": "order.*",
        "filter": null,
        "max_retries": null,
        "created_at": "2026-02-11T20:00:00Z",
        "updated_at": "2026-02-11T20:00:00Z"
      }
    ]
  }
]
```

**Example:**

```bash
curl http://localhost:8005/api/subscribers \
  -H "X-Slurpee-Admin-Secret: YOUR_ADMIN_SECRET"
```

---

### DELETE /api/subscribers/{id}

Delete a subscriber and all its subscriptions.

**Authentication:** Admin secret (`X-Slurpee-Admin-Secret`)

**Response:** 204 No Content

**Example:**

```bash
curl -X DELETE http://localhost:8005/api/subscribers/0193a5b0-1234-7000-8000-000000000001 \
  -H "X-Slurpee-Admin-Secret: YOUR_ADMIN_SECRET"
```

---

## Version

### GET /api/version

Returns the application name and version. No authentication required.

**Response (200 OK):**

```json
{
  "app": "slurpee",
  "version": "1.0.0"
}
```

**Example:**

```bash
curl http://localhost:8005/api/version
```

---

## Webhook Delivery Format

When Slurpee delivers an event to a subscriber, it sends an HTTP POST request:

**Headers:**

| Header | Value |
|--------|-------|
| `Content-Type` | `application/json` |
| `X-Slurpee-Secret` | The subscriber's `auth_secret` |
| `X-Event-ID` | The event UUID |
| `X-Event-Subject` | The event subject string |

**Body:** The event's `data` field as a JSON object. This is the raw data payload, not the full event envelope.

**Expected response:** Any HTTP 2xx status code indicates success. Any other status code (or connection error) is treated as a failure and triggers a retry.

Delivery requests have a 30-second timeout.

---

## Error Responses

All error responses follow this format:

```json
{
  "error": "Description of what went wrong"
}
```

| Status Code | Meaning |
|-------------|---------|
| 400 | Bad request — missing required fields, invalid UUID, malformed JSON |
| 401 | Unauthorized — missing or invalid authentication headers |
| 403 | Forbidden — subject not permitted by API secret scope |
| 404 | Not found — event or subscriber does not exist |
| 500 | Internal server error |
