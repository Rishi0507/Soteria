// Package extract turns an unstructured recall notice into structured,
// validated entities using an LLM behind a deterministic guardrail.
//
// The model proposes; the validator disposes. Every UPC and lot code the
// model returns must be present in the notice text (the model cannot invent
// identifiers), UPCs must be check-digit-valid GTINs, and anything the
// validator removes is recorded in Corrections so the decision is auditable.
// Valid GTINs the model missed are added back from a regex pass, so the
// output is never worse than the deterministic baseline.
package extract

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"soteria/libs/core/matching"
)

// Hazard types (contracts/events/recall.extracted.v1.json).
const (
	HazardPathogen        = "pathogen"
	HazardAllergen        = "undeclared_allergen"
	HazardForeignMaterial = "foreign_material"
	HazardChemical        = "chemical"
	HazardMislabeling     = "mislabeling"
	HazardOther           = "other"
)

var hazardTypes = map[string]bool{HazardPathogen: true, HazardAllergen: true, HazardForeignMaterial: true, HazardChemical: true, HazardMislabeling: true, HazardOther: true}

// Product is one recalled product line.
type Product struct {
	Name     string   `json:"name"`
	Brand    string   `json:"brand,omitempty"`
	Sizes    []string `json:"sizes,omitempty"`
	UPCs     []string `json:"upcs,omitempty"`      // normalized digits, check-digit valid
	LotCodes []string `json:"lot_codes,omitempty"` // verbatim from the notice
	BestBy   []string `json:"best_by,omitempty"`   // verbatim date/code text
}

// Hazard is why the product is recalled.
type Hazard struct {
	Type      string   `json:"type"`
	Agent     string   `json:"agent,omitempty"`     // "Listeria monocytogenes", "glass pieces", "peanut"
	Allergens []string `json:"allergens,omitempty"` // lowercase, e.g. "milk", "peanut", "tree nuts"
}

// Distribution is where the product went.
type Distribution struct {
	Nationwide bool     `json:"nationwide,omitempty"`
	States     []string `json:"states,omitempty"` // 2-letter US codes
	Countries  []string `json:"countries,omitempty"`
	Retailers  []string `json:"retailers,omitempty"`
}

// Extraction is the structured reading of one notice.
type Extraction struct {
	Source         string       `json:"source"`
	SourceID       string       `json:"source_id"`
	Firm           string       `json:"firm,omitempty"`
	Brand          string       `json:"brand,omitempty"`
	Products       []Product    `json:"products"`
	Hazard         Hazard       `json:"hazard"`
	Classification string       `json:"classification,omitempty"`
	Distribution   Distribution `json:"distribution"`
	Confidence     float64      `json:"confidence"`
	Evidence       []string     `json:"evidence,omitempty"` // short verbatim quotes supporting the key fields
	Notes          string       `json:"notes,omitempty"`
	Model          string       `json:"model"`
	ExtractedAt    time.Time    `json:"extracted_at"`
	Corrections    []string     `json:"corrections,omitempty"` // what the validator changed, and why
}

// AllUPCs flattens product UPCs (deduplicated, sorted).
func (e Extraction) AllUPCs() []string {
	return uniqSorted(collect(e.Products, func(p Product) []string { return p.UPCs }))
}

// AllLots flattens product lot codes (deduplicated, sorted).
func (e Extraction) AllLots() []string {
	return uniqSorted(collect(e.Products, func(p Product) []string { return p.LotCodes }))
}

// ---- validation ----------------------------------------------------------------

var (
	// A run of 12–14 digits possibly broken by single spaces or hyphens, as
	// agencies print UPCs ("0 11110-60902 1", "1 94346474004"). GTIN-8 is
	// deliberately excluded: 8-digit dates (20260811) and lot numbers pass the
	// check digit one time in ten and would be reported as products.
	digitRun = regexp.MustCompile(`(?:\d[ -]?){11,13}\d`)
	nonDigit = regexp.MustCompile(`\D`)
	wsRun    = regexp.MustCompile(`\s+`)
	stateRe  = regexp.MustCompile(`^[A-Z]{2}$`)
)

// allergenAliases maps model spellings to the canonical major-allergen names.
var allergenAliases = map[string]string{
	"milk": "milk", "dairy": "milk", "lactose": "milk", "whey": "milk", "casein": "milk", "cheese": "milk", "butter": "milk",
	"egg": "egg", "eggs": "egg", "egg lysozyme": "egg",
	"peanut": "peanut", "peanuts": "peanut", "peanut flour": "peanut",
	"tree nut": "tree nuts", "tree nuts": "tree nuts", "almond": "tree nuts", "almonds": "tree nuts", "pecan": "tree nuts", "pecans": "tree nuts",
	"pistachio": "tree nuts", "pistachios": "tree nuts", "walnut": "tree nuts", "walnuts": "tree nuts", "cashew": "tree nuts", "cashews": "tree nuts",
	"hazelnut": "tree nuts", "hazelnuts": "tree nuts", "coconut": "tree nuts",
	"soy": "soy", "soya": "soy", "soybean": "soy", "soybeans": "soy",
	"wheat": "wheat", "gluten": "wheat",
	"fish": "fish", "bonito": "fish", "sardine": "fish", "mackerel": "fish", "anchovy": "fish", "anchovies": "fish",
	"shellfish": "shellfish", "crustacean": "shellfish", "crustacean shellfish": "shellfish", "shrimp": "shellfish", "crab": "shellfish", "lobster": "shellfish",
	"sesame": "sesame", "sesame seeds": "sesame",
	"sulfite": "sulfites", "sulfites": "sulfites", "sulphites": "sulfites",
	"mustard": "mustard", "celery": "celery", "lupin": "lupin", "molluscs": "molluscs", "mollusks": "molluscs",
}

// Validate applies the guardrail to a model output given the notice text.
// It mutates e in place and returns it for convenience.
func Validate(e *Extraction, text string) *Extraction {
	textDigits := digitRunsIn(text)      // set of normalized digit runs present in the text
	textNorm := normalizeForSearch(text) // lowercase, whitespace-collapsed
	var corrections []string

	seenUPC := map[string]bool{}
	for i := range e.Products {
		p := &e.Products[i]
		p.Name = strings.TrimSpace(p.Name)

		// UPCs: must appear in the text; valid GTINs are normalized to
		// GTIN-14. A code that is printed in the notice but fails its check
		// digit is a typo at the agency (it happens: see the Sun Noodle case
		// UPC), so it is kept verbatim and flagged rather than dropped —
		// a downstream exact match on the typo is still evidence.
		var upcs []string
		for _, raw := range p.UPCs {
			digits := nonDigit.ReplaceAllString(raw, "")
			present := textDigits[digits] || textDigits[strings.TrimLeft(digits, "0")]
			gtin := ""
			if len(digits) >= 12 && len(digits) <= 14 {
				gtin = matching.NormalizeGTIN(digits)
			}
			switch {
			case len(digits) < 12 || len(digits) > 14:
				corrections = append(corrections, "dropped upc "+strings.TrimSpace(raw)+": "+strconv.Itoa(len(digits))+" digits is not a GTIN")
			case !present && (gtin == "" || !textDigits[gtin]):
				corrections = append(corrections, "dropped upc "+strings.TrimSpace(raw)+": not present in notice text")
			case gtin == "":
				if !seenUPC[digits] {
					upcs = append(upcs, digits)
					seenUPC[digits] = true
					corrections = append(corrections, "kept upc "+digits+" verbatim: check digit invalid (typo in the notice?)")
				}
			case seenUPC[gtin]:
				// duplicate across products; keep on first product only
			default:
				upcs = append(upcs, gtin)
				seenUPC[gtin] = true
			}
		}
		p.UPCs = upcs

		// Lot codes: verbatim presence (case-insensitive, whitespace-normalized).
		var lots []string
		for _, raw := range p.LotCodes {
			code := strings.TrimSpace(raw)
			if code == "" {
				continue
			}
			if !strings.Contains(textNorm, normalizeForSearch(code)) {
				corrections = append(corrections, "dropped lot "+code+": not present in notice text")
				continue
			}
			lots = append(lots, code)
		}
		p.LotCodes = uniqKeepOrder(lots)
		p.Sizes = uniqKeepOrder(trimAll(p.Sizes))
		p.BestBy = uniqKeepOrder(trimAll(p.BestBy))
	}

	// Regex safety net: valid GTINs in the text the model did not return.
	missed := []string{}
	for _, m := range digitRun.FindAllString(text, -1) {
		run := nonDigit.ReplaceAllString(m, "")
		if len(run) < 12 || len(run) > 14 {
			continue
		}
		if gtin := matching.NormalizeGTIN(run); gtin != "" && !seenUPC[gtin] {
			missed = append(missed, gtin)
			seenUPC[gtin] = true
		}
	}
	if len(missed) > 0 {
		sort.Strings(missed)
		if len(e.Products) == 0 {
			e.Products = append(e.Products, Product{Name: "unspecified product"})
		}
		e.Products[0].UPCs = append(e.Products[0].UPCs, missed...)
		corrections = append(corrections, "added upcs found by regex but missed by the model: "+strings.Join(missed, ", "))
	}

	// Hazard.
	e.Hazard.Type = strings.ToLower(strings.TrimSpace(e.Hazard.Type))
	if !hazardTypes[e.Hazard.Type] {
		corrections = append(corrections, "hazard type "+e.Hazard.Type+" not in enum; set to other")
		e.Hazard.Type = HazardOther
	}
	var allergens []string
	for _, a := range e.Hazard.Allergens {
		k := strings.ToLower(strings.TrimSpace(a))
		if canon, ok := allergenAliases[k]; ok {
			k = canon
		}
		if k != "" {
			allergens = append(allergens, k)
		}
	}
	e.Hazard.Allergens = uniqKeepOrder(allergens)
	if len(e.Hazard.Allergens) > 0 && e.Hazard.Type != HazardAllergen && e.Hazard.Type != HazardMislabeling {
		corrections = append(corrections, "allergens listed but hazard type was "+e.Hazard.Type+"; set to undeclared_allergen")
		e.Hazard.Type = HazardAllergen
	}

	// Distribution.
	var states []string
	for _, s := range e.Distribution.States {
		s = strings.ToUpper(strings.TrimSpace(s))
		if stateRe.MatchString(s) {
			states = append(states, s)
		} else if full, ok := stateCodes[strings.ToLower(s)]; ok {
			states = append(states, full)
		} else if s != "" {
			corrections = append(corrections, "dropped state "+s+": not a US state code")
		}
	}
	e.Distribution.States = uniqKeepOrder(states)
	if strings.Contains(textNorm, "nationwide") {
		e.Distribution.Nationwide = true
	}

	// Confidence.
	if e.Confidence < 0 || e.Confidence > 1 {
		corrections = append(corrections, "confidence out of range; clamped")
	}
	e.Confidence = clamp(e.Confidence, 0, 1)
	if len(e.Products) == 0 {
		e.Confidence = min(e.Confidence, 0.3)
		corrections = append(corrections, "no products extracted; confidence capped at 0.3")
	}
	dropped := 0
	for _, c := range corrections {
		if strings.HasPrefix(c, "dropped") {
			dropped++
		}
	}
	if dropped > 0 {
		e.Confidence = clamp(e.Confidence-0.05*float64(dropped), 0, 1)
	}
	e.Corrections = append(e.Corrections, corrections...)
	return e
}

// digitRunsIn returns every normalized digit run in the text, keyed both
// with and without a leading zero so UPC-A/EAN-13 spellings match.
func digitRunsIn(text string) map[string]bool {
	out := map[string]bool{}
	for _, m := range digitRun.FindAllString(text, -1) {
		d := nonDigit.ReplaceAllString(m, "")
		out[d] = true
		out[strings.TrimLeft(d, "0")] = true
		if g := matching.NormalizeGTIN(d); g != "" {
			out[g] = true
		}
	}
	return out
}

func normalizeForSearch(s string) string {
	return strings.ToLower(wsRun.ReplaceAllString(strings.TrimSpace(s), " "))
}

func clamp(x, lo, hi float64) float64 {
	if x < lo {
		return lo
	}
	if x > hi {
		return hi
	}
	return x
}

func collect(ps []Product, f func(Product) []string) []string {
	var out []string
	for _, p := range ps {
		out = append(out, f(p)...)
	}
	return out
}

func uniqSorted(xs []string) []string {
	m := map[string]bool{}
	var out []string
	for _, x := range xs {
		if x != "" && !m[x] {
			m[x] = true
			out = append(out, x)
		}
	}
	sort.Strings(out)
	return out
}

func uniqKeepOrder(xs []string) []string {
	m := map[string]bool{}
	var out []string
	for _, x := range xs {
		if x != "" && !m[x] {
			m[x] = true
			out = append(out, x)
		}
	}
	return out
}

func trimAll(xs []string) []string {
	out := make([]string, 0, len(xs))
	for _, x := range xs {
		if t := strings.TrimSpace(x); t != "" {
			out = append(out, t)
		}
	}
	return out
}

var stateCodes = map[string]string{
	"alabama": "AL", "alaska": "AK", "arizona": "AZ", "arkansas": "AR", "california": "CA", "colorado": "CO", "connecticut": "CT", "delaware": "DE",
	"florida": "FL", "georgia": "GA", "hawaii": "HI", "idaho": "ID", "illinois": "IL", "indiana": "IN", "iowa": "IA", "kansas": "KS", "kentucky": "KY",
	"louisiana": "LA", "maine": "ME", "maryland": "MD", "massachusetts": "MA", "michigan": "MI", "minnesota": "MN", "mississippi": "MS", "missouri": "MO",
	"montana": "MT", "nebraska": "NE", "nevada": "NV", "new hampshire": "NH", "new jersey": "NJ", "new mexico": "NM", "new york": "NY", "north carolina": "NC",
	"north dakota": "ND", "ohio": "OH", "oklahoma": "OK", "oregon": "OR", "pennsylvania": "PA", "rhode island": "RI", "south carolina": "SC", "south dakota": "SD",
	"tennessee": "TN", "texas": "TX", "utah": "UT", "vermont": "VT", "virginia": "VA", "washington": "WA", "west virginia": "WV", "wisconsin": "WI", "wyoming": "WY",
	"district of columbia": "DC", "puerto rico": "PR",
}
