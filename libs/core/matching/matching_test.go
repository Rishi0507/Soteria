package matching

import (
	"reflect"
	"testing"
)

func TestNormalizeGTIN(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"0 41196 91053 7", "00041196910537"}, // UPC-A -> GTIN-14
		{"041196910537", "00041196910537"},
		{"4119691053", ""},   // wrong length
		{"041196910538", ""}, // bad check digit
		{"", ""},
	}
	for _, c := range cases {
		if got := NormalizeGTIN(c.in); got != c.want {
			t.Errorf("NormalizeGTIN(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestExtractLotCodes(t *testing.T) {
	text := `Recalled product bears Lot Code: 8H-1132, 8H-1133 and 8H-1140.
	         Batch # A2291 was also affected. Lot numbers are printed on the side panel.`
	got := ExtractLotCodes(text)
	want := []string{"8H-1132", "8H-1133", "8H-1140", "A2291"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ExtractLotCodes = %v, want %v", got, want)
	}
}

func TestExtractLotCodesIgnoresProse(t *testing.T) {
	if got := ExtractLotCodes("No lot numbers were provided by the firm."); len(got) != 0 {
		t.Fatalf("expected no lot codes from prose, got %v", got)
	}
}

func TestScoreExactGTINDominates(t *testing.T) {
	s := Signals{
		UPCs:     []string{"0 41196 91053 7"},
		Brands:   []string{"Sunfield Farms"},
		Text:     "Sunfield Farms Creamy Peanut Butter 16 oz jars, undeclared peanut",
		LotCodes: []string{"8H-1132"},
	}
	c := Candidate{
		GTIN:         "041196910537",
		SKU:          "SF-PB-16",
		Brand:        "Sunfield Farms",
		ProductTitle: "Sunfield Farms Creamy Peanut Butter 16 oz",
		LotCodes:     []string{"8H-1132", "8H-2000"},
	}
	got := Score(s, c)
	if got.Confidence < 0.9 {
		t.Fatalf("exact GTIN + brand + title should be high confidence, got %.3f (%v)", got.Confidence, got.Evidence)
	}
	if !reflect.DeepEqual(got.LotCodes, []string{"8H-1132"}) {
		t.Fatalf("lot intersection = %v, want [8H-1132]", got.LotCodes)
	}
}

func TestScoreTextOnlyStaysBelowAutoHold(t *testing.T) {
	s := Signals{
		Brands: []string{"Sunfield Farms"},
		Text:   "Sunfield Farms Creamy Peanut Butter 16 oz",
	}
	c := Candidate{
		GTIN:         "041196910537",
		Brand:        "Sunfield Farms",
		ProductTitle: "Sunfield Farms Creamy Peanut Butter 16 oz",
	}
	// Brand + a perfect title is still only circumstantial: it must land in the
	// human-review band, not auto-hold.
	if got := Score(s, c); got.Confidence >= 0.85 {
		t.Fatalf("text-only match should stay below the auto-hold band, got %.3f", got.Confidence)
	}
}

func TestScoreWrongProductScoresLow(t *testing.T) {
	s := Signals{UPCs: []string{"041196910537"}, Brands: []string{"Sunfield Farms"}, Text: "Creamy Peanut Butter"}
	c := Candidate{GTIN: "012345678905", Brand: "Northvale", ProductTitle: "Sparkling Mineral Water 1L"}
	if got := Score(s, c); got.Confidence > 0.1 {
		t.Fatalf("unrelated product scored %.3f", got.Confidence)
	}
}

func TestRankOrdersAndFloors(t *testing.T) {
	s := Signals{UPCs: []string{"041196910537"}, Text: "Creamy Peanut Butter 16 oz"}
	results := Rank(s, []Candidate{
		{GTIN: "012345678905", ProductTitle: "Sparkling Mineral Water 1L"},
		{GTIN: "041196910537", ProductTitle: "Creamy Peanut Butter 16 oz"},
	}, 0.3)
	if len(results) != 1 {
		t.Fatalf("floor should drop the unrelated candidate, got %d results", len(results))
	}
	if results[0].Candidate.GTIN != "041196910537" {
		t.Fatalf("wrong top match: %s", results[0].Candidate.GTIN)
	}
}

func TestSilentDiffAloneIsReviewBand(t *testing.T) {
	s := Signals{SKUs: []string{"SF-PB-16"}, Text: "Sunfield Creamy Peanut Butter", SilentDiff: 0.9}
	c := Candidate{GTIN: "041196910537", SKU: "SF-PB-16", ProductTitle: "Sunfield Creamy Peanut Butter 16 oz"}
	got := Score(s, c)
	if got.Confidence >= 0.85 || got.Confidence < 0.4 {
		t.Fatalf("silent-diff signal should land in the review band, got %.3f", got.Confidence)
	}
}

func TestTitleSimilarityHandlesNoisyNoticeText(t *testing.T) {
	notice := "The firm is recalling Sunfield Farms Creamy Peanut Butter 16 oz jars because they may contain undeclared peanut, distributed in CA, OR and WA."
	if sim := TitleSimilarity(notice, "Sunfield Farms Creamy Peanut Butter 16 oz"); sim < 0.5 {
		t.Fatalf("short title inside a long notice should still match, got %.3f", sim)
	}
}
