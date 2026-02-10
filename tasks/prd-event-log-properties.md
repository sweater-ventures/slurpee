# PRD: Display Log Config Properties on Events List

## Introduction

The Slurpee event broker has a Logging Configuration feature (`log_config` table) that maps event subjects to arrays of property names to extract from event JSONB data. Currently this is only used for server-side structured logging. This feature surfaces those configured properties directly on the events list page — including in live mode via SSE — so operators can see key event data at a glance without clicking into event details.

## Goals

- Display log_config properties as inline key=value badges in a new "Properties" column on the events list
- Include property data in the SSE live stream so live mode rows show properties immediately
- Require zero configuration changes — leverage existing log_config entries automatically

## User Stories

### US-001: Add Properties Column to Events List Query

**Description:** As a developer, I need the events list queries to join against `log_config` and extract configured properties from each event's JSONB data, so the view layer has property data available to render.

**Acceptance Criteria:**
- [ ] Add a new SQL query (or modify existing) that joins events with log_config on subject and extracts the configured properties from the event's `data` JSONB column
- [ ] The query returns a property map or serialized key/value pairs alongside existing event fields
- [ ] Events with no matching log_config return empty/null for properties
- [ ] Run `sqlc generate` successfully
- [ ] `go build ./...` passes
- [ ] Existing tests pass

### US-002: Display Properties Column on Events List Page

**Description:** As an operator, I want to see a "Properties" column on the events list page showing key=value badges for each event's configured log properties, so I can see important event data without clicking into details.

**Acceptance Criteria:**
- [ ] A new "Properties" column appears in the events table between "Timestamp" and "Status"
- [ ] Properties render as compact key=value badges/tags (e.g., DaisyUI badge components)
- [ ] Events with no log_config show an empty cell (no placeholder text)
- [ ] Events with log_config but where a property key is missing from the event data omit that property (no blank badges)
- [ ] Properties display correctly for both filtered and unfiltered event list views
- [ ] `go build ./...` passes
- [ ] Existing tests pass

### US-003: Include Properties in EventBus Messages

**Description:** As a developer, I need the EventBus publish path to look up log_config and include extracted properties in the BusMessage, so SSE live mode can display properties on new event rows without a separate database query.

**Acceptance Criteria:**
- [ ] BusMessage struct gains a `Properties` field (map or key/value pairs)
- [ ] When an event is created, the publish path looks up log_config for the event's subject and extracts matching properties from event data
- [ ] If no log_config exists for the subject, `Properties` is empty/nil (not an error)
- [ ] `go build ./...` passes
- [ ] Existing tests pass

### US-004: Display Properties in Live Mode Event Rows

**Description:** As an operator, I want live mode event rows to show the same property badges as static rows, so I get the same information whether the page loaded normally or events arrived via SSE.

**Acceptance Criteria:**
- [ ] SSE `created` messages include property data in the JSON payload
- [ ] JavaScript `created` handler renders property badges in the new Properties column
- [ ] Badge styling matches the static page rendering from US-002
- [ ] Live mode `status_changed` messages do not need to include properties (they update existing rows)
- [ ] `go build ./...` passes
- [ ] Existing tests pass

### US-005: Include Properties in Missed Events Recovery

**Description:** As an operator, when I reconnect after a brief disconnection, missed events should include property data so recovered rows display properties correctly.

**Acceptance Criteria:**
- [ ] The `/events/missed` endpoint includes property data in its response
- [ ] Recovered event rows render property badges identically to live and static rows
- [ ] `go build ./...` passes
- [ ] Existing tests pass

## Functional Requirements

- FR-1: Add a SQL query that joins `events` with `log_config` on `events.subject = log_config.subject` and extracts properties from `events.data` using the `log_config.log_properties` array
- FR-2: Add a "Properties" column to the events list table, positioned between "Timestamp" and "Status"
- FR-3: Render each property as a compact badge/tag showing `key=value` format
- FR-4: Events with no matching log_config display an empty Properties cell
- FR-5: Extend `BusMessage` with a `Properties` field (e.g., `map[string]any` or `map[string]string`)
- FR-6: Populate `BusMessage.Properties` at event creation time by looking up log_config and extracting matching properties from event data
- FR-7: SSE `created` event payloads include the properties data
- FR-8: Frontend JavaScript renders property badges for live mode rows matching the server-rendered style
- FR-9: The `/events/missed` recovery endpoint includes property data for each returned event

## Non-Goals

- No filtering or sorting by property values
- No editing of log_config from the events list page (existing `/logging` page handles this)
- No property display on the event detail page (it already shows full JSON data)
- No caching of log_config lookups (can be added later if needed)

## Technical Considerations

- The `log_config` lookup uses exact subject match via `GetLogConfigBySubject` — this means events must match a log_config subject exactly (no pattern/wildcard matching)
- Event data is JSONB stored as `[]byte` in Go — property extraction requires `json.Unmarshal` into `map[string]any`
- The existing `LogEvent()` function in `api/events.go` already implements the pattern of looking up log_config and extracting properties — this logic should be reused or refactored into a shared helper
- For the SQL approach, PostgreSQL's `jsonb_each_text()` or direct `->>`  operators can extract properties, but doing it in Go may be simpler given the dynamic nature of the property list
- BusMessage is published in `api/events.go` (line ~165) and `views/events.go` (line ~408) — both publish paths need to include properties
- The SSE handler in `views/events.go` serializes BusMessage to JSON — adding the Properties field will automatically include it in the SSE payload

## Success Metrics

- Configured log properties visible on events list page without clicking into event details
- Live mode rows show properties identically to page-loaded rows
- No measurable performance degradation on the events list page

## Open Questions

- Should property values be truncated if they exceed a certain length? (e.g., long strings could break layout)
- If log_config uses pattern matching in the future, should this feature support it?
