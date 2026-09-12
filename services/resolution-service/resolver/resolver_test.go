package resolver

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"soteria/libs/core/bus"
	"soteria/libs/core/events"
	"soteria/services/resolution-service/catalog"
)

func TestClassificationMapsFeedLabels(t *testing.T) {
	cases := map[string]string{
		"Class I":          "CLASS_I",
		"class ii":         "CLASS_II",
		"Class III":        "CLASS_III",
		"High - Class I":   "CLASS_I",
		"serious risk":     "CLASS_I",
		"alert":            "UNCLASSIFIED",
		"":                 "UNCLASSIFIED",
		"not yet assigned": "UNCLASSIFIED",
	}
	for in, want := range cases {
		if got := Classification(in); got != want {
			t.Errorf("Classification(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestResolvesOpenFDAShapedNotice exercises the real inbound contract: barcodes
// buried in the product description, lot codes in code_info, severity as a
// free-form label, and every optional field nullable.
func TestResolvesOpenFDAShapedNotice(t *testing.T) {
	b := bus.NewInMem(nil)
	store := NewStore()
	cat := catalog.NewStatic(catalog.Product{
		GTIN: "041196910537", SKU: "SF-GB-12", Brand: "Sunfield Farms",
		ProductTitle: "Sunfield Farms Chewy Granola Bars 12 ct",
		LotCodes:     []string{"8H-1132", "8H-1133", "8H-2000"},
	})
	r := New(cat, b, store, nil)

	published := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	body, err := json.Marshal(events.RecallRawReceived{
		EventID: events.NewID(), EventType: "recall.raw.received", Version: 1,
		OccurredAt: time.Now().UTC(), Producer: "ingestion-fda",
		Source: "fda_enforcement", SourceID: "F-0921-2026", PublishedAt: &published,
		Normalized: events.RecallNormalized{
			Title:              "Sunfield Farms recalls Chewy Granola Bars",
			Firm:               strptr("Sunfield Farms"),
			ProductDescription: strptr("Sunfield Farms Chewy Granola Bars 12 ct, UPC 0 41196 91053 7"),
			Reason:             strptr("undeclared peanut"),
			CodeInfo:           strptr("Lot Code: 8H-1132, 8H-1133"),
			Classification:     strptr("Class I"),
			Country:            "US",
		},
		Raw: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	env, err := events.InboundEnvelope(events.TypeRecallRawReceived, body)
	if err != nil {
		t.Fatalf("inbound envelope: %v", err)
	}
	if err := r.Handle(context.Background(), env); err != nil {
		t.Fatalf("handle: %v", err)
	}

	published_ := b.PublishedOfType(events.TypeLotResolved)
	if len(published_) != 1 {
		t.Fatalf("expected one resolution, got %d", len(published_))
	}
	var out events.LotResolved
	if err := published_[0].Into(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.IncidentID != "inc-fda_enforcement-f-0921-2026" {
		t.Errorf("incident id = %q", out.IncidentID)
	}
	if out.Scope != events.ScopeLot {
		t.Errorf("scope = %q, want LOT", out.Scope)
	}
	if out.Classification != "CLASS_I" {
		t.Errorf("classification = %q, want CLASS_I", out.Classification)
	}
	if out.Confidence < 0.85 {
		t.Errorf("confidence = %.3f, want a barcode-grade match", out.Confidence)
	}
	if got := out.Matches[0].LotCodes; len(got) != 2 {
		t.Errorf("lot codes = %v, want the two recalled lots", got)
	}
	if len(out.Allergens) != 1 || out.Allergens[0] != "peanuts" {
		t.Errorf("allergens = %v, want [peanuts]", out.Allergens)
	}
}

// TestNoticeWithNoNullableFieldsIsSafe: every optional field in the ingestion
// contract is nullable, and a notice for a product we do not carry must not error.
func TestNoticeWithNullFieldsIsSafe(t *testing.T) {
	b := bus.NewInMem(nil)
	r := New(catalog.NewStatic(), b, NewStore(), nil)
	body, err := json.Marshal(events.RecallRawReceived{
		EventID: events.NewID(), EventType: "recall.raw.received", Version: 1,
		OccurredAt: time.Now().UTC(), Producer: "ingestion-rasff",
		Source: "eu_rasff", SourceID: "2026.1234",
		Normalized: events.RecallNormalized{Title: "Salmonella in sesame seeds", Country: "EU"},
		Raw:        json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	env, err := events.InboundEnvelope(events.TypeRecallRawReceived, body)
	if err != nil {
		t.Fatalf("inbound envelope: %v", err)
	}
	if err := r.Handle(context.Background(), env); err != nil {
		t.Fatalf("a notice for an uncarried product must not fail: %v", err)
	}
	if n := len(b.PublishedOfType(events.TypeLotResolved)); n != 0 {
		t.Fatalf("expected no resolution, got %d", n)
	}
}

func strptr(s string) *string { return &s }
