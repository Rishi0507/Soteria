# recall-extractor

The agentic step of the pipeline. Every raw recall notice (openFDA report, FDA press release, USDA FSIS notice, EU RASFF notification) is read by an LLM that returns **which products, which identifiers, why, and where** as JSON — then a deterministic guardrail validates every claim against the notice text before anything is published.

```
ingestion.recall.raw.received.*  ──▶  recall-extractor  ──▶  ingestion.recall.extracted.v1
     messy agency text                  LLM + guardrail          products[] {name, brand, sizes, upcs, lot_codes, best_by}
                                                                 hazard {type, agent, allergens}
                                                                 distribution {states, nationwide, countries, retailers}
                                                                 confidence, evidence[], corrections[]
```

## Why an LLM, and why a guardrail

Recall notices are prose written by humans at four agencies. The regex approach finds digit runs; it cannot tell a UPC from an item number, a lot code from a date, "Kroger" (the brand on the box) from "Midwest Poultry Servc" (the packer), or that bonito/sardine/mackerel mean the allergen is *fish*. The model does all of that.

But a model can also invent. So the guardrail (`internal/extract/schema.go: Validate`) enforces:

| Rule | Effect |
|---|---|
| every UPC must appear in the notice text | invented codes dropped |
| UPCs must be 12–14 digits with a valid check digit → normalised GTIN-14 | dates and lot numbers cannot masquerade as products |
| a printed code with a bad check digit is **kept verbatim and flagged** | agencies mistype UPCs (the Sun Noodle case code really is wrong in the FDA report); the typo is still evidence |
| every lot code must appear verbatim | no invented lots |
| valid GTINs the model missed are added by regex | output is never worse than the deterministic baseline |
| hazard enum, canonical allergen names, 2-letter state codes, `nationwide` detection | consumers get stable vocab |
| confidence penalised per dropped identifier; capped at 0.3 with no products | downstream thresholds mean something |
| everything it changed is written to `corrections[]` | auditable |

## Evaluation

`eval/` holds **16 real notices** with hand-labelled expectations — including a 28-product egg recall with UPCs printed as `0 11110-60902 1`, a notice whose code_info is a bare list of 16 sibling UPCs with two malformed ones, 16 interleaved batch codes, a 10-digit "UPC" that is not a GTIN, two press releases with no identifiers, an FSIS notice and a RASFF notification.

```
TOTAL (16 cases, model groq/openai/gpt-oss-120b):
  UPC P=1.000 R=1.000 | lots P=1.000 R=1.000 | hazard 1.000 | allergens 1.000 | distribution 1.000 | brand 1.000
```

Model answers are recorded in `eval/recordings.json`, so `go test ./...` (and CI) replays the suite with no API key. To re-run live after a prompt change:

```sh
EVAL_RECORD=1 GROQ_API_KEY=gsk_... go test ./eval -v -count=1
```

The suite fails CI if UPC precision/recall drop below 0.95, lot precision/recall below 0.80, or hazard/allergen/brand accuracy below 0.9. Two prompt fixes came out of the first run (FSIS establishment numbers are lot codes; brand is the package label, not the recalling firm) — that loop is the point of the suite.

## Run

```sh
make test                                   # unit + contract + eval (recorded)
make text T="FIRM: ... PRODUCT: ... UPC 194346207961 ..."   # ad-hoc extraction
cd ../ingestion-fda && go run ./cmd --once --dry-run --backfill-days 14 > /tmp/raw.jsonl
cd ../recall-extractor && make replay F=/tmp/raw.jsonl       # extract real notices, print JSON
make run                                    # consume from RabbitMQ; POST /v1/extract, /healthz, /metrics on :8086
```

Config: `GROQ_API_KEY` (console.groq.com, free), `GROQ_MODEL` (default `openai/gpt-oss-120b`), `RABBITMQ_URL`, `CACHE_PATH`, `PORT`. Every model answer is cached in SQLite keyed by (model, text), so redeliveries, restarts and evals never repeat a call. ~10 s per notice on the free tier; a few notices a day in production.

## For Person 3 (resolution-service)

Bind `resolution.recall-raw` (or a new queue) to `ingestion.recall.extracted.v1` and build `matching.Signals` from the payload instead of re-parsing raw text:

- `products[].upcs` → `Signals.UPCs` (already GTIN-14, check-digit valid)
- `products[].lot_codes` → `Signals.LotCodes` (verbatim)
- `brand` / `products[].brand` → `Signals.Brands` (package brand, e.g. "Kroger", not the packer)
- `hazard.type` / `hazard.allergens` → hazard classification without the `strings.Contains(l, "listeria")` heuristics
- `distribution.states` / `nationwide` → scope the hold to affected locations
- `confidence` → feed into the auto-hold threshold alongside the match score
- `text` → what the model read; `corrections` → what the guardrail changed

The raw event keeps flowing too, so nothing breaks until you switch.

## Next upgrade

FDA press releases (the *earliest* signal) arrive via RSS with a 300-character description, so their UPCs/lots are not in the text yet. `ingestion-fda` should fetch the linked press-release page and carry its body in `raw` — the extractor will then pull identifiers out of press releases days before the enforcement report exists.
