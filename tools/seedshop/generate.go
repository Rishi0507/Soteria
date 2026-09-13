package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"soteria/libs/core/matching"
	"soteria/libs/shopify"
)

const openFDAEnforcement = "https://api.fda.gov/food/enforcement.json"

// runGenerate builds a seed from live openFDA enforcement reports.
//
// The point is that the demo catalog is not invented: every product is a real
// item with a real barcode that a real agency recalled, and the lot code the
// notice names is a lot the store actually holds. The only invented part is the
// *clean* stock beside it, and that is the part being demonstrated: a hold that
// takes the recalled lot and leaves the rest sellable.
func runGenerate(ctx context.Context, args []string) error {
	fs := flags("generate", args)
	out := fs.String("out", "seed.json", "file to write")
	count := fs.Int("count", 3, "how many recalled products to seed")
	days := fs.Int("days", 45, "how far back to search for recalls")
	if err := fs.Parse(args); err != nil {
		return err
	}

	records, err := fetchEnforcement(ctx, *days)
	if err != nil {
		return err
	}
	fmt.Printf("openFDA: %d enforcement reports in the last %d days\n", len(records), *days)

	seed := Seed{
		GeneratedAt: time.Now().UTC(),
		Source:      "openFDA food enforcement reports",
	}
	for _, r := range records {
		if len(seed.Products) >= *count {
			break
		}
		p, ok := toSeedProduct(r)
		if !ok {
			continue
		}
		seed.Products = append(seed.Products, p)
	}
	if len(seed.Products) == 0 {
		return fmt.Errorf("no recall in the last %d days carried both a valid barcode and a lot code; widen -days", *days)
	}

	// A substitute the rescue flow can offer: same category and price, and it
	// must not be under recall itself.
	seed.Products = append(seed.Products, substituteFor(seed.Products[0]))

	if err := writeSeed(*out, seed); err != nil {
		return err
	}
	fmt.Printf("\nwrote %s with %d products:\n", *out, len(seed.Products))
	for _, p := range seed.Products {
		fmt.Printf("  %-14s %-42s %d units in %d lots\n", p.Barcode, truncate(p.Title, 42), p.Units(), len(p.Lots))
		if p.Provenance != "" {
			fmt.Printf("                 %s\n", p.Provenance)
		}
	}
	fmt.Printf("\nNext: seedshop apply -seed %s\n", *out)
	return nil
}

type enforcementRecord struct {
	RecallNumber       string `json:"recall_number"`
	ProductDescription string `json:"product_description"`
	CodeInfo           string `json:"code_info"`
	ReasonForRecall    string `json:"reason_for_recall"`
	RecallingFirm      string `json:"recalling_firm"`
	Classification     string `json:"classification"`
	ReportDate         string `json:"report_date"`
}

func fetchEnforcement(ctx context.Context, days int) ([]enforcementRecord, error) {
	from := time.Now().AddDate(0, 0, -days).Format("20060102")
	to := time.Now().Format("20060102")

	q := url.Values{}
	q.Set("search", fmt.Sprintf("report_date:[%s TO %s]", from, to))
	q.Set("limit", "100")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, openFDAEnforcement+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "soteria-seedshop/1.0 (+https://soteria.dev)")

	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("openfda: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openfda: HTTP %d", resp.StatusCode)
	}
	var body struct {
		Results []enforcementRecord `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("openfda: decode: %w", err)
	}
	return body.Results, nil
}

var (
	barcodeCandidate = regexp.MustCompile(`\b\d[\d\s-]{6,18}\d\b`)
	lotCandidate     = regexp.MustCompile(`(?i)\b(?:lot|batch)\s*(?:code|number|no\.?|#)?\s*[:\-]?\s*([A-Z0-9][A-Z0-9\-]{2,19})`)
)

// toSeedProduct converts a recall into a product the store can carry, or reports
// that it is unusable. A recall without a valid barcode or without a lot code is
// not a demo of surgical containment, so it is skipped rather than patched up.
func toSeedProduct(r enforcementRecord) (SeedProduct, bool) {
	gtin := firstValidGTIN(r.ProductDescription)
	if gtin == "" {
		return SeedProduct{}, false
	}
	lots := matching.ExtractLotCodes(r.CodeInfo)
	if len(lots) == 0 {
		return SeedProduct{}, false
	}
	recalled := lots[0]

	title := cleanTitle(r.ProductDescription)
	vendor := strings.TrimSpace(r.RecallingFirm)
	if vendor == "" {
		vendor = "Unknown"
	}

	return SeedProduct{
		Title:   title,
		Vendor:  vendor,
		SKU:     skuFrom(gtin),
		Barcode: gtin,
		Price:   "4.99",
		// One recalled lot plus two clean ones. The clean stock is what makes the
		// containment surgical rather than a whole-SKU pull, and it is the only
		// invented part of this record.
		Lots: []shopify.Lot{
			{Code: recalled, Units: 40},
			{Code: "CLEAN-A", Units: 35},
			{Code: "CLEAN-B", Units: 25},
		},
		Provenance: fmt.Sprintf("openFDA %s (%s): %s", r.RecallNumber, r.Classification, truncate(r.ReasonForRecall, 70)),
	}, true
}

// substituteFor builds an unaffected product in the same price band, so the
// order rescue flow has something safe to offer.
func substituteFor(p SeedProduct) SeedProduct {
	return SeedProduct{
		Title:      "Soteria Pantry Substitute Pack",
		Vendor:     "Soteria Pantry",
		SKU:        "SOT-SUB-01",
		Barcode:    "012345678905", // a valid GTIN reserved for the demo substitute
		Price:      p.Price,
		Lots:       []shopify.Lot{{Code: "SUB-2026-01", Units: 120}},
		Provenance: "not under recall; the same-price option the rescue flow offers",
	}
}

// firstValidGTIN returns the first number in the text that is a real GTIN.
// Check-digit validation matters here: recall text is full of weights, case
// counts and phone numbers, and a plausible-looking number that is not a GTIN
// would seed a product no recall could ever match.
func firstValidGTIN(text string) string {
	for _, c := range barcodeCandidate.FindAllString(text, -1) {
		if g := matching.NormalizeGTIN(c); g != "" {
			return canonicalBarcode(g)
		}
	}
	return ""
}

// canonicalBarcode narrows a GTIN-14 to the shortest standard width that keeps
// every significant digit, which is the form printed on the pack and the form a
// merchant types into Shopify.
//
// Stripping leading zeros without regard to width is the trap here: it turns
// 00085315054108 into an 11-digit string that is no GTIN at all, and a product
// seeded with one can never be matched by a recall.
func canonicalBarcode(gtin14 string) string {
	for _, width := range []int{8, 12, 13, 14} {
		if len(gtin14) < width {
			continue
		}
		candidate := gtin14[len(gtin14)-width:]
		if strings.Trim(gtin14[:len(gtin14)-width], "0") != "" {
			continue // dropping a significant digit would be a different product
		}
		if matching.NormalizeGTIN(candidate) != "" {
			return candidate
		}
	}
	return gtin14
}

func cleanTitle(description string) string {
	t := strings.TrimSpace(strings.ReplaceAll(description, "\n", " "))
	if i := strings.Index(t, ". "); i > 20 && i < 80 {
		t = t[:i]
	}
	return truncate(t, 90)
}

func skuFrom(gtin string) string {
	if len(gtin) < 6 {
		return "SOT-" + gtin
	}
	return "SOT-" + gtin[len(gtin)-6:]
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(strings.Join(strings.Fields(s), " "))
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
