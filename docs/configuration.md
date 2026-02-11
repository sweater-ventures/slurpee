# Configuration Reference

Slurpee is configured through environment variables and/or command-line flags. In dev mode (`--dev`), variables are also loaded from a `.env` file.

## Configuration Options

| Flag | Environment Variable | Default | Description |
|------|---------------------|---------|-------------|
| `--dev` | `DEV_MODE` | `false` | Enable development mode. Loads `.env` file and sets log level to debug by default. |
| `-p`, `--port` | `LISTEN_PORT` | `8005` | HTTP server listen port. |
| `--log-level` | `LOG_LEVEL` | `default` | Log level: `debug`, `info`, `warn`. When `default`, uses `debug` in dev mode, `info` otherwise. |
| `--db-host` | `DB_HOST` | `localhost` | PostgreSQL server hostname. |
| `--db-name` | `DB_NAME` | `slurpee` | PostgreSQL database name. |
| `--db-port` | `DB_PORT` | `5432` | PostgreSQL server port. |
| `--db-max-conns` | `DB_MAX_CONNS` | `10` | Maximum database connection pool size. |
| `--db-min-conns` | `DB_MIN_CONNS` | `1` | Minimum database connection pool size. |
| `--db-ssl-mode` | `DB_SSL_MODE` | `disable` | PostgreSQL SSL mode (`disable`, `require`, `verify-ca`, `verify-full`). |
| `--db-username` | `DB_USERNAME` | `slurpee` | Database username for the application (DML operations). |
| `--db-password` | `DB_PASSWORD` | `badpassword` | Database password for the application user. |
| `--base-url` | `BASE_URL` | `http://localhost:8005` | Base URL for the application. |
| `--admin-secret` | `ADMIN_SECRET` | _(empty)_ | Pre-shared secret for web UI login and admin API endpoints. **Required for production use.** |
| `--max-parallel` | `MAX_PARALLEL` | `1` | Default maximum concurrent deliveries per subscriber. |
| `--max-retries` | `MAX_RETRIES` | `5` | Maximum delivery retry attempts per subscription. |
| `--max-backoff-seconds` | `MAX_BACKOFF_SECONDS` | `300` | Maximum backoff delay in seconds (cap for exponential backoff). |
| `--delivery-queue-size` | `DELIVERY_QUEUE_SIZE` | `5000` | Capacity of the internal delivery task queue. |
| `--delivery-workers` | `DELIVERY_WORKERS` | `10` | Number of concurrent delivery worker goroutines. |
| `--delivery-chan-size` | `DELIVERY_CHAN_SIZE` | `1000` | Buffer size of the inbound event delivery channel. |

## Database Setup

Slurpee uses PostgreSQL and expects two database roles:

1. **Admin role** — owns the database and runs schema migrations (used by `sql-migrate`)
2. **Application role** — has SELECT, INSERT, UPDATE, DELETE permissions on tables (used by Slurpee at runtime)

### Setting up PostgreSQL manually

```sql
-- Create roles
CREATE ROLE slurpee_admin LOGIN PASSWORD 'your-admin-password';
CREATE ROLE slurpee LOGIN PASSWORD 'your-app-password';

-- Create database
CREATE DATABASE slurpee OWNER slurpee_admin;

-- Connect to the database and set permissions
\connect slurpee

GRANT ALL ON SCHEMA public TO slurpee_admin;
GRANT USAGE ON SCHEMA public TO slurpee;

GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO slurpee;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO slurpee;

-- Auto-grant permissions on future tables created by the admin role
ALTER DEFAULT PRIVILEGES FOR ROLE slurpee_admin IN SCHEMA public
    GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO slurpee;
ALTER DEFAULT PRIVILEGES FOR ROLE slurpee_admin IN SCHEMA public
    GRANT USAGE, SELECT ON SEQUENCES TO slurpee;
```

### Using the development Docker Compose

The `docker-db/` directory provides a Docker Compose setup for local development:

```bash
cd docker-db
docker compose up -d
```

This starts PostgreSQL 16 on port 5432 and runs the init script at `docker-db/init/01-create-users-and-db.sql` to create both roles and the database.

### Running migrations

Schema migrations are managed with [sql-migrate](https://github.com/rubenv/sql-migrate). The migration files live in the `schema/` directory.

To apply migrations manually:

```bash
# Set environment variables for the admin role
export DB_HOST=localhost
export DB_NAME=slurpee
export DB_ADMIN_USERNAME=slurpee_admin
export DB_ADMIN_PASSWORD=your-admin-password
export DB_SSL_MODE=disable

sql-migrate up
```

The `dbconfig.yml` file references these environment variables:

```yaml
development:
  dialect: postgres
  dir: schema
  datasource: host=${DB_HOST} dbname=${DB_NAME} user=${DB_ADMIN_USERNAME} password=${DB_ADMIN_PASSWORD} sslmode=${DB_SSL_MODE}
```

## Docker Deployment

The provided `Dockerfile` builds a multi-stage Alpine Linux image:

1. **Build stage** — compiles templ templates, compresses static assets, and builds the Go binary
2. **Runtime stage** — minimal Alpine image with the binary, migrations, and `sql-migrate`

The Docker entrypoint automatically runs `sql-migrate up` before starting the server, so migrations are applied on every container start.

### Running with Docker

```bash
docker run -p 8005:8005 \
  -e DB_HOST=your-db-host \
  -e DB_NAME=slurpee \
  -e DB_USERNAME=slurpee \
  -e DB_PASSWORD=your-app-password \
  -e DB_ADMIN_USERNAME=slurpee_admin \
  -e DB_ADMIN_PASSWORD=your-admin-password \
  -e DB_SSL_MODE=disable \
  -e ADMIN_SECRET=your-admin-secret \
  slurpee
```

## Delivery Tuning

The delivery pipeline has several settings that affect throughput and resource usage:

| Setting | What it controls | Increase when... |
|---------|-----------------|------------------|
| `DELIVERY_WORKERS` | Worker goroutines processing delivery tasks | You need higher throughput and have many subscribers |
| `DELIVERY_QUEUE_SIZE` | Buffer for internal task queue | Workers can't keep up and events back up |
| `DELIVERY_CHAN_SIZE` | Buffer for inbound event channel | High event publish rate causes back-pressure |
| `MAX_PARALLEL` | Per-subscriber concurrency limit | A subscriber can handle more concurrent requests |
| `MAX_RETRIES` | Retry attempts before giving up | Subscribers have intermittent failures |
| `MAX_BACKOFF_SECONDS` | Cap on exponential backoff delay | You want to limit how long retries stretch out |

The delivery pipeline flow is:

```
Event published
  -> DeliveryChan (buffered: DELIVERY_CHAN_SIZE)
    -> Dispatcher matches subscriptions
      -> Task Queue (buffered: DELIVERY_QUEUE_SIZE)
        -> Workers (count: DELIVERY_WORKERS)
          -> Per-subscriber semaphore (MAX_PARALLEL)
            -> HTTP POST to subscriber
```

For most deployments, the defaults are reasonable. If you're processing thousands of events per second, start by increasing `DELIVERY_WORKERS` and `MAX_PARALLEL`.
