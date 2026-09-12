# RabbitMQ topology registry

Owned by Person 3. Producers append their exchanges/routing keys here via PR; consumers own their queues and bindings.

Conventions (PRD §6.3): one durable **topic** exchange per bounded context; routing key `<domain>.<entity>.<action>.<version>`; nobody publishes into another service's queue; every consumer queue has a dead-letter exchange.

## Exchanges

| Exchange | Type | Durable | Declared by | Notes |
|---|---|---|---|---|
| `ingestion.x` | topic | yes | ingestion-* services (idempotent `ExchangeDeclare`) and infra | Recall feed events from Person 1's connectors |

## Routing keys on `ingestion.x`

| Routing key | Schema | Producer(s) | Message properties |
|---|---|---|---|
| `ingestion.recall.raw.received.v1` | [`events/recall.raw.received.v1.json`](events/recall.raw.received.v1.json) | `ingestion-fda`, `ingestion-usda`, `ingestion-rasff` | `content_type=application/json`, `delivery_mode=persistent`, `message_id=<event_id>`, `type=recall.raw.received`, `app_id=<producer>`, `timestamp=<occurred_at>` |

Delivery semantics: **at-least-once**. Producers mark a notice as seen only after the broker confirms the publish, so a crash between publish and mark can re-emit the same `source_id` with a new `event_id`. Consumers should dedupe on (`source`, `source_id`) or on `event_id`.

Suggested consumer binding (resolution-service, Person 3): queue `resolution.recall-raw` bound to `ingestion.x` with pattern `ingestion.recall.raw.received.*`, DLX `resolution.dlx`.
