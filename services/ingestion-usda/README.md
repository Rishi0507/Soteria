# ingestion-usda

Polls the USDA FSIS recall API (meat, poultry, egg products — the highest-fatality recall class, which FDA feeds never carry) and publishes `recall.raw.received.v1` to `ingestion.x`.

| Source | `source` | Endpoint | Dedup key |
|---|---|---|---|
| FSIS recalls & public health alerts | `usda_fsis` | `GET https://www.fsis.usda.gov/fsis/api/recall/v/1` | `field_recall_number` (e.g. `031-2024`, `PHA-09052026-01`) |

The API has no `since` parameter and returns the current year's full list on every call, so each poll is diffed against the dedup store. HTML in `field_summary` / `field_product_items` is flattened to line-separated text in `normalized`; the raw HTML is preserved in `raw`.

## Network caveat

`fsis.usda.gov` sits behind Akamai and returns **403 to some non-US networks** (observed from India on 2026-09-12; the site's HTML pages and RSS are blocked too, not just the API). Options:

- Run the service from US egress (the intended deployment), or
- Set `HTTPS_PROXY` (Go's HTTP client honors it), or
- Point `FSIS_BASE_URL` at a proxy/fixture server for local development.

The field mapping is built from the documented v1 payload and a fixture (`internal/fsis/testdata/recalls.json`). **Action for whoever first runs this from a US network:** save one live response over the fixture and re-run `make test` — the mapper tests will flag any renamed field.

```sh
make dry-run                                                    # against the live API (needs US egress)
FSIS_BASE_URL=http://127.0.0.1:8765/recalls.json make dry-run   # against a local fixture server
make once && make run
```
