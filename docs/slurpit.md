# slurpit — Load Testing CLI

`slurpit` is a command-line load testing tool for Slurpee. It can publish events at a controlled rate, receive events via webhook, and run end-to-end benchmarks with latency measurement.

## Building

```bash
go build -o slurpit ./slurpit
```

## Subcommands

### slurpit send

Publish events to Slurpee at a configurable rate.

```bash
slurpit send \
  --url http://localhost:8005 \
  --secret-id YOUR_SECRET_UUID \
  --secret YOUR_SECRET_VALUE \
  --subject loadtest.event \
  --rate 50 \
  --count 1000 \
  --workers 4
```

Each event is published with a `sent_at` timestamp in the data payload, which the receive mode uses for latency calculation.

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--url` | _(required)_ | Slurpee base URL |
| `--secret-id` | _(required)_ | API secret UUID |
| `--secret` | _(required)_ | API secret value |
| `--subject` | `loadtest.event` | Event subject |
| `--rate` | `10` | Events per second |
| `--count` | `100` | Total events to send |
| `--workers` | `1` | Concurrent sender goroutines |

**Output:** Live progress showing sent count, error count, and worker count. Final summary with total sent, errors, elapsed time, and actual events/sec.

---

### slurpit receive

Start a webhook listener that auto-registers as a Slurpee subscriber, receives events, and measures end-to-end latency.

```bash
slurpit receive \
  --url http://localhost:8005 \
  --admin-secret YOUR_ADMIN_SECRET \
  --listen :9090 \
  --endpoint-url http://your-host:9090 \
  --subject "loadtest.*" \
  --duration 60s
```

On startup, the receiver:
1. Registers a subscriber via `POST /api/subscribers` with a random name
2. Starts an HTTP server to receive webhook deliveries
3. Measures latency by comparing each event's `sent_at` field to the receive time
4. On shutdown, deregisters the subscriber via `DELETE /api/subscribers/{id}`

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--url` | _(required)_ | Slurpee base URL |
| `--admin-secret` | _(required)_ | Admin secret for subscriber registration |
| `--listen` | `:9090` | Local listen address |
| `--endpoint-url` | _(required)_ | Publicly reachable URL for this receiver (what Slurpee will POST to) |
| `--subject` | `loadtest.*` | Subject pattern to subscribe to |
| `--duration` | `30s` | How long to listen |

**Output:** Live stats showing received count, rate per second, and min/max/mean latency. Final summary with total received, throughput, and latency percentiles (p50, p95, p99).

---

### slurpit bench

Run a combined send + receive benchmark. This is the recommended way to measure end-to-end performance.

```bash
slurpit bench \
  --url http://localhost:8005 \
  --secret-id YOUR_SECRET_UUID \
  --secret YOUR_SECRET_VALUE \
  --admin-secret YOUR_ADMIN_SECRET \
  --listen :9090 \
  --endpoint-url http://your-host:9090 \
  --subject loadtest.event \
  --subscribe-pattern "loadtest.*" \
  --rate 100 \
  --count 500 \
  --workers 4 \
  --drain 10s
```

The bench command:
1. Starts a webhook receiver
2. Registers a subscriber
3. Sends events at the configured rate
4. Waits for the drain period to collect remaining deliveries
5. Shuts down the receiver and deregisters the subscriber
6. Prints a combined summary

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--url` | _(required)_ | Slurpee base URL |
| `--secret-id` | _(required)_ | API secret UUID |
| `--secret` | _(required)_ | API secret value |
| `--admin-secret` | _(required)_ | Admin secret for subscriber registration |
| `--listen` | `:9090` | Local listen address for webhook receiver |
| `--endpoint-url` | _(required)_ | Publicly reachable URL for the receiver |
| `--subject` | `loadtest.event` | Event subject to send |
| `--subscribe-pattern` | `loadtest.*` | Subject pattern for subscriber |
| `--rate` | `10` | Events per second |
| `--count` | `100` | Total events to send |
| `--workers` | `1` | Concurrent sender goroutines |
| `--drain` | `5s` | Time to wait after sending for remaining events |

**Example output:**

```
Receiver listening on :9090
Registered subscriber (ID: 0193a5b0-1234-7000-8000-000000000001)
Send complete: 500/500 sent, 0 errors, 5.0s, 100.0 events/sec
Draining for 10s...

=== Bench Summary ===
  Sent           : 500/500 events (0 errors)
  Send rate      : 100.0 events/sec
  Received       : 500 events
  Delivery       : 100.0%
  Total duration : 15.2s
  Latency min    : 2.3 ms
  Latency max    : 145.6 ms
  Latency mean   : 12.4 ms
  Latency p50    : 8.1 ms
  Latency p95    : 45.2 ms
  Latency p99    : 98.7 ms
=====================
```

## Prerequisites

Before running slurpit, you need:

1. A running Slurpee instance
2. An **API secret** (created via the web UI) with a `subject_pattern` that matches your test subject
3. The **admin secret** (for receive/bench modes, to register the webhook subscriber)
4. The receiver's endpoint URL must be reachable from the Slurpee instance (e.g., if Slurpee runs in Docker, use your host's IP rather than `localhost`)

## Tips

- Start with a low rate and increase gradually to find your system's limits
- Use `--workers` to increase sender parallelism if the HTTP client becomes the bottleneck
- The `--drain` flag in bench mode should be long enough for all events to be delivered; increase it if you see delivery percentage below 100%
- Latency measurements depend on clock synchronization between the sender and receiver; for most accurate results, run both on the same machine
