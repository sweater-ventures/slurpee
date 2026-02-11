# Slurpee User Guide

Slurpee is an event broker that accepts events via a REST API and delivers them to HTTP webhook subscribers. All events are stored in PostgreSQL for durability, with automatic retries using exponential backoff for failed deliveries.

## Features

- **REST API** for publishing events and managing subscribers
- **Webhook delivery** with configurable retry logic and exponential backoff
- **Subject-based routing** with wildcard pattern matching
- **Content filtering** on event data (top-level JSON key matching)
- **Scoped API secrets** restricting which subjects a client can publish to
- **Web dashboard** with real-time SSE updates, event search, and subscriber management
- **Delivery audit trail** recording every attempt with full request/response details
- **Resume on restart** — pending and partial deliveries continue after a server restart
- **Load testing CLI** (`slurpit`) for benchmarking end-to-end performance

## Quick Start with Docker

1. Start PostgreSQL (if you don't already have one):

   ```bash
   cd docker-db
   docker compose up -d
   ```

2. Run Slurpee:

   ```bash
   docker run -p 8005:8005 \
     -e DB_HOST=host.docker.internal \
     -e DB_NAME=slurpee \
     -e DB_USERNAME=slurpee \
     -e DB_PASSWORD=userpassword456 \
     -e DB_ADMIN_USERNAME=slurpee_admin \
     -e DB_ADMIN_PASSWORD=securepassword123 \
     -e DB_SSL_MODE=disable \
     -e ADMIN_SECRET=choose-a-strong-secret \
     slurpee
   ```

3. Open `http://localhost:8005` and log in with your admin secret.

## Quick Start from Source

**Prerequisites:** Go 1.25+, PostgreSQL, [sql-migrate](https://github.com/rubenv/sql-migrate)

1. Clone the repository and set up the database (see [Configuration](configuration.md#database-setup)).

2. Run migrations:

   ```bash
   export DB_HOST=localhost DB_NAME=slurpee DB_SSL_MODE=disable
   export DB_ADMIN_USERNAME=slurpee_admin DB_ADMIN_PASSWORD=your-admin-password
   sql-migrate up
   ```

3. Build and run:

   ```bash
   make
   ./slurpee --db-username slurpee --db-password your-app-password --admin-secret choose-a-strong-secret
   ```

   Or for development mode (with `.env` file and hot reload):

   ```bash
   make dev
   ```

## Your First Event

1. Open the web UI at `http://localhost:8005` and log in.

2. Go to **API Secrets** and create a new secret:
   - Name: `my-first-secret`
   - Subject pattern: `*` (allows all subjects)
   - Copy the plaintext secret value — it's shown only once.

3. Publish an event:

   ```bash
   curl -X POST http://localhost:8005/api/events \
     -H "Content-Type: application/json" \
     -H "X-Slurpee-Secret-ID: SECRET_UUID_FROM_STEP_2" \
     -H "X-Slurpee-Secret: SECRET_VALUE_FROM_STEP_2" \
     -d '{
       "subject": "hello.world",
       "data": {"message": "Hello from Slurpee!"}
     }'
   ```

4. The event appears in the web UI's event list. Since there are no subscribers, it will have status `recorded`.

5. To see delivery in action, register a subscriber via the web UI or the API (see [API Reference](api-reference.md#post-apisubscribers)).

## Documentation

| Document | Description |
|----------|-------------|
| [Core Concepts](concepts.md) | Events, subjects, subscribers, subscriptions, delivery, and API secrets |
| [Configuration](configuration.md) | All config options, database setup, Docker deployment, delivery tuning |
| [API Reference](api-reference.md) | Complete REST API with curl examples |
| [Web Interface](web-ui.md) | Dashboard walkthrough |
| [slurpit CLI](slurpit.md) | Load testing tool guide |
