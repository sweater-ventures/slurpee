# Slurpee Event Broker

Slurpee is a lightweight event broker that accepts events via a REST API and delivers them to HTTP webhook subscribers. All events are stored in PostgreSQL for durability, with automatic retries using exponential backoff for failed deliveries.

Slurpee provides both a REST API and a web dashboard for managing events, subscribers, and API secrets.

## Documentation

Full documentation is available in the [`docs/`](docs/) directory:

- [Getting Started](docs/README.md) — quick start guides for Docker and source builds
- [Core Concepts](docs/concepts.md) — events, subjects, subscribers, subscriptions, delivery, and API secrets
- [Configuration](docs/configuration.md) — all config options, database setup, Docker deployment, delivery tuning
- [API Reference](docs/api-reference.md) — complete REST API with curl examples
- [Web Interface](docs/web-ui.md) — dashboard walkthrough with screenshots
- [slurpit CLI](docs/slurpit.md) — load testing tool guide

## Development

Technologies used:

- Go standard library for HTTP serving (no web frameworks)
- [Templ](https://templ.guide) templates — compiles to Go code
- DaisyUI (version 5) for the web interface (Tailwind where needed)
- PostgreSQL for event storage
- [sqlc](https://sqlc.dev) (version 1.30) for type-safe database access
- sql-migrate (version 1.8.1) for schema migrations

### Project Structure

- `api/` — REST API endpoints
- `app/` — application core, delivery pipeline, secret management
- `components/` — reusable Templ UI components
- `config/` — configuration loading (env vars and CLI flags)
- `db/` — sqlc-generated database code
- `docker-db/` — Docker Compose for local PostgreSQL
- `middleware/` — HTTP middleware (logging, session auth)
- `queries/` — sqlc SQL query definitions
- `schema/` — database migration files
- `slurpit/` — load testing CLI tool
- `static/` — CSS, JS, and image assets
- `views/` — Templ page templates with application logic
