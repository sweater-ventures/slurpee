# Slurpee Event Broker

Slurpee Event Broker is a simple event broker using REST endpoints to publish
and subscribe to events. Events are delivered via HTTP POST requests
to subscriber endpoints. All events are stored in a postgres database for
durability. If events fail to be delivered, the broker will retry using an
exponential back-off strategy.

Slurpee provides both a REST API and a Web interface for managing events.

Currently the web interface is unathenticated. It is intended to be run
behind a firewall. Delivery of events to subscribers is authenticated
using a shared secret in the request headers.

## Web Interface

The Web interface allows a user to:

- View and create events and their details
- View and replay event delivery
- Search for events by subject, date range, delivery status, and content
- View and modify subscribers and their subscriptions
- View logging configuration for event subjects

All search and filtering is handled using HTMX to provide a responsive
user experience. DaisyUI is used for styling of components.

## API

Events are accepted via HTTP POST requests to the `/events` endpoint. An
Event is saved to the database, then asynchronously delivered to all subscribers.

Subscribers are registered via the `/subscribers` endpoint. Subscribers can
safely subscribe repeatedly and the endpoint will update their subscriptions.
Subscriptions can define how many parallel deliveries can be made to a subscriber,
which subjects to subscribe to, and optional filtering on event data. Subscriptions
are unique per endpoint URL.

## Event Structure

Events consist of:

- a subject (string)
- an event id (UUID v7, auto assigned if not provided)
- a timestamp (auto assigned if not provided)
- an optional trace id (UUID)
- data (JSON object)

Additional properties that Slurpee uses, but can't be submitted by clients:

- retry count (integer, auto assigned)
- delivery attempts (list) each entry contains:
  - data from subscription for attempt (url endpoint)
  - timestamp of delivery attempt
  - headers sent
  - response status code
  - response headers
  - response body
  - evaluated status for attempt (succeeded, failed, pending)
- overall delivery status (rollup of delivery attempts, i.e. if all attempts succeeded,
  if any attempts are pending, etc)
- timestamp of latest status update

Slurpee does not care about or enforce any schema on the event data.
It always logs event id's and subjects when it receives events. For a particular
subject, properties from the event data can also be logged. This can be configured
via the web interface.

## Development

Technologies used:

- DaisyUI (version 5) for the web interface (Tailwind where needed)
- [Templ](https://templ.guide) templates -- compiles to go code
- golang standard library where possible (no other web frameworks)
- golang slog for logging
- postgres database for event storage
- [sqlc](https://sqlc.dev) (version 1.30) for postgres database access from go code
- sql-migrate (version 1.8.1) for schema migrations (schema is stored
  in `schema/` directory)

Technologies NEVER used:

- nodejs
- react
- angular
- vue
- nextjs
- bun
- deno

### Project Structure

- `api/` holds REST API endpoints for managing events and subscribers
- `app/` holds application data structure including data access
- `components/` holds Templ components for web interface. These are dumb components
  that do not have any application logic, nor domain knowledge. They should be mostly
  reusable outside of this project.
- `components/icons/` templ components for SVG icons
- `config/` holds configuration loading code, configuration is done through
  environment variables and command line flags. In dev mode `.env` is loaded for
  environment variables.
- `db/` this is the directory where sqlc generated code is stored. It is always
  generated from sqlc, and the latest version should be checked in.
- `docker-db/` this holds some utilities for helping developers run a local
  postgres database in docker for development.
- `middleware/` holds HTTP middleware for logging, tracing, etc.
- `queries/` holds sqlc formatted SQL queries. This will end up being generated
  into code in the `db/` directory by sqlc.
- `schema/` holds database schema migration files, `sql-migrate` is used to apply
  and create migrations.
- `static/` holds static assets for the web interface (CSS, JS, images).
- `views/` holds Templ templates for web interface pages. These templates
  include components from the `components/` directory, and have application logic
  to render data from the backend.
