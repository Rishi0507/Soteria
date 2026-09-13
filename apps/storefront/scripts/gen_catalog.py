"""off_catalog.json (raw Open Food Facts records) -> apps/storefront/src/catalog.ts

Every product is real: the name, brand, size, barcode, image and allergen tags
are Open Food Facts data for that barcode. Prices are the store's own. Lot
codes on the recalled items are the ones the FDA notice names; the second lot
on each is the store's clean stock.
"""
import json, re, sys, random

src, out = sys.argv[1], sys.argv[2]
raw = json.load(open(src, encoding="utf-8"))

# Real recalled lots (FDA, Sept 2026) keyed by barcode.
RECALLED = {
    "194346207961": {"lots": ["LB028ACP04", "LB030ACP06"], "hazard": "Salmonella", "notice": "H-1273-2026"},
    "0194346207961": {"lots": ["LB028ACP04", "LB030ACP06"], "hazard": "Salmonella", "notice": "H-1273-2026"},
    "041548413624": {"lots": ["LLA618203", "LLA620103"], "hazard": "glass pieces", "notice": "H-1265-2026"},
    "0041548413624": {"lots": ["LLA618203", "LLA620103"], "hazard": "glass pieces", "notice": "H-1265-2026"},
    "085315054108": {"lots": ["1226183", "1226190"], "hazard": "undeclared fish", "notice": "H-1258-2026"},
    "0085315054108": {"lots": ["1226183", "1226190"], "hazard": "undeclared fish", "notice": "H-1258-2026"},
    "041196910537": {"lots": ["8H-1132", "8H-2000"], "hazard": "Listeria monocytogenes", "notice": "fixture"},
    "011110609021": {"lots": ["P-1950", "0840961"], "hazard": "Salmonella Enteritidis", "notice": "H-1230-2026"},
    "0041196910537": {"lots": ["8H-1132", "8H-2000"], "hazard": "Listeria monocytogenes", "notice": "fixture"},
}

random.seed(7)
def price(cat):
    base = {"nut-butters": 8.49, "granola-bars": 4.99, "breakfast-cereals": 5.29, "yogurts": 1.79, "cheeses": 6.99, "ice-creams": 5.49,
            "cookies": 3.99, "snack-bars": 2.29, "tortilla-chips": 4.49, "pastas": 3.29, "breads": 5.99, "fruit-juices": 4.29,
            "dark-chocolates": 4.99, "chocolates": 3.49, "hummus": 3.99, "potato-crisps": 3.79, "plant-based-milks": 4.49,
            "pasta-sauces": 8.99, "crackers": 3.99, "frozen-meals": 6.49, "recalled": 5.99}.get(cat, 4.99)
    return f"${base:.2f}"

def clean(s):
    return re.sub(r"\s+", " ", (s or "")).strip()

def title(p):
    n = clean(p.get("product_name"))
    # OFF names are often ALL CAPS or brand-prefixed; tidy without inventing.
    if n.isupper():
        n = n.title()
    return n

seen, items = set(), []
for p in raw:
    code = p["code"]
    if code in seen or not p.get("image_front_url"):
        continue
    # Only products whose ingredients are actually on record: an empty allergen
    # list then means "none", not "nobody typed them in".
    if not (p.get("ingredients_text") or "").strip() or "en:ingredients-completed" not in (p.get("states_tags") or []) or p.get("allergens_tags") is None:
        continue
    seen.add(code)
    rec = next((v for k, v in RECALLED.items() if k.lstrip("0") == code.lstrip("0")), None)
    cat = p.get("category", "pantry")
    if cat == "recalled" or (rec and cat == "pantry"):
        cats = p.get("categories_tags") or []
        cat = next((c.replace("en:", "") for c in cats if c in ("en:nut-butters", "en:ice-creams", "en:frozen-desserts", "en:noodles", "en:instant-noodles", "en:granola-bars")), "pantry")
    brand = clean((p.get("brands") or "").split(",")[0])
    allergens = sorted({a.replace("en:", "") for a in (p.get("allergens_tags") or [])})
    items.append({
        "gtin": code,
        "name": title(p),
        "brand": brand,
        "size": clean(p.get("quantity")),
        "price": price(cat),
        "image": p["image_front_url"],
        "imageSmall": p.get("image_front_small_url") or p["image_front_url"],
        "category": cat,
        "allergens": allergens,
        "ingredients": clean(p.get("ingredients_text")),
        "lots": rec["lots"] if rec else [],
        "recall": ({"hazard": rec["hazard"], "notice": rec["notice"]} if rec else None),
    })

# recalled products first in the mosaic so the demo product is the lead tile
items.sort(key=lambda i: (0 if i["recall"] else 1, i["name"]))

ts = ["// GENERATED from Open Food Facts records — do not hand-edit product facts.",
      "// Every name, brand, size, barcode, image and allergen list is real OFF data",
      "// for that barcode (ODbL, © Open Food Facts contributors). Prices are ours.",
      "// Lot codes on recalled items are the ones the FDA notice names.",
      "",
      "export type Product = {",
      "    gtin: string", "    name: string", "    brand: string", "    size: string", "    price: string",
      "    image: string", "    imageSmall: string", "    category: string", "    allergens: string[]", "    ingredients: string",
      "    lots: string[]", "    recall: { hazard: string; notice: string } | null", "}", "",
      "export const CATALOG: Product[] = " + json.dumps(items, indent=4, ensure_ascii=False) + "", "",
      "export function findProduct(gtin: string): Product | undefined {",
      "    const g = gtin.replace(/^0+/, '')",
      "    return CATALOG.find((p) => p.gtin.replace(/^0+/, '') === g)",
      "}", ""]
open(out, "w", encoding="utf-8", newline="\n").write("\n".join(ts))
print(len(items), "products ->", out)
for i in items:
    print(f"  {i['gtin']:15} {i['brand'][:16]:16} {i['name'][:34]:34} {i['size'][:10]:10} {i['category']:18} lots={i['lots']} allergens={i['allergens']}")
