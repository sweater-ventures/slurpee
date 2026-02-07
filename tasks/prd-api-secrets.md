# PRD: Pre-Shared API Secrets with Scoped Access

## Introduction

Add a security layer to Slurpee Event Broker using pre-shared API secrets with scoped access control. Each secret has two independent scope dimensions: a `subject_pattern` that limits which events can be sent, and a set of associated subscribers that can be edited. A single secret can manage multiple subscribers. All operations (API and UI) require a valid secret, with read-only access granted to any valid secret holder. An existing `ADMIN_SECRET` environment variable controls access to the secrets management interface.

## Goals

- Prevent unauthorized access to all Slurpee operations (both API and UI)
- Scope event sending via a subject pattern stored directly on the API key
- Scope subscriber management to specific subscribers (many-to-many)
- Provide session-based UI authentication (enter secret once per browser session)
- Allow administrators to manage secrets through the UI using the existing `ADMIN_SECRET`
- Store secrets securely using bcrypt hashing (plaintext shown only at creation time)

## User Stories

### US-001: API Secrets Database Tables

**Description:** As a developer, I need database tables to store hashed API secrets with their send scope and subscriber associations so the system can validate and authorize incoming requests.

**Acceptance Criteria:**

- [ ] New `api_secrets` table with columns: `id` (UUID PK), `name` (TEXT, human-friendly label), `secret_hash` (TEXT, bcrypt hash), `subject_pattern` (TEXT, SQL LIKE pattern limiting which event subjects can be sent, e.g. `order.%`), `created_at` (TIMESTAMPTZ)
- [ ] New `api_secret_subscribers` join table with columns: `api_secret_id` (UUID FK to api_secrets), `subscriber_id` (UUID FK to subscribers), PRIMARY KEY (`api_secret_id`, `subscriber_id`)
- [ ] Foreign keys with CASCADE delete on both sides (deleting a secret removes its subscriber links; deleting a subscriber removes its secret links)
- [ ] Migration file(s) created in `schema/` directory
- [ ] sqlc queries: `InsertApiSecret`, `ListApiSecrets`, `DeleteApiSecret`, `GetApiSecretByID`, `AddApiSecretSubscriber`, `RemoveApiSecretSubscriber`, `ListSubscribersForApiSecret`, `ListApiSecretsForSubscriber`
- [ ] Run `sqlc generate` successfully
- [ ] `go build` passes

### US-002: Secret Hashing and Validation Helpers

**Description:** As a developer, I need utility functions to hash secrets at creation time and validate incoming secrets against stored hashes so that authentication logic is centralized and reusable.

**Acceptance Criteria:**

- [ ] Function to generate a random API secret string (32+ chars, crypto/rand)
- [ ] Function to hash a secret using bcrypt (cost 10)
- [ ] Function `ValidateSecret(ctx, db, plaintextSecret) -> (*ApiSecret, error)` that iterates all stored hashes and returns the matching secret record or error
- [ ] Function `CheckSubscriberScope(ctx, db, secret *ApiSecret, subscriberID UUID) -> bool` to verify a secret is associated with a given subscriber (via join table)
- [ ] Function `CheckSendScope(secret *ApiSecret, subject string) -> bool` that checks if the event subject matches the secret's `subject_pattern` using SQL LIKE semantics (Go-side matching with `%` and `_` wildcards)
- [ ] `go build` and `go vet` pass

### US-003: Protect API Endpoints with Secret Validation

**Description:** As an API consumer, I must provide a valid `X-Slurpee-Secret` header to access any API endpoint, so that unauthorized callers are rejected.

**Acceptance Criteria:**

- [ ] `POST /api/events`: Requires `X-Slurpee-Secret` header. The event's subject must match the secret's `subject_pattern`. Returns 401 if invalid secret, 403 if subject doesn't match pattern.
- [ ] `GET /api/events/{id}`: Requires any valid `X-Slurpee-Secret` header (read-only, any scope). Returns 401 if invalid.
- [ ] `POST /api/subscribers` and `GET /api/subscribers`: Continue requiring `X-Slurpee-Admin-Secret` (no change to existing admin behavior)
- [ ] All 401/403 responses return JSON error body: `{"error": "message"}`
- [ ] `go build` and `go vet` pass

### US-004: Session-Based UI Authentication

**Description:** As a UI user, I want to enter my secret once on a login page and have it remembered for my browser session, so I don't need to re-enter it for every action.

**Acceptance Criteria:**

- [ ] `GET /login` renders a login form with a single secret input field and submit button
- [ ] `POST /login` validates the secret against stored hashes. On success, sets an HTTP-only session cookie and redirects to `/events`. On failure, re-renders login with error message. Log failed login attempts.
- [ ] Admin login: If the entered secret matches `ADMIN_SECRET` env var, create an admin session (grants full access including secrets management)
- [ ] `POST /logout` clears the session cookie and redirects to `/login`
- [ ] Session stored in-memory (map of session token -> scope info). Session token is a crypto/rand generated string. Scope info includes: secret ID, subject_pattern, associated subscriber IDs, admin flag, and expiry time (24 hours from login).
- [ ] Expired sessions rejected on access and cleaned up; user redirected to `/login`
- [ ] Logout link visible in the sidebar/header when authenticated
- [ ] `go build` and `go vet` pass

### US-005: UI Auth Middleware

**Description:** As a system operator, I want all UI routes protected by session authentication so that unauthenticated users cannot view or modify any data.

**Acceptance Criteria:**

- [ ] Middleware checks for valid session cookie on all UI routes except `GET /login`, `POST /login`, `GET /version`, and static assets
- [ ] Unauthenticated requests redirect to `/login` (302)
- [ ] Authenticated session injects scope info into request context for downstream handlers
- [ ] Read-only pages (event list, event detail, subscriber list, subscriber detail, logging config) accessible with any valid session
- [ ] Write operations (subscriber edit, subscription create/delete, event create, event replay) check that session scope matches:
  - Subscriber edit/subscriptions: target subscriber must be in the session's associated subscriber list
  - Event create/replay: event subject must match the session's `subject_pattern`
  - Admin sessions bypass all scope checks
- [ ] SSE stream endpoint (`/events/stream`, `/events/stream/missed`) accessible with any valid session
- [ ] `go build` and `go vet` pass

### US-006: Secrets Management Page

**Description:** As an administrator, I want a UI page to create and delete API secrets so I can onboard and offboard API consumers without restarting the server.

**Acceptance Criteria:**

- [ ] `GET /secrets` page accessible only with admin session (logged in with `ADMIN_SECRET`). Non-admin sessions get 403.
- [ ] Page lists all existing secrets: name, subject pattern, associated subscriber names (comma-separated), created date. Secret hash is NOT shown (bcrypt, irreversible).
- [ ] "Create Secret" form with: name (text input), subject pattern (text input, e.g. `order.%` or `%` for all), subscribers (multi-select checkboxes of existing subscribers — may be left empty for send-only keys)
- [ ] On creation: generate random secret, hash with bcrypt, store hash in DB, create join table entries for selected subscribers. Display the plaintext secret ONCE in a success banner with copy-to-clipboard button. Warn user it won't be shown again.
- [ ] Subscriber association constraint: when adding subscribers to a secret, all subscribers must share the same host and port as the first subscriber associated with the secret. Display validation error if a subscriber with a different host:port is selected.
- [ ] Edit button per secret to modify subscriber associations and subject pattern (but NOT regenerate the secret itself)
- [ ] Delete button per secret with confirmation. Deletes the secret and its subscriber associations from the DB.
- [ ] Navigation link to `/secrets` visible in sidebar only for admin sessions
- [ ] `go build` and `go vet` pass

### US-007: Scope-Aware UI Controls

**Description:** As a UI user with a subscriber-scoped session, I want to see edit controls only for resources within my scope, so the UI reflects my actual permissions.

**Acceptance Criteria:**

- [ ] Subscriber list page: all subscribers visible (read-only), but edit links/buttons only shown for subscribers in the session's associated subscriber list. Admin sees all edit controls.
- [ ] Subscriber detail page: edit form, subscription add/delete buttons hidden if subscriber is not in the session's associated subscriber list. Admin sees all controls.
- [ ] Event create page: accessible to all authenticated users. On submit, scope is validated server-side (event subject must match session's `subject_pattern`). If no match, show error.
- [ ] Event replay buttons: visible to admin sessions and sessions whose `subject_pattern` matches the event subject. Hidden otherwise.
- [ ] `go build` and `go vet` pass

## Functional Requirements

- FR-1: Add `api_secrets` table with `id`, `name`, `secret_hash`, `subject_pattern`, `created_at`
- FR-2: Add `api_secret_subscribers` join table with `api_secret_id` (FK), `subscriber_id` (FK), composite PK
- FR-3: Hash secrets with bcrypt (cost 10) before storage; never store plaintext
- FR-4: Generate secrets using crypto/rand (minimum 32 characters, URL-safe base64)
- FR-5: API endpoints validate `X-Slurpee-Secret` header against all stored bcrypt hashes
- FR-6: Event creation scope check: event subject must match the secret's `subject_pattern` using SQL LIKE semantics (`%` = any chars, `_` = single char)
- FR-7: Subscriber edit scope check: target subscriber must exist in the secret's `api_secret_subscribers` entries
- FR-8: Session cookie: HTTP-only, Do not set Secure, SameSite=Lax, path=/
- FR-9: Session store: in-memory Go map protected by sync.RWMutex, mapping session token to scope (secret ID, subject_pattern, subscriber IDs list, or admin flag)
- FR-10: Admin session created when login secret matches `ADMIN_SECRET` env var — bypasses all scope restrictions
- FR-11: Redirect unauthenticated UI requests to `/login`
- FR-12: `POST /api/subscribers` and `GET /api/subscribers` continue to use `X-Slurpee-Admin-Secret` header (existing behavior preserved)
- FR-13: CASCADE delete on both FKs in join table (deleting a secret or subscriber cleans up associations)
- FR-14: Session TTL: 24 hours from login. Expired sessions are rejected and cleaned up.
- FR-15: Log failed login attempts (slog warning with request context)
- FR-16: Secrets may have an empty subscriber list (send-only keys with no subscriber management access)
- FR-17: Subscriber association constraint: all subscribers linked to a single secret must share the same host and port (parsed from `endpoint_url`). Enforced at creation and edit time.

## Non-Goals

- No OAuth, JWT, or token refresh — this is simple pre-shared secret auth
- No rate limiting on login attempts (failed attempts are logged but not throttled)
- No secret rotation/expiry — secrets are valid until deleted
- No per-subscription scope granularity (subscriber scope is all-or-nothing per subscriber)
- No multi-factor authentication
- No API endpoint for secret management (UI-only, admin-only)
- No session persistence across server restarts (in-memory sessions cleared on restart)

## Technical Considerations

- **Bcrypt iteration on every API request**: With a small number of secrets (< 50), iterating bcrypt comparisons is acceptable. If this becomes a bottleneck, consider adding a secret prefix/ID for direct lookup.
- **Session storage**: In-memory map is sufficient for single-instance deployment. If horizontal scaling is needed later, move sessions to the database.
- **Subject matching for send scope**: The secret's own `subject_pattern` uses SQL LIKE semantics (`%` = any sequence, `_` = single char). Implement Go-side matching for runtime checks (no DB round-trip needed since the pattern is on the secret itself).
- **Cookie security**: Don't use `Secure` flag. Always use `HttpOnly` and `SameSite=Lax`.
- **Existing admin secret**: The `ADMIN_SECRET` env var already protects `POST /api/subscribers` and `GET /api/subscribers`. This behavior is unchanged. The admin secret additionally grants admin UI sessions.

## Success Metrics

- All API endpoints return 401 for missing/invalid secrets
- All UI pages redirect to login when unauthenticated
- Secrets cannot modify subscribers outside their associated subscriber list
- Event creation is rejected when the event subject doesn't match the secret's `subject_pattern`
- Admin can create and delete secrets through the UI
- Plaintext secret displayed exactly once at creation time

## Open Questions

None — all questions resolved and incorporated into requirements above.
