package offacts

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestHTTPCoverageDistinguishesUntaggedFromAllergenFree is the reason coverage
// exists: Open Food Facts omits allergens_tags for products nobody annotated,
// which is byte-identical to a product that genuinely has no allergens.
func TestHTTPCoverageDistinguishesUntaggedFromAllergenFree(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		coverage string
		allergen string
	}{
		{
			name:     "tagged with allergens",
			body:     `{"status":1,"product":{"product_name":"Bars","allergens_tags":["en:peanuts"]}}`,
			coverage: CoverageComplete,
			allergen: "peanuts",
		},
		{
			name:     "tagged and genuinely allergen-free",
			body:     `{"status":1,"product":{"product_name":"Water","allergens_tags":[]}}`,
			coverage: CoverageComplete,
		},
		{
			name:     "never tagged, but has ingredients",
			body:     `{"status":1,"product":{"product_name":"Crackers","ingredients_text":"wheat flour, salt"}}`,
			coverage: CoveragePartial,
		},
		{
			name:     "bare record, nothing contributed",
			body:     `{"status":1,"product":{"product_name":"Mystery snack"}}`,
			coverage: CoverageAbsent,
		},
		{
			name:     "unknown barcode",
			body:     `{"status":0}`,
			coverage: CoverageAbsent,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(c.body))
			}))
			defer srv.Close()

			client := &HTTPClient{BaseURL: srv.URL, HTTP: srv.Client()}
			got, err := client.Lookup(context.Background(), "041196910537")
			if err != nil {
				t.Fatalf("lookup: %v", err)
			}
			if got.Coverage != c.coverage {
				t.Fatalf("coverage = %q, want %q", got.Coverage, c.coverage)
			}
			if c.allergen != "" && !got.AllergenSet()[c.allergen] {
				t.Fatalf("expected allergen %q in %v", c.allergen, got.Allergens)
			}
			if c.coverage != CoverageComplete && got.Complete() {
				t.Fatal("a record below COMPLETE must never report Complete()")
			}
		})
	}
}

func TestHTTPServerErrorIsAbsentNotEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	got, err := (&HTTPClient{BaseURL: srv.URL, HTTP: srv.Client()}).Lookup(context.Background(), "041196910537")
	if err != nil {
		t.Fatalf("a failed lookup is data, not an error: %v", err)
	}
	if got.Coverage != CoverageAbsent {
		t.Fatalf("coverage = %q, want ABSENT", got.Coverage)
	}
}

func TestStaticDefaultsToCompleteAndMissesAreAbsent(t *testing.T) {
	s := NewStatic(Product{GTIN: "1", Allergens: []string{"en:gluten"}})
	got, _ := s.Lookup(context.Background(), "1")
	if !got.Complete() {
		t.Fatal("a seeded product is authoritative test data, so COMPLETE")
	}
	missing, _ := s.Lookup(context.Background(), "2")
	if missing.Coverage != CoverageAbsent {
		t.Fatalf("coverage = %q, want ABSENT", missing.Coverage)
	}
}

func TestAllergenSetIncludesTracesAndNormalizesTags(t *testing.T) {
	p := Product{Allergens: []string{"en:peanuts"}, Traces: []string{"en:tree_nuts"}, Coverage: CoverageComplete}
	set := p.AllergenSet()
	if !set["peanuts"] {
		t.Error("expected the declared allergen")
	}
	if !set["tree nuts"] {
		t.Error("a may-contain trace is not an acceptable risk in a recall substitute")
	}
}
