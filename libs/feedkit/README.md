# feedkit

Shared Go module for Sotería's feed connectors (`services/ingestion-*`). A connector implements one interface; feedkit does everything else.

```go
type Source interface {
    Name() string                                              // fda_enforcement | fda_press | usda_fsis | eu_rasff
    Fetch(ctx context.Context, since time.Time) ([]Item, error) // notices published at/after since
}
```

## Packages

| Package | Role |
|---|---|
| `source` | `Source`, `Item`, `Normalized` — the connector contract |
| `event` | Builds and validates `recall.raw.received.v1`; exchange/routing-key constants |
| `dedup` | SQLite store (pure Go, no cgo): `(source, source_id)` seen-set + last-success per source |
| `httpx` | HTTP client: timeout, descriptive User-Agent, retries with backoff + jitter, honors `Retry-After`, never retries 4xx |
| `publish` | `Publisher` interface; `AMQP` (durable topic exchange, publisher confirms, inline + background reconnect, `Connected()`); `DryRun` (JSON lines to stdout) |
| `health` | Per-source state, broker probe, `/healthz` (503 when unhealthy), `/readyz`, Prometheus-text `/metrics` |
| `poller` | The loop: fetch → dedupe → build event → validate → publish (confirm) → **then** mark seen |
| `app` | One-call bootstrap: flags/env, store, publisher, pollers, health server, graceful shutdown |
| `contracttest` | Test helper: validate payloads against `/contracts/events/*.json` |
| `dev/` | `docker-compose.yml` for a local RabbitMQ |

## Guarantees

- **At-least-once.** A notice is marked seen only after the broker confirms the publish. A crash in between re-emits it with a new `event_id`; consumers dedupe on `event_id` or `(source, source_id)`.
- **A feed outage is visible.** Three consecutive failed polls, or no success within 3× the poll interval, flips `/healthz` to 503 with `last_error`. A lost broker connection does too — even while the feed is idle.
- **A contract violation never poisons a poll.** An item that fails `event.Validate()` is logged and skipped (not marked seen); the rest of the tick proceeds.
- **Dry-run is side-effect-free.** `--dry-run` prints events and uses a throwaway in-memory store.

## Writing a connector

1. Implement `source.Source`. Put the untouched upstream record in `Item.Raw` (must be a JSON object) and only *project* fields into `Normalized` — no parsing intelligence (that's the resolution-service's job).
2. Record a real response into `testdata/` and write a mapper test.
3. Add a `contract_test.go` in the service root that runs fixture items through `event.New(...).Marshal()` and `contracttest.Validate`.
4. Wire it in `cmd/main.go` with `app.Main(producer, builder)`.

## Health semantics

| Signal | Unhealthy when |
|---|---|
| `sources.<name>.consecutive_failures` | ≥ 3 |
| `sources.<name>.last_success_at` | older than 3 × interval (or never, past 3 × interval after start) |
| `broker.connected` | AMQP connection down (not monitored in dry-run) |

`/metrics` exposes `soteria_ingestion_up`, `soteria_ingestion_broker_connected`, and per-source gauges/counters (`..._consecutive_failures`, `..._items_published_total`, `..._fetch_failures_total`, …).
