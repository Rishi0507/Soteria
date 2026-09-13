package fsis

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"soteria/libs/feedkit/event"
	"soteria/libs/feedkit/httpx"
)

func fixture(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/recalls.json")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParseFixture(t *testing.T) {
	items, err := Parse(fixture(t), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("want 3 items got %d", len(items))
	}

	boar := items[0]
	if boar.SourceID != "031-2024" {
		t.Errorf("source_id: %q", boar.SourceID)
	}
	if boar.PublishedAt == nil || !boar.PublishedAt.Equal(time.Date(2024, 7, 26, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("published_at: %v", boar.PublishedAt)
	}
	if boar.SourceURL != "https://www.fsis.usda.gov/recalls-alerts/boar-s-head-provisions-co.-recalls-ready-eat-liverwurst-and-other-deli-meat-products-due" {
		t.Errorf("source_url: %q", boar.SourceURL)
	}
	n := boar.Normalized
	if n.Firm != "EST. 12612" || n.Classification != "High - Class I" || n.Distribution != "Nationwide" || n.Country != "US" {
		t.Errorf("normalized: %+v", n)
	}
	// HTML list → one product per line, entities decoded, tags gone.
	if strings.Contains(n.ProductDescription, "<") || !strings.Contains(n.ProductDescription, "\n") {
		t.Errorf("product_description not flattened: %q", n.ProductDescription)
	}
	if !strings.HasPrefix(n.ProductDescription, "3.5-lb. loaves") || !strings.Contains(n.ProductDescription, "sell by date 10 AUG") {
		t.Errorf("product_description: %q", n.ProductDescription)
	}
	if !strings.HasPrefix(n.Reason, "Product Contamination — ") || !strings.Contains(n.Reason, "Listeria monocytogenes") {
		t.Errorf("reason: %q", n.Reason)
	}

	wolverine := items[1]
	if !strings.Contains(wolverine.Normalized.ProductDescription, `"Wolverine Packing Co. 5oz Ground Beef Patties" with lot code 3927`) {
		t.Errorf("entities not decoded: %q", wolverine.Normalized.ProductDescription)
	}

	pha := items[2]
	if pha.SourceID != "PHA-09052026-01" || pha.Normalized.Classification != "" {
		t.Errorf("public health alert: %+v", pha.Normalized)
	}
	if pha.PublishedAt == nil || !pha.PublishedAt.Equal(time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("'Sep 5, 2026' date not parsed: %v", pha.PublishedAt)
	}
	if !strings.HasPrefix(pha.SourceURL, "https://www.fsis.usda.gov/recalls-alerts/fsis-issues") {
		t.Errorf("absolute press release URL mangled: %q", pha.SourceURL)
	}

	for _, it := range items {
		if err := event.New("ingestion-usda", "usda_fsis", it, time.Now()).Validate(); err != nil {
			t.Errorf("%s: %v", it.SourceID, err)
		}
	}
}

func TestParseFiltersBySince(t *testing.T) {
	items, err := Parse(fixture(t), time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].SourceID != "PHA-09052026-01" {
		t.Fatalf("since filter: got %d items", len(items))
	}
}

func TestParseAcceptsWrappedPayload(t *testing.T) {
	body := []byte(`{"data":[{"field_recall_number":"001-2026","field_title":"x","field_recall_date":"2026-01-02"}]}`)
	items, err := Parse(body, time.Time{})
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%d err=%v", len(items), err)
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	if _, err := Parse([]byte(`<html>Access Denied</html>`), time.Time{}); err == nil {
		t.Fatal("an Akamai block page must surface as an error, not an empty poll")
	}
}

func TestFetchSendsAcceptHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/json" {
			t.Errorf("Accept: %q", r.Header.Get("Accept"))
		}
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()
	s := New(httpx.New("test"))
	s.BaseURL = srv.URL
	items, err := s.Fetch(context.Background(), time.Time{})
	if err != nil || len(items) != 0 {
		t.Fatalf("items=%d err=%v", len(items), err)
	}
}

func TestStripHTML(t *testing.T) {
	in := `<p>Hello&nbsp;<em>world</em></p><ul><li>one &amp; two</li><li>three</li></ul>`
	want := "Hello world\none & two\nthree"
	if got := stripHTML(in); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
