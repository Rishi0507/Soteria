# ingestion-usda

Polls the USDA FSIS recall API (meat, poultry, egg products — the highest-fatality recall class, which FDA feeds never carry) and publishes `recall.raw.received.v1` to `ingestion.x`.

| Source | `source` | Endpoint | Dedup key |
|---|---|---|---|
| FSIS recalls & public health alerts | `usda_fsis` | `GET https://www.fsis.usda.gov/fsis/api/recall/v/1` | `field_recall_number` (e.g. `031-2024`, `PHA-09052026-01`) |

The API has no `since` parameter and returns the current year's full list on every call, so each poll is diffed against the dedup store. HTML in `field_summary` / `field_product_items` is flattened to line-separated text in `normalized`; the raw HTML is preserved in `raw`.

## Network caveat

`fsis.usda.gov` sits behind Akamai and refuses requests that do not look like a browser XHR. The connector sends the header set the edge accepts (see `browserUA` / `requestHeaders` in `internal/fsis/fsis.go`); **verified live from a non-US network on 2026-09-13: 14 recalls in a 60-day window, all fields mapped**. That header set is a fingerprint and will rot; when the edge rules change, override with `FSIS_USER_AGENT`, or point `FSIS_BASE_URL` at a US-egress proxy / fixture server. The live payload sends some fields as arrays and others as strings per record — the mapper accepts both.

```sh
make dry-run                                                    # against the live API
FSIS_BASE_URL=http://127.0.0.1:8765/recalls.json make dry-run   # against a local fixture server
make once && make run
```
