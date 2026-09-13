package fdarss

import (
	"os"
	"strings"
	"testing"
	"time"

	"soteria/libs/feedkit/event"
)

func TestParseRecordedFeed(t *testing.T) {
	b, err := os.ReadFile("testdata/recalls.xml")
	if err != nil {
		t.Fatal(err)
	}
	items, err := Parse(b, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 20 {
		t.Fatalf("want 20 items got %d", len(items))
	}
	first := items[0]
	if !strings.HasPrefix(first.SourceID, "http://www.fda.gov/safety/recalls-market-withdrawals-safety-alerts/") {
		t.Errorf("source_id should be the guid: %q", first.SourceID)
	}
	if first.Normalized.Title != "Whole Foods Market Issues Allergy Alert on Undeclared Egg in Cabricharme Cheese" {
		t.Errorf("title: %q", first.Normalized.Title)
	}
	// "Fri, 11 Sep 2026 18:15:00 EDT" → 22:15 UTC
	want := time.Date(2026, 9, 11, 22, 15, 0, 0, time.UTC)
	if first.PublishedAt == nil || !first.PublishedAt.Equal(want) {
		t.Errorf("published_at: got %v want %v", first.PublishedAt, want)
	}
	if !strings.Contains(first.Normalized.ProductDescription, "egg lysozyme") {
		t.Errorf("description: %q", first.Normalized.ProductDescription)
	}
	for _, it := range items {
		ev := event.New("ingestion-fda", "fda_press", it, time.Now())
		if err := ev.Validate(); err != nil {
			t.Errorf("%s: %v", it.SourceID, err)
		}
	}
}

func TestParseFiltersBySince(t *testing.T) {
	b, _ := os.ReadFile("testdata/recalls.xml")
	all, _ := Parse(b, time.Time{})
	recent, err := Parse(b, time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) == 0 || len(recent) >= len(all) {
		t.Fatalf("since filter ineffective: all=%d recent=%d", len(all), len(recent))
	}
	for _, it := range recent {
		if it.PublishedAt.Before(time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)) {
			t.Errorf("item before since leaked: %v", it.PublishedAt)
		}
	}
}

func TestParsePubDateZones(t *testing.T) {
	cases := map[string]time.Time{
		"Fri, 11 Sep 2026 18:15:00 EDT":   time.Date(2026, 9, 11, 22, 15, 0, 0, time.UTC),
		"Mon, 12 Jan 2026 09:00:00 EST":   time.Date(2026, 1, 12, 14, 0, 0, 0, time.UTC),
		"Mon, 12 Jan 2026 09:00:00 -0500": time.Date(2026, 1, 12, 14, 0, 0, 0, time.UTC),
		"Mon, 12 Jan 2026 09:00:00 GMT":   time.Date(2026, 1, 12, 9, 0, 0, 0, time.UTC),
	}
	for in, want := range cases {
		got := parsePubDate(in)
		if got == nil || !got.Equal(want) {
			t.Errorf("%q: got %v want %v", in, got, want)
		}
	}
	if parsePubDate("garbage") != nil {
		t.Error("garbage should yield nil")
	}
}
