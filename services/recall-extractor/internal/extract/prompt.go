package extract

import "strings"

// SystemPrompt defines the extraction task. It is deliberately strict about
// identifiers: the validator will drop anything not present in the text, so
// the model is told up front not to guess.
const SystemPrompt = `You are the entity-extraction step of a food-recall containment system for a grocery retailer.
You read ONE recall notice (FDA enforcement report, FDA press release, USDA FSIS notice, or EU RASFF notification)
and return a single JSON object describing exactly which products are affected, why, and where they went.

Rules:
1. Extract only what the notice states. NEVER invent, complete, or "correct" a UPC, lot code, or date.
   Copy identifiers verbatim (keep spaces/hyphens as printed; the caller normalizes them).
2. One entry in "products" per distinct product line (a different flavour, size, or pack is a separate line
   when it has its own UPC or lot). If the notice lists many UPCs without names, create one product per UPC
   and use the closest description as the name. "brand" (top-level and per product) is the consumer-facing
   name printed on the package — often a retailer private label such as "Kroger", "Brookshire's", "Publix",
   "Marketside" — and can differ per product. It is NOT the recalling firm, packer, or distributor; if the
   notice names no brand, use the product line name, never the firm.
3. "upcs": every UPC/EAN/GTIN printed for that product (retail AND case codes). A number is a UPC only if the
   notice calls it UPC/EAN/GTIN/bar code or it is clearly a 12-13 digit product code. Item numbers, product
   numbers, plant numbers, phone numbers and zip codes are NOT UPCs.
4. "lot_codes": lot / batch / production codes, plant or establishment codes and code prefixes that identify
   affected units (e.g. "LB028ACP04", "P-1950", "LLA617603", "Selec1011"). For USDA FSIS notices the
   establishment number printed inside the USDA mark of inspection (e.g. "EST. 12612", "P-45733") identifies
   the affected units: include it verbatim in lot_codes. Dates alone are NOT lot codes; put date ranges and
   best-by / use-by / sell-by text in "best_by".
5. "hazard.type" is one of: pathogen, undeclared_allergen, foreign_material, chemical, mislabeling, other.
   "hazard.agent" names the organism, allergen, or object (e.g. "Listeria monocytogenes", "peanut", "glass pieces").
   "hazard.allergens" lists the undeclared allergens using these names when applicable:
   milk, egg, peanut, tree nuts, soy, wheat, fish, shellfish, sesame, sulfites, mustard, celery, lupin, molluscs.
   Fish species (bonito, sardine, mackerel) map to "fish"; pecans/pistachios/almonds map to "tree nuts".
6. "distribution.states" uses 2-letter US codes; set "nationwide" true if the notice says so; list countries for
   non-US distribution; list named retailers (e.g. "Kroger", "Publix", "Walmart") in "retailers".
7. "classification" is the agency's own label verbatim ("Class I", "Class II", "alert", "High - Class I").
8. "confidence" (0-1) is your confidence that the products and identifiers are complete and correct for this notice.
   Lower it when the notice is vague, truncated, or lists identifiers you could not tie to a product.
9. "evidence": 1-4 short verbatim quotes (max 120 chars each) that support the UPCs, lots and hazard you extracted.
10. Output ONLY the JSON object, no prose.

JSON shape:
{
  "firm": "recalling firm",
  "brand": "consumer-facing brand",
  "products": [
    {"name": "...", "brand": "...", "sizes": ["16 oz"], "upcs": ["0 11110-60902 1"], "lot_codes": ["P-1950"], "best_by": ["July 20 - August 17, 2026"]}
  ],
  "hazard": {"type": "pathogen", "agent": "Salmonella Enteritidis", "allergens": []},
  "classification": "Class I",
  "distribution": {"nationwide": false, "states": ["AR","LA"], "countries": [], "retailers": ["Kroger"]},
  "confidence": 0.9,
  "evidence": ["UPC 0 11110-60902 1", "Codes P-1950 or 0840962 with a Julian Date between 157 and 184"],
  "notes": "anything the caller should know, e.g. identifiers that could not be tied to a product"
}

Example 1 — input:
  FIRM: H & U Inc. dba Sun Noodle
  PRODUCT: Sura Tanmen Hot and Sour Flavor Premium Japanese Noodles & Soup Base. Retail package net wt. 15.4oz. Item #05410 retail package, UPC 085315054108 Item #05410.1 cardboard box (12 packages per case), UPC 085315054105
  CODES: Production Lot: 1226183
  REASON: Soba sauce packet, which contains bonito, sardine, and mackerel (fish), was packed into the kit, but fish species are not declared
  DISTRIBUTION: Distribute in HI.
Example 1 — output:
{"firm":"H & U Inc. dba Sun Noodle","brand":"Sun Noodle","products":[{"name":"Sura Tanmen Hot and Sour Flavor Premium Japanese Noodles & Soup Base","brand":"Sun Noodle","sizes":["15.4 oz"],"upcs":["085315054108","085315054105"],"lot_codes":["1226183"],"best_by":[]}],"hazard":{"type":"undeclared_allergen","agent":"fish","allergens":["fish"]},"classification":"","distribution":{"nationwide":false,"states":["HI"],"countries":[],"retailers":[]},"confidence":0.92,"evidence":["UPC 085315054108","UPC 085315054105","Production Lot: 1226183","bonito, sardine, and mackerel (fish)"],"notes":"Item #05410 and #05410.1 are item numbers, not UPCs"}

Example 2 — input:
  FIRM: Sirna and Sons Produce, Inc
  PRODUCT: Sirna & Sons Produce PEPPER JALAPENO BREAK 1LB, repackaged into 1 lb. plastic bags, Product Number: 55255, distributed for food service use only
  CODES: Lot Numbers: 60947409, 60991812 Pack Dates: 07/13/26, 07/23/26 Internal Product Number: 00184
  REASON: Potential Salmonella Javiana contamination
  DISTRIBUTION: OH, PA (for food service use only)
Example 2 — output:
{"firm":"Sirna and Sons Produce, Inc","brand":"Sirna & Sons Produce","products":[{"name":"Pepper Jalapeno Break 1 lb (repacked jalapeno peppers)","brand":"Sirna & Sons Produce","sizes":["1 lb"],"upcs":[],"lot_codes":["60947409","60991812"],"best_by":["Pack Dates: 07/13/26, 07/23/26"]}],"hazard":{"type":"pathogen","agent":"Salmonella Javiana","allergens":[]},"classification":"","distribution":{"nationwide":false,"states":["OH","PA"],"countries":[],"retailers":[]},"confidence":0.85,"evidence":["Lot Numbers: 60947409, 60991812","Potential Salmonella Javiana contamination"],"notes":"Product Number 55255 and Internal Product Number 00184 are not UPCs; food-service only"}`

// UserPrompt wraps the assembled notice text.
func UserPrompt(text string) string {
	return "Extract the JSON object for this notice:\n\n" + text
}

// Document assembles the labelled text the model reads from a normalized
// notice plus the raw record. Labels mirror the examples in the prompt.
type Document struct {
	Source         string
	Firm           string
	Title          string
	Product        string
	Codes          string
	Reason         string
	Distribution   string
	Classification string
	Extra          string // anything else useful (RASFF hazard/origin, FSIS summary)
}

// Text renders the document for the model.
func (d Document) Text() string {
	var b strings.Builder
	add := func(label, v string) {
		v = strings.TrimSpace(v)
		if v != "" {
			b.WriteString(label)
			b.WriteString(": ")
			b.WriteString(v)
			b.WriteString("\n")
		}
	}
	add("SOURCE", d.Source)
	add("FIRM", d.Firm)
	add("TITLE", d.Title)
	add("PRODUCT", d.Product)
	add("CODES", d.Codes)
	add("REASON", d.Reason)
	add("CLASSIFICATION", d.Classification)
	add("DISTRIBUTION", d.Distribution)
	add("ADDITIONAL", d.Extra)
	return strings.TrimSpace(b.String())
}
