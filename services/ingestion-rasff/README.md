# ingestion-rasff

Ingests EU RASFF (Rapid Alert System for Food and Feed) notifications and publishes `recall.raw.received.v1` to `ingestion.x`. RASFF is the early-warning window: contaminated multinational ingredient batches often surface here days before a US notice.

## Modes

| `RASFF_MODE` | Status | How |
|---|---|---|
| `file` (default) | **working** | Reads every `*.json` / `*.csv` in `RASFF_FILE_DIR` (default `fixtures/`) on each poll. Drop the RASFF Window search export into the folder; old files can stay (dedup on `reference`). |
| `http` | **not implemented — refuses to start** | Reserved for a verified portal-backend adapter (see below). |

Accepted columns (any casing/spacing; portal export headers and common variants are aliased): `reference`, `date`, `notifying country`, `classification`, `type`, `subject`, `product category`, `product`, `hazards`, `risk decision`, `distribution status`, `origin`, `distribution`, `url`. CSV may be comma or semicolon separated, with or without a BOM. Dates: `YYYY-MM-DD`, `DD/MM/YYYY`, `DD-MM-YYYY`, `2 Jan 2006`.

```sh
make dry-run                         # 4 sample notifications from fixtures/
RASFF_FILE_DIR=/path/to/exports make run
```

## Why no API adapter yet

There is no official public RASFF API. The RASFF Window portal (`https://webgate.ec.europa.eu/rasff-window/screen/search`) is an Angular SPA over an internal backend at `/rasff-window/backend`. Probing on 2026-09-12 found the client calls `/consumer/search` and `/notification/search/consolidated` (plus `/notification/search/export`), but the full path, request body and pagination live in a lazy-loaded bundle; the obvious candidates (`backend/public/consumer/search`, `backend/consumer/search`) return 404.

To implement `http` mode: open the portal with browser devtools, run a search, copy the request (URL, method, JSON body, headers) into `internal/rasff/http`, and reuse `rasff.ParseJSON` / `rasff.ToItem` for the response. Treat it as best-effort: it is undocumented and may change without notice, which is exactly why `file` mode exists.
