# Sotería — Predictive Recall Intelligence & Containment Layer

Sotería knows a food product is dangerous before anyone announces it, contains it at lot-level precision instead of wiping whole SKUs, and produces legal-grade proof of how fast you acted. Full product requirements: [`docs/prd.md`](docs/prd.md).

This is a contract-first, 3-person microservice build (Go services, RabbitMQ events, React storefront). Each person owns a set of folders end-to-end; the only shared surface is [`/contracts`](contracts).

## Repository map

| Path | Owner | What |
|---|---|---|
| `contracts/` | Person 3 (all review) | Event JSON Schemas, OpenAPI specs, RabbitMQ topology registry |
| `libs/feedkit/` | Person 1 | Shared Go module for feed connectors: poll loop, dedup store, AMQP publisher, health/metrics |
| `services/ingestion-fda/` | Person 1 | openFDA enforcement + FDA press RSS → `recall.raw.received.v1` |
| `services/ingestion-usda/` | Person 1 | USDA FSIS recalls (meat/poultry/egg) → `recall.raw.received.v1` |
| `services/ingestion-rasff/` | Person 1 | EU RASFF notifications → `recall.raw.received.v1` |
| `docs/` | — | PRD and design notes |

## Quick start (ingestion services)

Requirements: Go 1.26+, Docker.

```sh
# 1. local broker (management UI on http://localhost:15672, guest/guest)
docker compose -f libs/feedkit/dev/docker-compose.yml up -d

# 2. see what a service would publish, without touching the broker
cd services/ingestion-fda && make dry-run ARGS="--backfill-days 14"

# 3. publish for real (single pass), then run again to watch dedup yield 0
make once ARGS="--backfill-days 14"
make once ARGS="--backfill-days 14"

# 4. long-running poller with health + metrics
make run          # GET :8080/healthz  :8080/readyz  :8080/metrics
```

Configuration is environment-driven; see [`.env.example`](.env.example). Every service accepts `--once`, `--dry-run`, `--backfill-days`, `--interval`, `--db`, `--http`, `--rabbitmq`, `--log-level`.

## Testing

Each module is independent (`go.work` ties them together for local builds):

```sh
for m in libs/feedkit services/ingestion-fda services/ingestion-usda services/ingestion-rasff; do (cd $m && go vet ./... && go test ./...); done
```

Every service has a **contract test** that validates fixture-derived events against `contracts/events/recall.raw.received.v1.json`, so schema drift fails in CI ([`.github/workflows/ingestion.yml`](.github/workflows/ingestion.yml)).

## Event flow

```
openFDA ─┐
FDA RSS ─┤ ingestion-fda ──┐
FSIS ────── ingestion-usda ─┼─▶ exchange ingestion.x ──▶ resolution-service (P3)
RASFF ───── ingestion-rasff ┘   key ingestion.recall.raw.received.v1
```

See [`contracts/rabbitmq-topology.md`](contracts/rabbitmq-topology.md) for message properties and delivery semantics (at-least-once; consumers dedupe on `event_id` or `(source, source_id)`).
