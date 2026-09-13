# ingestion-rasff

Ingests EU RASFF (Rapid Alert System for Food and Feed) notifications and publishes `recall.raw.received.v1` to `ingestion.x`. RASFF is the early-warning window: contaminated multinational ingredient batches often surface here days before a US notice.

## Modes

| `RASFF_MODE` | Status | How |
|---|---|---|
| `file` (default) | **working** | Reads every `*.json` / `*.csv` in `RASFF_FILE_DIR` (default `fixtures/`) on each poll. Drop the RASFF Window search export into the folder; old files can stay (dedup on `reference`). |
| `http` | **working** | Polls the portal's public RSS feed, `GET /rasff-window/backend/public/consumer/rss/all/en/` (trailing slash required). Live and unattended, but carries fewer fields than an export: reference, subject, notifying country, date, and hazard/product/origin split out of the subject. |

Accepted columns (any casing/spacing; portal export headers and common variants are aliased): `reference`, `date`, `notifying country`, `classification`, `type`, `subject`, `product category`, `product`, `hazards`, `risk decision`, `distribution status`, `origin`, `distribution`, `url`. CSV may be comma or semicolon separated, with or without a BOM. Dates: `YYYY-MM-DD`, `DD/MM/YYYY`, `DD-MM-YYYY`, `2 Jan 2006`.

```sh
make dry-run                         # 4 sample notifications from fixtures/
RASFF_FILE_DIR=/path/to/exports make run
```

## Which mode to use

Both, for different jobs. `http` is the unattended live signal; `file` is the richer record.

There is still no documented RASFF API. The search endpoints the portal's own table uses are unreachable without a session, but the SPA also links a **public RSS feed**, and that is what `http` mode polls:

```
GET https://webgate.ec.europa.eu/rasff-window/backend/public/consumer/rss/{market}/{lang}/
    market: "all" (every single-market country) or a numeric organization id
    lang:   "en"
```

The trailing slash is required; without it the gateway answers a 404 HTML page. The path was read out of the SPA's lazy-loaded chunk, which builds it as `./backend/public/consumer/rss/${market}/${lang}/`. Verified against the live feed on 2026-09-12: 70 notifications.

The feed gives reference, subject, notifying country and date; hazard, product and origin are split out of the subject, which follows the portal's own `<hazard> in <product> from <origin>` convention (a subject that does not fit is kept verbatim and simply not split). Fields an export carries and the feed does not — classification, risk decision, distribution, product category — stay empty, which is why `file` mode remains the fuller source for an incident someone is actually working.

It is an undocumented endpoint, so treat it as best-effort and expect it to change without notice.
