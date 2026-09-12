# Core domain services

Sotería is a predictive recall intelligence and containment platform. When a food safety recall is
announced, most retailers pull the entire product line, destroying inventory that was never
contaminated. Sotería resolves a recall down to the exact lot number, holds only that lot, and gives
affected customers a confirmed substitute instead of a silent cancellation.

This document covers the **core domain layer**: the canonical event contracts, the RabbitMQ
topology, and the three services that carry an incident from a raw recall notice to a contained lot
and a rescued order.

## Table of contents

1. [Scope](#scope)
2. [System architecture](#system-architecture)
3. [Incident lifecycle](#incident-lifecycle)
4. [Services](#services)
5. [Shared libraries](#shared-libraries)
6. [Messaging](#messaging)
7. [Design rules](#design-rules)
8. [Running locally](#running-locally)
9. [Testing](#testing)
10. [Repository layout](#repository-layout)

## Scope

**In this repository**

- `contracts/`: event schemas, OpenAPI specifications, and the RabbitMQ topology registry.
- `services/resolution-service`: recall text to GTIN and lot codes, with confidence and evidence.
- `services/containment-service`: threshold policy, inventory holds, and the human review queue.
- `services/order-rescue-service`: in-flight order rescue with explicit customer consent.
- `libs/core/`: shared libraries (events, messaging, matching confidence, commerce and enrichment seams).

**Deliberately elsewhere**

- Recall feed ingestion, silent catalog diffing, anti-evasion monitoring, customer notification
  delivery, and the production Shopify client. These are owned by the integrations workstream and
  reach this layer purely as events, or through the interfaces declared in `libs/core/`.
- The storefront, the ops console, and the Open Food Facts integration layer. These are owned by the
  customer-facing workstream and consume the APIs specified in `contracts/openapi`.
- The audit dossier service and infrastructure as code. These are follow-on work in this same layer
  and are not part of this delivery.

## System architecture

```mermaid
flowchart TB
    subgraph external["External sources"]
        feeds["FDA / USDA FSIS / EU RASFF feeds"]
        catalogs["Distributor catalog snapshots"]
        shop["Shopify Admin API"]
        off["Open Food Facts"]
    end

    subgraph ingestion["Integrations workstream (out of scope here)"]
        ing["Ingestion and silent-diff services"]
        notify["Notification service"]
    end

    subgraph core["Core domain layer (this repository)"]
        res["resolution-service"]
        con["containment-service"]
        resc["order-rescue-service"]
    end

    subgraph ui["Customer-facing workstream (out of scope here)"]
        store["Storefront"]
        ops["Ops console"]
    end

    bus{{"RabbitMQ topic exchanges"}}

    feeds --> ing
    catalogs --> ing
    ing -- "ingestion.recall.raw.received.v1" --> bus
    bus --> res
    res -- "resolution.lot.resolved.v1" --> bus
    bus --> con
    con -- "containment.action.proposed.v1<br>containment.action.taken.v1" --> bus
    bus --> resc
    resc -- "rescue.order.proposed.v1" --> bus
    bus --> notify
    bus --> ops
    bus --> store
    con -.-> shop
    resc -.-> off
    store -- "confirm (REST)" --> resc
    ops -- "review and tune (REST)" --> con
    store -- "lot status (REST)" --> res
```

Plain arrows carry asynchronous events. Dotted arrows are outbound calls to external systems.
Arrows marked REST are synchronous calls defined in `contracts/openapi`.

## Incident lifecycle

```mermaid
sequenceDiagram
    autonumber
    participant Feed as Ingestion
    participant Res as resolution-service
    participant Con as containment-service
    participant Shop as Commerce platform
    participant Ops as Ops console
    participant Resc as order-rescue-service
    participant Cust as Customer

    Feed->>Res: ingestion.recall.raw.received.v1
    Res->>Res: extract GTIN and lot codes, score confidence
    Res->>Con: resolution.lot.resolved.v1

    alt confidence at or above threshold
        Con->>Shop: hold only the affected lots
        Con->>Resc: containment.action.taken.v1 (AUTO_HOLD)
    else confidence below threshold
        Con->>Ops: containment.action.proposed.v1
        Ops->>Con: confirm or reject (REST)
        Con->>Shop: hold only on confirm
        Con->>Resc: containment.action.taken.v1 (HUMAN_CONFIRMED or HUMAN_REJECTED)
    end

    Resc->>Resc: find in-flight orders holding an affected lot
    Resc->>Cust: rescue.order.proposed.v1 (same-price, allergen-safe options)
    Cust->>Resc: confirm one option (REST, consent token)
    Resc->>Resc: rescue.order.confirmed.v1, then apply the swap
```

Containment follows a strict state machine, so every incident ends in one defensible outcome:

```mermaid
stateDiagram-v2
    [*] --> PENDING_REVIEW: confidence below threshold
    [*] --> AUTO_HELD: confidence at or above threshold
    PENDING_REVIEW --> HUMAN_CONFIRMED: reviewer approves
    PENDING_REVIEW --> HUMAN_REJECTED: reviewer declines
    AUTO_HELD --> FAILED: commerce platform write failed
    HUMAN_CONFIRMED --> FAILED: commerce platform write failed
    AUTO_HELD --> [*]
    HUMAN_CONFIRMED --> [*]
    HUMAN_REJECTED --> [*]
    FAILED --> [*]
```

## Services

| Service | Port | Responsibility | API specification |
|---|---|---|---|
| `resolution-service` | 8081 | Resolves recall text to GTIN and lot codes, scores confidence with evidence, answers the Verified Safe Lot query | `contracts/openapi/resolution-api.v1.yaml` |
| `containment-service` | 8082 | Applies the auto-hold threshold, writes the inventory hold, serves the review queue and live threshold tuning | `contracts/openapi/containment-api.v1.yaml` |
| `order-rescue-service` | 8083 | Finds affected in-flight orders, proposes same-price allergen-safe substitutes, applies a swap only after customer consent | `contracts/openapi/order-rescue-api.v1.yaml` |

### resolution-service

Consumes `ingestion.recall.raw.received.v1` and `ingestion.catalog.sku.vanished.v1`. The ingestion
contract hands over verbatim feed text and leaves all parsing here, so the service extracts barcodes
(validated by
check digit, normalized to GTIN-14) from the product description, lot or batch codes from
`normalized.code_info`, and a severity class from the feed's free-form label, scores every catalog
product against the signal, and publishes `resolution.lot.resolved.v1` with a per-match evidence trail.
A resolution that recovers no lot codes is marked `SKU` scope so containment can treat it more
cautiously.

### containment-service

Consumes `resolution.lot.resolved.v1`. Confidence at or above the threshold results in an immediate hold of the
named lots; anything lower is published as `containment.action.proposed.v1` and waits in the review
queue. A reviewer may narrow the held lots but never widen them. Every outcome, including a
rejection, is published as `containment.action.taken.v1` with per-target results, so the record of
what was held and what stayed sellable is complete.

### order-rescue-service

Consumes `containment.action.taken.v1`. For each held target it scans in-flight orders for lines
carrying an affected lot and proposes options: substitutes at the identical price whose allergen
profile is verified against Open Food Facts, plus a refund and a cancel option. Nothing is swapped
until `rescue.order.confirmed.v1` is produced from an explicit customer confirmation carrying a valid
consent token.

## Shared libraries

| Package | Purpose |
|---|---|
| `libs/core/events` | Go binding of `contracts/events/*.json`: envelope, routing keys, payload types, validation |
| `libs/core/bus` | Publish and consume seam, with a RabbitMQ implementation (topic exchanges, per-queue dead-letter exchange, publisher confirms, manual acknowledgement, bounded retries) and an in-process implementation used by tests |
| `libs/core/matching` | Shared matching-confidence library: identifier normalization, lot code extraction, text similarity, weighted scoring with evidence |
| `libs/core/shopify` | The `InventoryClient` interface containment writes through, plus a lot-level fake store |
| `libs/core/offacts` | Open Food Facts provider interface (HTTP and static), allergen normalization, hazard allergen parsing |

`libs/core/matching` is shared with anti-evasion monitoring so that "confident" has exactly one definition
across the platform.

## Messaging

Events published by this layer use an envelope (`contracts/events/_envelope.v1.json`) carrying
`event_id`, `event_type`, `correlation_id` (the incident id) and `causation_id` (the event that caused
it), so an entire incident can be reconstructed from the bus alone. Inbound ingestion events are flat
rather than enveloped, per the schema the ingestion services own; `libs/core/events.InboundEnvelope`
adapts them at the boundary so handlers see one shape. Exchanges are organized one per bounded context
and routing keys follow `<context>.<subject>.<action>.<version>`.

```mermaid
flowchart LR
    ingx["ingestion.x"] --> rq["resolution.recall-raw"]
    rq --> res["resolution-service"]
    res --> resx["resolution.x"]
    resx --> cq["containment.lot-resolved"]
    cq --> con["containment-service"]
    con --> conx["containment.x"]
    conx --> rcq["rescue.containment-taken"]
    rcq --> resc["order-rescue-service"]
    resc --> rx["rescue.x"]
    rx --> rfq["rescue.confirmations"]
    rfq --> resc
    conx --> other["notification, audit, storefront and ops queues"]
    resx --> other
    rx --> other
```

The full registry of exchanges, queues, bindings and dead-letter routing is
`contracts/rabbitmq-topology.md`.

## Design rules

- **Lot scope is the product.** A `SKU` scope resolution faces a higher threshold
  (`sku_scope_threshold`, default 0.95) than a lot-level hold (`auto_hold_threshold`, default 0.85).
  Removing an entire product line should be harder than removing a lot.
- **Confidence must be explainable.** Every match carries weighted evidence (exact barcode, brand,
  title similarity, silent-diff signal), so a reviewer and an auditor read the same reasoning.
- **An unidentified lot is not a safe lot.** The lot status endpoint returns `UNKNOWN_LOT` rather
  than `SAFE` when it cannot place a unit against a live incident.
- **Consent is structural.** The confirm endpoint validates an HMAC consent token and publishes the
  confirmation event; the swap is applied by the event handler, so an API call and a replayed event
  follow the same single-writer path.
- **Missing allergen data is unsafe data.** A substitute with no enrichment record, or carrying any
  allergen the original did not, is refused. Refund and cancel are always offered.
- **At-least-once delivery is assumed.** Handlers deduplicate on `event_id` and incidents deduplicate
  on a deterministic `incident_id`, so a replayed notice cannot hold twice or propose twice.
- **Contact details stay on the notification path.** Customer email and phone travel in the rescue
  event for delivery, but are excluded from API responses and from anything intended for an audit
  record.

## Running locally

```bash
# each service in its own terminal
cd services/resolution-service   && go run ./cmd/resolution-service
cd services/containment-service  && go run ./cmd/containment-service
cd services/order-rescue-service && go run ./cmd/order-rescue-service
```

Without `RABBITMQ_URL`, each service runs on its own in-process bus, which is convenient when working
against a single service. With `RABBITMQ_URL=amqp://guest:guest@localhost:5672/` the services declare
their queues per the topology registry and exchange events for real.

Seed fixtures live in each service's `testdata/` directory. Every path and threshold is overridable
by environment variable, documented in the header comment of each `cmd/*/main.go`.

Example requests:

```bash
curl "localhost:8081/v1/lots/status?gtin=041196910537&lot_code=8H-2000"
curl "localhost:8082/v1/containment/actions?status=PENDING_REVIEW"
curl  localhost:8082/v1/containment/config
curl "localhost:8083/v1/rescues?order_id=ORD-1001"
```

## Testing

```bash
go test soteria/libs/core/... soteria/tests/e2e/...
```

`tests/e2e` wires all three services onto one bus and asserts the entire chain, with no broker
required. It covers the surgical hold (65 units of the recalled lots held while 60 units of the clean
lot stay sellable), the review queue path including reviewer narrowing and rejection, live threshold
tuning, redelivery idempotence, refusal of a forged consent token, and reporting of a commerce
platform failure. `libs/core/matching` carries unit tests for barcode validation, lot extraction and
scoring bands.

## Repository layout

```
contracts/
  events/            JSON Schema for every event on the bus
  openapi/           REST specifications consumed by the storefront and ops console
  rabbitmq-topology.md
libs/core/
  events/ bus/ matching/ shopify/ offacts/
services/
  resolution-service/    cmd, api, catalog, resolver, testdata
  containment-service/   cmd, api, containment, testdata
  order-rescue-service/  cmd, api, orders, rescue, testdata
tests/
  e2e/               cross-service flow tests
```

Each service is an independent Go module with its own dependencies and can be built, tested and
deployed on its own. `go.work` ties them together for local development.
