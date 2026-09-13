# ingestion-fda

Polls two FDA feeds and publishes `recall.raw.received.v1` to `ingestion.x`.

| Source | `source` | Endpoint | Dedup key | Notes |
|---|---|---|---|---|
| openFDA food enforcement | `fda_enforcement` | `GET https://api.fda.gov/food/enforcement.json?search=report_date:[A+TO+B]&sort=report_date:desc&limit=100&skip=N` | `recall_number` | 40 req/min keyless, 240 with `OPENFDA_API_KEY`. A 404 `NOT_FOUND` means "no matches", not an outage. Reports lag the real recall by days–weeks. |
| FDA recalls press RSS | `fda_press` | `https://www.fda.gov/about-fda/contact-fda/stay-informed/rss-feeds/recalls/rss.xml` | RSS `guid` | Press releases — typically days earlier than the enforcement report. Unstructured (title/summary/link). |

Both sources run in one binary on the same cadence (`POLL_INTERVAL`, default 5m). On the first run the enforcement query looks back `BACKFILL_DAYS`; afterwards it re-requests a 48h overlap before the last success so late-indexed reports are not missed (dedup absorbs the overlap).

```sh
make dry-run ARGS="--backfill-days 14"   # print events, publish nothing
make once    ARGS="--backfill-days 14"   # publish, exit; run twice → second publishes 0
make run                                 # poller + /healthz on :8080
make docker                              # build soteria/ingestion-fda from the repo root
```

Config: `.env.example` at the repo root. Tests: `make test` (mapper tests on recorded responses in `internal/*/testdata`, pagination, contract test).
