# Sotería — Product Requirements Document
### Predictive Recall Intelligence & Containment Layer

**Version:** 0.2 (3-person build plan)
**Owner:** Product/Eng Lead
**Status:** Ready for team assignment

---

## 1. Problem & Thesis

Retailers today react to recalls *after* the FDA press-releases them, and when they react, they wipe out the entire SKU — burning inventory that was never actually contaminated. Sotería flips both failures:

1. **Earlier signal** — cross-reference multiple agency feeds (FDA, USDA FSIS, EU RASFF) plus silent catalog-diffing, so contamination is known days before a formal US notice, or even when no formal notice is ever filed.
2. **Surgical containment** — resolve a recall down to the exact lot number, not the whole SKU, so unaffected inventory stays sellable.
3. **Closed-loop customer handling** — rescue in-flight orders with a confirmed substitute instead of a blind cancellation, and notify affected buyers directly.
4. **Anti-evasion** — catch recalled lots being laundered back into the market via resellers/liquidators.
5. **Proof as an asset** — every action produces a timestamped audit trail usable for insurance/regulatory defense.

## 2. Goals / Non-Goals

**Goals (v1)**
- Ingest and normalize FDA + USDA FSIS + EU RASFF recall feeds into one canonical event model.
- Detect silent recalls via catalog diffing against at least one real distributor/manufacturer catalog source.
- Resolve recall text to UPC/GTIN + lot number with a confidence score.
- Auto-hold/zero the exact lot in Shopify above a confidence threshold; queue for human confirm below it.
- Order rescue flow: detect in-flight orders containing an affected lot, propose an allergen-safe same-price substitute, require explicit customer confirmation.
- Direct customer notification (email/SMS) for anyone who purchased an affected lot.
- Anti-evasion monitoring against at least one liquidation/resale marketplace class.
- Auto-generate a cryptographically timestamped audit dossier per incident.

**Non-Goals (v1)**
- Multi-country regulatory coverage beyond US (FDA/USDA) + EU (RASFF).
- POS integrations beyond Shopify (design for pluggability, but only ship Shopify connector).
- Full insurance-premium integration (only produce the artifact insurers would want).

## 3. Success Metrics
- Time from external notice availability → internal `RecallDetected` event: **< 15 min** (target).
- % of recalls resolved to lot-level (not just SKU-level): **> 80%**.
- False-positive auto-hold rate requiring manual reversal: **< 5%**.
- Order rescue acceptance rate (customer confirms substitute vs. cancels): tracked, no hard target v1.
- Audit dossier generation time post-containment action: **< 5 min**.

## 4. High-Level System Architecture

Three people, three clean verticals, connected only through **RabbitMQ events** and **versioned REST/OpenAPI contracts**. This is what keeps three people from ever needing to touch the same file: each person owns a set of services end-to-end (their own repo folders, their own DB schemas, their own CI job), and the only shared surface is the `/contracts` directory, which is append-only/versioned.

```
                     ┌────────────────────────────────────────────────┐
                     │               EXTERNAL WORLD                   │
                     │  FDA / USDA FSIS / EU RASFF / Open Food Facts   │
                     │  Shopify / Liquidation Marketplaces / Email-SMS │
                     └───────────────┬──────────────────────────────┘
                                     │
        ┌────────────────────────────▼───────────────────────────────┐
        │                 PERSON 1 — API & EXTERNAL LAYER              │
        │  ingestion (FDA/USDA/RASFF) · silent-diff · anti-evasion      │
        │  Shopify client · notification delivery · n8n workflows       │
        └────────────────────────────┬───────────────────────────────┘
                                     │ publishes: recall.raw.received,
                                     │ catalog.sku.vanished, evasion.flagged
                     ┌───────────────▼───────────────────────┐
                     │   RabbitMQ (topology owned by Person 3) │
                     └──┬───────────────────────┬────────────┘
                        │                        │
             ┌──────────▼──────────┐   ┌─────────▼─────────────┐
             │  PERSON 3 — BACKEND  │   │  PERSON 2 — STOREFRONT  │
             │  resolution-service  │   │  React/TS storefront    │
             │  containment-service │   │  Open Food Facts        │
             │  audit-proof-service │◄──┤  order rescue UI        │
             │  RabbitMQ + infra     │   │  ops console (review Q) │
             │  contracts + CI/CD    │   │  "Verified Safe Lot" UI │
             └──────────────────────┘   └────────────────────────┘
```

## 5. Tech Stack (fixed, per requirements)
| Concern | Stack |
|---|---|
| Core microservices | **Golang** (performance, concurrency for feed polling & event processing) |
| Data/ML-heavy work (parsing, fuzzy matching, catalog diffing, packaging-signature matching) | **Python** |
| Storefront + internal ops UI | **React + TypeScript** |
| Async messaging / event bus | **RabbitMQ** |
| Cross-system automation & notification orchestration | **n8n** |
| Product enrichment | **Open Food Facts API** |
| Commerce platform | **Shopify Admin API** |

## 6. Contract-First Development (how 3 people avoid merge conflicts)

1. **Contracts before code.** `/contracts` — event schemas, OpenAPI specs, RabbitMQ topology — is written and signed off by all 3 people in Sprint 0, before any service implementation starts.
2. **One person = one set of folders.** No two people ever edit inside the same service directory. Person 3 has final say on `/contracts` structure since they own the message bus and the two central domain services, but any change is proposed as a PR the other two must approve.
3. **Versioned, never-breaking events.** A schema change ships as `EventName.v2` alongside `.v1` until every consumer has migrated.
4. **Mock-first for Person 2.** Storefront and ops console build against a mock server generated from the OpenAPI specs, so frontend work is never blocked waiting on Person 1's or Person 3's real services to be running.
5. **Contract tests in CI.** Every producer's CI validates its published events against `/contracts`; every consumer's CI validates its parsing against the same schema. Person 3 owns the shared test harness since they own the schemas being tested against.
6. **Weekly 20-minute sync.** All 3 walk the event-flow diagram (Section 4) and confirm nothing has silently diverged.

### 6.1 Repo layout
```
/soteria
  /contracts/                        <- shared source of truth (Person 3 owns, both others review)
    /events/*.json                       e.g. RecallDetected.v1.json
    /openapi/*.yaml                      e.g. containment-api.v1.yaml
    /rabbitmq-topology.md                exchange/queue/routing-key registry
  /services/
    /ingestion-fda/                  Person 1 (Go)
    /ingestion-usda/                 Person 1 (Go)
    /ingestion-rasff/                Person 1 (Go)
    /ingestion-silent-diff/          Person 1 (Python)
    /anti-evasion-service/           Person 1 (Python)
    /notification-service/           Person 1 (Go)
    /resolution-service/             Person 3 (Go)
    /containment-service/            Person 3 (Go)
    /audit-proof-service/            Person 3 (Go)
    /order-rescue-service/           Person 3, consumed by Person 2's UI (Go)
  /apps/
    /storefront/                     Person 2 (React/TS)
    /ops-console/                    Person 2 (React/TS)
  /automation/
    /n8n-workflows/                  Person 1 (exported JSON workflows)
  /infra/                            Person 3 (docker, k8s, CI/CD, RabbitMQ infra-as-code)
  /docs/
    prd.md (this file)
```

### 6.2 Canonical events
| Event | Producer | Consumers |
|---|---|---|
| `recall.raw.received.v1` | Ingestion (P1) | Resolution Service (P3) |
| `catalog.sku.vanished.v1` | Silent-diff (P1) | Resolution Service (P3) |
| `lot.resolved.v1` | Resolution Service (P3) | Containment (P3), Storefront (P2) |
| `containment.action.proposed.v1` | Containment (P3) | Ops Console (P2), Audit (P3) |
| `containment.action.taken.v1` | Containment (P3) | Storefront (P2), Audit (P3), Notification (P1) |
| `order.rescue.proposed.v1` | Order Rescue (P3) | Notification (P1), Storefront UI (P2), Audit (P3) |
| `order.rescue.confirmed.v1` | Storefront (P2) | Order Rescue Service (P3) |
| `evasion.flagged.v1` | Anti-Evasion (P1) | Audit (P3), Notification (P1) |
| `audit.dossier.generated.v1` | Audit (P3) | Ops Console (P2) |

### 6.3 RabbitMQ topology conventions (owned by Person 3, documented in `/contracts/rabbitmq-topology.md`)
- One topic exchange per bounded context: `ingestion.x`, `resolution.x`, `containment.x`, `evasion.x`, `audit.x`, `notification.x`.
- Routing key pattern: `<domain>.<entity>.<action>.<version>`.
- Each service owns its own queue(s) bound to the exchanges it needs; nobody publishes directly into another service's queue.
- Dead-letter exchange per queue, needed for the audit trail's completeness/replay guarantees.

### 6.4 API contracts
- All REST APIs are OpenAPI 3.0 specs in `/contracts/openapi`, written **before** implementation.
- Person 2 codegens a TypeScript client from these specs — eliminates most frontend/backend integration drift.
- Shopify webhook payloads and Open Food Facts response shapes get their own schema files so parsing logic is independently testable from business logic.

---

## 7. Work Breakdown — 3 People, Full Scope

### Person 1 — API, External Integrations & Automation Lead
**Owns:** `/services/ingestion-fda`, `/services/ingestion-usda`, `/services/ingestion-rasff`, `/services/ingestion-silent-diff`, `/services/anti-evasion-service`, `/services/notification-service`, `/automation/n8n-workflows`, Shopify API client library.
**Stack:** Go (all connector/notification services), Python (silent-diff catalog matching, anti-evasion packaging/date-code signature matching), n8n (scheduling, retries, cross-system automation glue).

**A. Detection feeds**
- Build normalized connectors for FDA openFDA API, USDA FSIS API, EU RASFF portal/API → publish `recall.raw.received.v1`.
- Handle polling cadence, dedup, rate limits, retries, feed-outage alerting.

**B. Silent recall detection** *(absorbed from the original "additional" scope)*
- Scheduled diff of manufacturer/distributor catalog snapshots (Python: pandas/fuzzy matching) to flag SKUs quietly vanishing → publish `catalog.sku.vanished.v1`.

**C. Anti-evasion** *(absorbed from the original "additional" scope)*
- Monitor liquidation marketplaces/resale platforms for packaging/date-code signatures matching a recalled lot reappearing post-recall-date → publish `evasion.flagged.v1`.
- Shares a matching-confidence library with Resolution Service (Person 3) so text/image matching logic isn't duplicated — defined once, imported by both.

**D. Shopify client library**
- Shared, versioned Go client wrapping auth/rate-limits, consumed by Person 3's Containment Service — built here since it's an external API integration, but lives as a shared package so Person 3 doesn't reimplement it.

**E. Notifications & orchestration** *(absorbed from the original "additional" scope)*
- Delivery-guaranteed Notification Service (Go) triggered by `containment.action.taken.v1`, `order.rescue.proposed.v1`, `evasion.flagged.v1` — sends customer email/SMS and retailer alerts. Delivery guarantees matter here because "customer was notified" needs to be provable in the audit dossier.
- n8n workflows for lighter-weight automation: scheduled health-check pings across all ingestion feeds, Slack/email alerts to the retailer ops team, and any glue that doesn't need a dedicated microservice.

**Deliverables:** all ingestion + silent-diff + anti-evasion services live and publishing to RabbitMQ; Shopify client library; Notification Service; n8n workflow exports.

---

### Person 2 — Storefront, Open Food Facts & Ops Console Lead
**Owns:** `/apps/storefront`, `/apps/ops-console`, Open Food Facts integration layer.
**Stack:** React + TypeScript (codegen'd clients from `/contracts/openapi`), thin Go BFF layer only if storefront-specific data aggregation requires it.

**A. Storefront**
- "Verified Safe Lot" badge UI, consuming a read API built off `lot.resolved.v1`.
- Order rescue UX: surfaces the proposed substitute, requires explicit customer click-to-confirm (never auto-swap, especially across allergen lines), calls the endpoint that emits `order.rescue.confirmed.v1`.
- Customer-facing recall/notification banners.
- Open Food Facts integration for product enrichment (ingredients, allergens) — used both for storefront display and to validate substitute-matching allergen-safety (shared schema, consumed by Person 3's Order Rescue Service too).

**B. Ops console** *(absorbed from the original "additional" scope)*
- Internal review queue for below-threshold containment actions requiring human confirm, reading `containment.action.proposed.v1`.
- Dashboard for browsing the generated audit dossier archive (`audit.dossier.generated.v1`).
- Threshold-tuning UI so the confidence cutoff for auto-hold vs. human-confirm can be adjusted without a redeploy (config surface defined by Person 3).

**Deliverables:** TypeScript client generated from `/contracts/openapi`; storefront badge/rescue components (Storybook-able against mocks before backend is ready); ops console review queue and dossier dashboard.

---

### Person 3 — Backend Architecture, Microservices, Infra & Proof Lead
**Owns:** `/contracts` (final say), `/services/resolution-service`, `/services/containment-service`, `/services/order-rescue-service`, `/services/audit-proof-service`, RabbitMQ topology, `/infra`.
**Stack:** Go for all core services; owns infra-as-code and CI/CD.

**A. Core domain services**
- Design and own canonical event schemas + RabbitMQ exchange/queue topology (Section 6.2–6.3).
- Resolution Service: text → UPC/GTIN + lot number matching, confidence scoring (shares matching-confidence library with Person 1's anti-evasion work).
- Containment Service: threshold logic for auto-hold vs. human-confirm, Shopify writes via Person 1's client library, publishes containment events.
- Order Rescue Service: detects in-flight orders containing an affected lot, proposes an allergen-safe same-price substitute (cross-checks Open Food Facts allergen data via Person 2's schema), waits for explicit customer confirmation before any swap.

**B. Audit/Proof** *(absorbed from the original "additional" scope)*
- Audit/Proof Service: consumes containment, rescue, and evasion events; produces a cryptographically timestamped (RFC 3161 or hash-anchored) dossier per incident; exports as PDF for insurers/regulators.

**C. Platform/DevOps/Security** *(absorbed from the original "additional" scope)*
- Containerize every service, own per-service CI/CD pipelines so all 3 people can ship independently.
- RabbitMQ cluster setup and infra-as-code, secrets management, observability (logging/metrics/tracing) — critical since the audit trail's credibility depends on complete, tamper-evident logs.
- Security review: PII handling for customer notification data, access control on the ops console, data retention policy for dossiers.
- Owns the shared contract-test harness: every producer/consumer's CI validates against `/contracts` schemas, catching drift before merge.

**Deliverables:** finalized `/contracts` directory the other two build against; running resolution/containment/order-rescue/audit services; CI/CD templates any service folder can drop into; infra-as-code repo; contract-test harness.

---

## 8. Integration & Merge Strategy

1. **Contracts first, code second.** Sprint 0 is entirely `/contracts`, signed off by all 3 before any service code is written.
2. **Independent folders, independent CI.** Each service has its own `go.mod`/`package.json`, DB schema/migrations, and CI job — nothing to fight over except `/contracts` and two explicitly shared libraries (Shopify client, matching-confidence library).
3. **Mock-first unblocking.** Person 2 builds storefront + ops console against OpenAPI-generated mocks, never blocked on Person 1/3's real services being up.
4. **Versioned events, no silent breaks.** New schema needs → ship `.v2` alongside `.v1` until consumers migrate.
5. **Contract tests in CI** catch schema drift before merge, not after deploy.
6. **Weekly 20-minute integration sync** across all 3, walking the event-flow diagram in Section 4.

## 9. Suggested Milestones

| Phase | Scope | Primary owner(s) |
|---|---|---|
| M0 — Contracts | Event schemas, OpenAPI specs, RabbitMQ topology doc | Person 3 (lead), all review |
| M1 — Detection MVP | FDA+USDA+RASFF ingestion live, silent-diff prototype | Person 1 |
| M2 — Resolution + Containment MVP | Lot resolution, Shopify auto-hold w/ threshold, ops console review queue | Person 3, Person 2 |
| M3 — Customer-Facing | Storefront badge, order rescue flow, notifications live | Person 2, Person 1 |
| M4 — Anti-Evasion + Audit | Evasion monitoring live, audit dossier auto-generation | Person 1, Person 3 |
| M5 — Hardening | Infra, security review, contract-test coverage, observability | Person 3 (all support) |
