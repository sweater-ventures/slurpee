# PRD: Live Event Stream

## Introduction

Add real-time live event watching to the events page using Server-Sent Events (SSE). Users can toggle into a "live mode" that streams new events, delivery status changes, and individual delivery attempts directly into the events table as they happen. The existing filter bar applies to the live stream. When a user reconnects after being away, they see a "you missed X events" indicator with the option to load them.

## Goals

- Allow users to watch events flow through the system in real-time without manual refresh
- Stream all event lifecycle updates: creation, status changes, and delivery attempts
- Reuse the existing filter bar so users can focus the live stream on specific subjects, statuses, or content
- Handle disconnections gracefully with a missed-event indicator and catch-up mechanism

## User Stories

### US-001: SSE Event Bus Infrastructure
**Description:** As a developer, I need a pub/sub event bus in the application layer so that delivery and event-creation code can broadcast updates to connected SSE clients.

**Acceptance Criteria:**
- [ ] Add an `EventBus` to the `Application` struct that supports subscribe/unsubscribe/publish
- [ ] Each SSE client gets its own channel; slow consumers are dropped (non-blocking send)
- [ ] Bus message type includes: event type (created/status_changed/delivery_attempt), event data, and timestamp
- [ ] Delivery dispatcher publishes messages on event creation, status change, and delivery attempt completion
- [ ] Event creation handler (`eventCreateSubmitHandler`) publishes a "created" message
- [ ] `go build ./...` compiles successfully
- [ ] `go vet ./...` passes

### US-002: SSE Endpoint
**Description:** As a developer, I need an SSE HTTP endpoint so the browser can open a long-lived connection and receive real-time event updates.

**Acceptance Criteria:**
- [ ] `GET /events/stream` endpoint returns `text/event-stream` content type
- [ ] Endpoint subscribes to the EventBus and forwards messages as SSE data frames
- [ ] Supports query parameters matching existing filters (subject, status, date_from, date_to, content) to filter server-side before sending
- [ ] Sends a `lastEventId` field with each SSE message (using a monotonic sequence or timestamp) so the browser can report its last-seen ID on reconnect
- [ ] Connection cleanly unsubscribes from EventBus when the client disconnects
- [ ] Sends periodic keepalive comments (`: keepalive\n\n`) every 15 seconds to prevent proxy timeouts
- [ ] `go build ./...` compiles successfully
- [ ] `go vet ./...` passes

### US-003: Live Mode Toggle UI
**Description:** As a user, I want a toggle button on the events page to switch between the normal paginated view and live streaming mode so I can watch events in real-time.

**Acceptance Criteria:**
- [ ] "Go Live" button appears next to the "New Event" button on the events page
- [ ] Clicking the button switches the events table to live mode: clears existing rows and begins streaming
- [ ] Button changes to "Stop" (with a visual indicator like a pulsing dot) while live mode is active
- [ ] Clicking "Stop" closes the SSE connection and returns to the normal paginated view (reloads current page of events)
- [ ] New events prepend to the top of the table in live mode (newest first)
- [ ] Each row in live mode is clickable and navigates to the event detail page (same as normal mode)
- [ ] Live mode limits the visible table to the most recent 100 events to prevent unbounded DOM growth
- [ ] `go build ./...` compiles successfully
- [ ] `go vet ./...` passes
- [ ] Verify in browser using dev-browser skill

### US-004: Live Filtering
**Description:** As a user, I want the filter bar to apply to the live stream so I can watch only the events I care about.

**Acceptance Criteria:**
- [ ] When live mode is active, changing filters closes the current SSE connection and opens a new one with updated filter query parameters
- [ ] The "Search" button in the filter bar works in live mode (reconnects SSE with new filters)
- [ ] The "Clear" button resets filters and reconnects SSE without filters
- [ ] Server-side filtering works for subject (LIKE), status (exact match), and content (JSON containment)
- [ ] `go build ./...` compiles successfully
- [ ] `go vet ./...` passes
- [ ] Verify in browser using dev-browser skill

### US-005: Delivery Attempt Updates in Live Stream
**Description:** As a user, I want to see delivery status changes and individual delivery attempts in the live stream so I can monitor the full event lifecycle.

**Acceptance Criteria:**
- [ ] When an event's delivery status changes (pending → partial → delivered/failed), the corresponding row in the live table updates its status badge in-place
- [ ] Delivery attempt events are shown as sub-updates: either an inline indicator on the event row or a brief toast/flash showing "Delivered to subscriber X" or "Failed delivery to subscriber X"
- [ ] Status badge colors match the existing convention (success=delivered, error=failed, warning=pending, info=partial)
- [ ] `go build ./...` compiles successfully
- [ ] `go vet ./...` passes
- [ ] Verify in browser using dev-browser skill

### US-006: Reconnection with Missed Event Indicator
**Description:** As a user, when my connection drops and reconnects I want to know how many events I missed and have the option to load them.

**Acceptance Criteria:**
- [ ] When SSE reconnects, the browser sends `Last-Event-ID` header (built-in EventSource behavior)
- [ ] The SSE endpoint accepts `Last-Event-ID` and queries for events created after that point
- [ ] If missed events exist, a banner appears at the top of the live table: "You missed X events while away" with a "Load" button
- [ ] Clicking "Load" prepends the missed events to the live table
- [ ] If no events were missed, streaming resumes silently
- [ ] `go build ./...` compiles successfully
- [ ] `go vet ./...` passes
- [ ] Verify in browser using dev-browser skill

## Functional Requirements

- FR-1: The `Application` struct must include an `EventBus` that supports multiple concurrent SSE subscribers
- FR-2: The EventBus must use non-blocking sends to prevent slow clients from blocking the delivery pipeline
- FR-3: The SSE endpoint (`GET /events/stream`) must set headers: `Content-Type: text/event-stream`, `Cache-Control: no-cache`, `Connection: keep-alive`
- FR-4: Each SSE message must include an `id` field for reconnection support via `Last-Event-ID`
- FR-5: The SSE endpoint must support all existing filter query parameters (subject, status, date_from, date_to, content)
- FR-6: The delivery dispatcher must publish bus messages at three points: event received, status updated, delivery attempt completed
- FR-7: The live mode toggle must be a client-side JavaScript EventSource connection (not HTMX SSE extension, for better control)
- FR-8: The live table must cap at 100 visible rows, removing oldest rows when the limit is exceeded
- FR-9: Keepalive comments must be sent every 15 seconds to prevent connection drops behind proxies
- FR-10: On reconnect, the server must calculate missed events using `Last-Event-ID` and return a count before resuming the stream

## Non-Goals

- No WebSocket support — SSE is sufficient for this one-directional stream
- No persistent event bus or message queuing — the bus is in-memory only for connected clients
- No live updates on the event detail page (only the events list page)
- No sound or desktop notifications for new events
- No multi-tab coordination or shared SSE connections
- No authentication/authorization for the SSE endpoint beyond what exists for other routes

## Technical Considerations

- **EventBus pattern:** A simple Go struct with a mutex-protected map of subscriber channels. Publish iterates subscribers and does non-blocking channel sends (drop if full). This matches the existing channel-based architecture (e.g., `DeliveryChan`).
- **SSE message format:** JSON payloads with fields: `type` (created|status_changed|delivery_attempt), `event` (EventRow data), `delivery` (attempt details, if applicable). Each message gets a monotonically increasing `id`.
- **Integration points:** Hook into `dispatchEvent`, `updateEventStatus`, and `deliverToSubscriber` in `api/delivery.go` plus `eventCreateSubmitHandler` in `views/events.go`.
- **Client-side:** Use native `EventSource` API with JavaScript in a `<script>` block within the templ template. Parse incoming JSON and manipulate the DOM to prepend rows / update status badges.
- **Templ code generation:** After modifying `.templ` files, run `templ generate` to produce the corresponding `_templ.go` files.
- **Filter application:** Apply filters server-side in the SSE handler before sending to clients. Reuse the same filter logic from `eventsListHandler`.

## Success Metrics

- Events appear in the live stream within 1 second of creation
- Status changes reflect in-place within 1 second of the delivery completing
- SSE connection remains stable for at least 30 minutes without manual intervention
- Reconnection after network interruption shows accurate missed-event count

## Open Questions

- Should we add a visual/audio notification option for high-importance events in a future iteration?
- Should the live stream auto-pause if the browser tab is not visible (to save resources)?
