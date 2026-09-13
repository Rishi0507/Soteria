# seedshop

Prepares a Shopify store so a recall can be contained **surgically**, and checks that it stays
prepared.

Three things must be true, and each one fails differently:

| Requirement | If it's missing |
|---|---|
| A **Quarantine** location | A hold has nowhere to move stock to. Containment cannot run at all. |
| A valid **GTIN in the variant's Barcode** | No recall can ever match the product. It is invisible to the system. |
| A **`soteria.lots`** metafield on the variant | Shopify has no notion of lots, so a recall can only take the whole SKU: every unit pulled, which is the outcome Sotería exists to avoid. |

## Use

```sh
cd tools/seedshop

# 1. build a seed from live openFDA recalls (no credentials needed)
go run . generate -count 3 -days 45

# 2. apply it (idempotent, safe to re-run)
export SHOPIFY_SHOP=your-store.myshopify.com
export SHOPIFY_ACCESS_TOKEN=shpat_...
go run . apply -dry-run      # see what would change
go run . apply

# 3. readiness report, read-only
go run . verify
```

Token scopes: `read_products write_products read_inventory write_inventory read_locations`.
(The services additionally need `read_orders write_orders read_customers` at runtime.)

## Why generate from live recalls

The demo catalog is not invented. Each product is a real item, with the real barcode printed on its
pack, that a real agency recalled in the last few weeks — and the lot code the store holds is the lot
the notice actually names. A seeded store therefore matches a *genuine* FDA notice, rather than one
written to fit the fixtures.

The only invented part is the clean stock beside the recalled lot (`CLEAN-A`, `CLEAN-B`), and that is
precisely what is being demonstrated: a hold that takes 40 units and leaves 60 selling.

Each product records its `provenance`, so nobody has to wonder later which entries were real.

## Notes

- **Barcodes are check-digit validated.** A plausible-looking made-up number is rejected, because it
  would seed a product no recall could match. Barcodes are written in the shortest standard width
  (usually the 12-digit UPC-A as printed on the pack).
- **The lot ledger must equal stock at the selling location.** `verify` reports drift, because a
  ledger that disagrees with stock makes a hold move the wrong number of units. Nothing reconciles
  this automatically yet — re-run `verify` after sales.
- `apply` matches existing products **by barcode, not title**: the barcode is the identity a recall
  uses, so if the barcode exists the product exists whatever it is called.
- `seed.json` is generated and ages; it is not committed. `seed.example.json` is a snapshot for
  reference.
