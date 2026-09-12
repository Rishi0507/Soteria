# Hand-off: ingestion services → resolution-service (Person 1 → Person 3)

**Status (2026-09-12):** three producers live, one canonical event, contract drafted for your sign-off.

## What to consume

- Exchange: `ingestion.x` (topic, durable) — declared idempotently by the producers; declare it in `/infra` too, same args.
- Routing key: `ingestion.recall.raw.received.v1`
- Schema: [`contracts/events/recall.raw.received.v1.json`](../contracts/events/recall.raw.received.v1.json) — **please review/merge**; the producers' CI validates every fixture-derived event against it.
- Message properties: `content_type=application/json`, `delivery_mode=2`, `message_id=<event_id>`, `type=recall.raw.received`, `app_id=<producer>`, `timestamp=<occurred_at>`.

Suggested binding: queue `resolution.recall-raw`, pattern `ingestion.recall.raw.received.*`, DLX `resolution.dlx`.

## Semantics you must handle

- **At-least-once.** Producers mark a notice seen only after the broker confirms. Dedupe on `event_id`, or on `(source, source_id)` if you want idempotency across producer restarts.
- **`raw` is untouched upstream data**; its shape differs per `source`:
  - `fda_enforcement`: openFDA enforcement report (all-string fields, dates `YYYYMMDD`, `code_info` holds lot text, `product_description` often contains UPCs).
  - `fda_press`: RSS item `{title, link, description, pubDate, creator, guid}` — unstructured; earlier than enforcement reports.
  - `usda_fsis`: FSIS record (`field_*`, HTML in `field_summary` / `field_product_items`; lot/establishment codes inside the product text).
  - `eu_rasff`: portal export row (`reference`, `subject`, `hazards`, `origin`, `distribution`, …).
- **`normalized` is a light projection only** — `code_info`, `classification`, `distribution` are verbatim strings. Lot parsing, UPC resolution and severity mapping are yours.
- `published_at` may be `null` (feed had no parseable date); `occurred_at` is always set.

## Sample messages

Run any service with `--dry-run` to get real payloads:

```sh
cd services/ingestion-fda   && go run ./cmd --once --dry-run --backfill-days 14
cd services/ingestion-rasff && go run ./cmd --once --dry-run
```

## Open items on my side

- `usda_fsis` field names need one verification from a US network (the site 403s from here).
- `eu_rasff` runs on exported files; a portal-backend adapter is documented but unimplemented.
