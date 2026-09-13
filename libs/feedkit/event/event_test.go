package event

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"soteria/libs/feedkit/source"
)

func sample() source.Item {
	pub := time.Date(2026, 9, 2, 0, 0, 0, 0, time.FixedZone("EDT", -4*3600))
	return source.Item{
		SourceID:    "H-1258-2026",
		SourceURL:   "https://example.test/notice",
		PublishedAt: &pub,
		Normalized:  source.Normalized{Title: "Sura Tanmen", Firm: "Sun Noodle", Classification: "Class I", Country: "US"},
		Raw:         json.RawMessage(`{"recall_number":"H-1258-2026"}`),
	}
}

func TestNewProducesValidContractShape(t *testing.T) {
	now := time.Date(2026, 9, 12, 7, 0, 0, 0, time.UTC)
	ev := New("ingestion-fda", source.FDAEnforcement, sample(), now)
	body, err := ev.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatal(err)
	}
	// Optional strings are emitted as explicit nulls, not omitted.
	n := m["normalized"].(map[string]any)
	for _, k := range []string{"firm", "product_description", "reason", "code_info", "classification", "distribution"} {
		if _, present := n[k]; !present {
			t.Errorf("normalized.%s missing (should be null when empty)", k)
		}
	}
	if n["reason"] != nil {
		t.Errorf("reason should be null, got %v", n["reason"])
	}
	if got := m["published_at"]; got != "2026-09-02T04:00:00Z" {
		t.Errorf("published_at should be UTC RFC3339, got %v", got)
	}
	if m["event_type"] != Type || m["version"].(float64) != Version {
		t.Errorf("bad type/version: %v %v", m["event_type"], m["version"])
	}
}

func TestValidateRejectsBadEvents(t *testing.T) {
	now := time.Now()
	cases := map[string]func(*Event){
		"unknown source":  func(e *Event) { e.Source = "cdc" },
		"empty source_id": func(e *Event) { e.SourceID = "" },
		"empty title":     func(e *Event) { e.Normalized.Title = "" },
		"bad country":     func(e *Event) { e.Normalized.Country = "UK" },
		"raw not object":  func(e *Event) { e.Raw = json.RawMessage(`[1,2]`) },
		"raw invalid":     func(e *Event) { e.Raw = json.RawMessage(`{`) },
		"bad uuid":        func(e *Event) { e.EventID = "nope" },
		"wrong version":   func(e *Event) { e.Version = 2 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			ev := New("ingestion-fda", source.FDAEnforcement, sample(), now)
			mutate(&ev)
			if err := ev.Validate(); err == nil {
				t.Fatal("expected validation error")
			} else if !strings.Contains(err.Error(), ":") {
				t.Fatalf("error should name the field: %v", err)
			}
		})
	}
}
