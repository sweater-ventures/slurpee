# Web Interface Guide

Slurpee includes a built-in web dashboard for managing events, subscribers, API secrets, and logging configuration. The interface uses HTMX for dynamic updates and DaisyUI for styling.

## Login

Navigate to your Slurpee instance (default: `http://localhost:8005`). You'll be redirected to the login page.

Enter the admin secret (the `ADMIN_SECRET` environment variable configured on the server). On success, a session cookie is set and you're redirected to the events page.

To log out, use the logout option in the interface. This clears the session.

## Events

### Event list

The events page shows a searchable, filterable list of all events.

**Filters:**

- **Subject** — filter by event subject (exact match or partial)
- **Delivery status** — filter by status (pending, delivered, partial, failed, recorded)
- **Date range** — filter events by timestamp
- **Content** — search within event data JSON
- **Trace ID** — find events by trace ID

The event list updates in real time via Server-Sent Events (SSE). New events appear automatically without refreshing the page. Delivery status changes are also reflected live.

### Event detail

Click an event to view its full details:

- **Event data** — the complete JSON payload
- **Metadata** — subject, timestamp, trace ID, delivery status, retry count
- **Delivery attempts** — a log of every delivery attempt with request/response details (headers, status code, response body, timestamp)

### Replay

From the event detail page, you can replay delivery to any subscriber. This resets the event's status to pending, performs a single delivery attempt, and updates the status based on the result.

### Create event

The web UI includes a form for creating events directly, useful for testing. You can specify the subject, data (as JSON), and optional trace ID.

## Subscribers

### Subscriber list

View all registered subscribers with their endpoint URLs, subscription count, and timestamps.

### Subscriber detail

Click a subscriber to view and edit its details:

- **Name** — editable label
- **Endpoint URL** — the webhook URL
- **Max parallel** — concurrent delivery limit

### Subscription management

From the subscriber detail page, manage subscriptions:

- **Add subscription** — specify a subject pattern, optional JSON filter, and optional max retries
- **Edit subscription** — modify filter or max retries for an existing subscription
- **Delete subscription** — remove a subscription

## API Secrets

### Secret list

View all API secrets with their names, subject patterns, and associated subscribers.

### Create secret

When creating a new API secret:

1. Provide a **name** (e.g., "payment-service-prod")
2. Set a **subject pattern** to restrict which subjects this secret can publish to (e.g., `payment.*`)
3. Optionally associate the secret with specific **subscribers** (scoped by host:port)

After creation, the **plaintext secret value is displayed once**. Copy it immediately — it cannot be retrieved later (only the bcrypt hash is stored).

### Edit secret

You can update a secret's name, subject pattern, and subscriber associations. The secret value itself cannot be changed — create a new secret if needed.

### Delete secret

Delete a secret to revoke access. This takes effect immediately (the secret cache has a short TTL).

## Logging Configuration

The logging page lets you configure per-subject property extraction for server logs.

When Slurpee receives an event, it always logs the event ID and subject. With logging configuration, you can additionally extract and log specific fields from the event data.

**Example:** For subject `order.created`, configure properties `order_id, customer_id`. When an order event is received, the log line will include these fields extracted from the event data.

- **Subject** — the event subject to configure (exact match)
- **Properties** — comma-separated list of top-level JSON keys to extract from event data

Configurations are upserted by subject — setting properties for an existing subject updates the configuration.
