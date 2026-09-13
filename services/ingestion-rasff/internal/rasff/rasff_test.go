package rasff

import (
	"os"
	"strings"
	"testing"
	"time"

	"soteria/libs/feedkit/event"
)

func TestParseJSONFixture(t *testing.T) {
	f, err := os.Open("../../fixtures/rasff_2026-09_sample.json")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	ns, err := ParseJSON(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(ns) != 4 {
		t.Fatalf("want 4 notifications got %d", len(ns))
	}
	it, err := ToItem(ns[0])
	if err != nil {
		t.Fatal(err)
	}
	if it.SourceID != "2026.5412" || it.Normalized.Country != "EU" {
		t.Errorf("item: %+v", it.Normalized)
	}
	if it.PublishedAt == nil || !it.PublishedAt.Equal(time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("published_at: %v", it.PublishedAt)
	}
	n := it.Normalized
	if n.Title != "Salmonella Typhimurium in sesame seeds from India" || n.Reason != "Salmonella Typhimurium (presence /25g)" {
		t.Errorf("title/reason: %q / %q", n.Title, n.Reason)
	}
	if n.Classification != "alert / serious" {
		t.Errorf("classification: %q", n.Classification)
	}
	if !strings.Contains(n.ProductDescription, "lot SES-2607-A/B") || !strings.HasSuffix(n.ProductDescription, "(origin: India)") {
		t.Errorf("product_description: %q", n.ProductDescription)
	}
	if n.Distribution != "Germany, Netherlands, Poland, United States" {
		t.Errorf("distribution: %q", n.Distribution)
	}
	if it.SourceURL != "https://webgate.ec.europa.eu/rasff-window/screen/notification/2026.5412" {
		t.Errorf("source_url: %q", it.SourceURL)
	}
	// Border rejection with empty distribution falls back to the status text.
	br, _ := ToItem(ns[1])
	if br.Normalized.Distribution != "product not (yet) placed on the market" {
		t.Errorf("fallback distribution: %q", br.Normalized.Distribution)
	}
	for _, n := range ns {
		it, err := ToItem(n)
		if err != nil {
			t.Fatal(err)
		}
		if err := event.New("ingestion-rasff", "eu_rasff", it, time.Now()).Validate(); err != nil {
			t.Errorf("%s: %v", it.SourceID, err)
		}
	}
}

func TestParseCSVSemicolonWithBOM(t *testing.T) {
	f, err := os.Open("testdata/export_semicolon.csv")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	ns, err := ParseCSV(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(ns) != 2 {
		t.Fatalf("want 2 rows got %d: %+v", len(ns), ns)
	}
	if ns[0].Reference != "2026.5501" {
		t.Errorf("BOM not stripped / header not matched: %+v", ns[0])
	}
	if ns[0].NotifyingCountry != "Spain" || ns[0].Hazards != "shigatoxin-producing Escherichia coli (STEC)" {
		t.Errorf("row: %+v", ns[0])
	}
	it, err := ToItem(ns[0])
	if err != nil {
		t.Fatal(err)
	}
	// dd/mm/yyyy
	if it.PublishedAt == nil || !it.PublishedAt.Equal(time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("published_at: %v", it.PublishedAt)
	}
}

func TestHeaderAliasesAndTitleFallback(t *testing.T) {
	ns, err := ParseJSON(strings.NewReader(`[{"Notification reference":"2026.1","Date of notification":"2026-01-05","Hazard":"cadmium","Product name":"squid rings","Country of origin":"Vietnam"}]`))
	if err != nil {
		t.Fatal(err)
	}
	it, err := ToItem(ns[0])
	if err != nil {
		t.Fatal(err)
	}
	if it.Normalized.Title != "cadmium in squid rings" {
		t.Errorf("title fallback: %q", it.Normalized.Title)
	}
	if it.Normalized.ProductDescription != "squid rings (origin: Vietnam)" {
		t.Errorf("product: %q", it.Normalized.ProductDescription)
	}
}

func TestMissingReferenceIsAnError(t *testing.T) {
	if _, err := ToItem(Notification{Subject: "x"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestParseJSONRejectsGarbage(t *testing.T) {
	if _, err := ParseJSON(strings.NewReader(`<html>`)); err == nil {
		t.Fatal("expected error")
	}
}
