package extract

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"soteria/libs/core/events"
	feedevent "soteria/libs/feedkit/event"
)

// Real notice texts (openFDA, Sept 2026).
const (
	eggsText = `FIRM: MIDWEST POULTRY SERVC C/O H & LELECTRIC
PRODUCT: Grade A White In-shell Chicken eggs packaged in the following configurations: 1. Kroger, Medium, 12 Eggs, Net Wt 21 oz (1lb 5oz) 596g, UPC 0 11110-60902 1. 2. Kroger, Medium, 30 Eggs, UPC 0 11110-60980 9. 3. Kroger, Large 12 Eggs, UPC 0 11110-60903 8.
CODES: Codes P-1950 or 0840962 with a Julian Date between 157 and 184 and a Best By/Sell By date Between July 20 - August 17, 2026
REASON: Possible Salmonella Enteritidis
DISTRIBUTION: Arkansas, Louisiana, Mississippi, New Mexico, Oklahoma, Texas`

	popsText = `FIRM: D'Dioses Fruit Pops, Inc.
PRODUCT: Ice Pop, D'Dioses Arroz con Leche, 4 oz (85 g), with UPC 710594511843
CODES: 710594511867,710594511850, 827912008456, 8279120084732, 71059511812, 827912008487
REASON: May contain undeclared milk, pecans, pistachios, yellow #5 and red #40.
DISTRIBUTION: Distributed in the following states: NJ, CT, NY, PA.`

	noodleText = `FIRM: H & U Inc. dba Sun Noodle
PRODUCT: Sura Tanmen ... Item #05410 retail package, UPC 085315054108 Item #05410.1 cardboard box (12 packages per case), UPC 085315054105
CODES: Production Lot: 1226183
REASON: Soba sauce packet, which contains bonito, sardine, and mackerel (fish), was packed into the kit but fish species are not declared
DISTRIBUTION: Distribute in HI.`
)

func TestValidateDropsInventedAndInvalidUPCs(t *testing.T) {
	ext := Extraction{
		Products:     []Product{{Name: "Ice Pop", UPCs: []string{"710594511843", "8279120084732", "71059511812", "999999999999"}}},
		Hazard:       Hazard{Type: "undeclared_allergen", Allergens: []string{"Milk", "pecans", "Pistachios"}},
		Distribution: Distribution{States: []string{"nj", "CT", "New York", "PA", "Narnia"}},
		Confidence:   0.9,
	}
	Validate(&ext, popsText)
	got := ext.AllUPCs()
	// 710594511843 valid + regex safety net finds the other valid ones in CODES.
	for _, want := range []string{"00710594511843", "00710594511867", "00710594511850", "00827912008456", "00827912008487"} {
		if !contains(got, want) {
			t.Errorf("missing %s in %v", want, got)
		}
	}
	// 13-digit code printed in the notice with a bad check digit: kept verbatim, flagged.
	if !contains(got, "8279120084732") || !hasCorrection(ext, "kept upc 8279120084732 verbatim") {
		t.Errorf("typo upc should be kept and flagged: %v %v", got, ext.Corrections)
	}
	for _, bad := range []string{"71059511812", "999999999999"} {
		if containsSuffix(got, bad) {
			t.Errorf("malformed/invented upc kept: %s", bad)
		}
	}
	if !hasCorrection(ext, "11 digits is not a GTIN") || !hasCorrection(ext, "not present in notice text") || !hasCorrection(ext, "added upcs found by regex") {
		t.Errorf("corrections: %v", ext.Corrections)
	}
	if strings.Join(ext.Hazard.Allergens, ",") != "milk,tree nuts" {
		t.Errorf("allergens: %v", ext.Hazard.Allergens)
	}
	if strings.Join(ext.Distribution.States, ",") != "NJ,CT,NY,PA" {
		t.Errorf("states: %v", ext.Distribution.States)
	}
	if ext.Confidence >= 0.9 {
		t.Errorf("confidence should be penalised for dropped identifiers: %v", ext.Confidence)
	}
}

func TestValidateAcceptsSpacedUPCsAndVerbatimLots(t *testing.T) {
	ext := Extraction{
		Products: []Product{
			{Name: "Kroger Medium 12 Eggs", UPCs: []string{"0 11110-60902 1"}, LotCodes: []string{"P-1950", "0840962", "Julian 157"}},
			{Name: "Kroger Medium 30 Eggs", UPCs: []string{"0 11110-60980 9"}},
		},
		Hazard:       Hazard{Type: "pathogen", Agent: "Salmonella Enteritidis"},
		Distribution: Distribution{States: []string{"Arkansas", "Louisiana", "Mississippi", "New Mexico", "Oklahoma", "Texas"}},
		Confidence:   0.88,
	}
	Validate(&ext, eggsText)
	if u := ext.Products[0].UPCs; u[0] != "00011110609021" {
		t.Errorf("spaced upc: %v", u)
	}
	if u := ext.Products[1].UPCs; len(u) != 1 || u[0] != "00011110609809" {
		t.Errorf("second product upc: %v", u)
	}
	if l := ext.Products[0].LotCodes; strings.Join(l, ",") != "P-1950,0840962" {
		t.Errorf("lots: %v (Julian 157 is not verbatim in the text)", l)
	}
	if !contains(ext.AllUPCs(), "00011110609038") {
		t.Errorf("regex net should add the third UPC: %v", ext.AllUPCs())
	}
	if strings.Join(ext.Distribution.States, ",") != "AR,LA,MS,NM,OK,TX" {
		t.Errorf("states: %v", ext.Distribution.States)
	}
	if ext.Hazard.Type != HazardPathogen {
		t.Errorf("hazard: %+v", ext.Hazard)
	}
}

func TestValidateFixesHazardTypeAndEmptyProducts(t *testing.T) {
	ext := Extraction{Hazard: Hazard{Type: "allergy", Allergens: []string{"bonito", "mackerel"}}, Confidence: 1.7}
	Validate(&ext, noodleText)
	if ext.Hazard.Type != HazardAllergen || strings.Join(ext.Hazard.Allergens, ",") != "fish" {
		t.Errorf("hazard: %+v", ext.Hazard)
	}
	// No products from the model; the regex net adds the check-digit-valid UPC
	// (the case code 085315054105 fails its check digit and is not guessed).
	if len(ext.Products) != 1 || strings.Join(ext.Products[0].UPCs, ",") != "00085315054108" {
		t.Errorf("products: %+v", ext.Products)
	}
	if ext.Confidence > 1 {
		t.Errorf("confidence not clamped: %v", ext.Confidence)
	}
}

func TestParseModelJSONToleratesFences(t *testing.T) {
	for _, in := range []string{
		`{"firm":"x","products":[],"hazard":{"type":"other"},"confidence":0.5}`,
		"```json\n{\"firm\":\"x\",\"products\":[],\"hazard\":{\"type\":\"other\"},\"confidence\":0.5}\n```",
		"Here is the JSON:\n{\"firm\":\"x\",\"products\":[],\"hazard\":{\"type\":\"other\"},\"confidence\":0.5}",
	} {
		ext, err := ParseModelJSON(in)
		if err != nil || ext.Firm != "x" {
			t.Errorf("%q: %v %+v", in, err, ext)
		}
	}
	if _, err := ParseModelJSON("not json at all"); err == nil {
		t.Error("expected error")
	}
}

func TestServiceHandleExtractsValidatesAndPublishes(t *testing.T) {
	fake := &Fake{Answers: map[string]Extraction{
		"Sun Noodle": {
			Firm: "H & U Inc. dba Sun Noodle", Brand: "Sun Noodle",
			Products:     []Product{{Name: "Sura Tanmen Hot and Sour Flavor", UPCs: []string{"085315054108", "085315054105"}, LotCodes: []string{"1226183"}}},
			Hazard:       Hazard{Type: "undeclared_allergen", Agent: "fish", Allergens: []string{"fish"}},
			Distribution: Distribution{States: []string{"HI"}}, Confidence: 0.92,
		},
	}}
	out := &MemOut{}
	svc := New(fake, out, nil)
	raw := rawNotice(t, "fda_enforcement", "H-1258-2026", "H & U Inc. dba Sun Noodle",
		"Sura Tanmen Hot and Sour Flavor Premium Japanese Noodles & Soup Base. Item #05410 retail package, UPC 085315054108 Item #05410.1 cardboard box, UPC 085315054105",
		"Production Lot: 1226183", "Soba sauce packet, which contains bonito, sardine, and mackerel (fish), was packed but not declared", "Distribute in HI.", "Class I")
	body, _ := json.Marshal(raw)
	env := events.Envelope{EventID: events.NewID(), EventType: "ingestion.recall.raw.received.v1", EventVersion: 1, OccurredAt: time.Now(), Producer: "ingestion-fda", Payload: body}
	if err := svc.Handle(context.Background(), env); err != nil {
		t.Fatal(err)
	}
	if len(out.Events) != 1 {
		t.Fatalf("published %d", len(out.Events))
	}
	pub := out.Events[0]
	if pub.EventType != TypeExtracted || pub.CausationID != env.EventID || pub.CorrelationID != "fda_enforcement:H-1258-2026" {
		t.Errorf("envelope: %+v", pub)
	}
	var p Extracted
	if err := json.Unmarshal(pub.Payload, &p); err != nil {
		t.Fatal(err)
	}
	if p.Source != "fda_enforcement" || p.SourceID != "H-1258-2026" || p.RawEventID != env.EventID || p.Classification != "Class I" || p.Model != "fake" {
		t.Errorf("payload: %+v", p.Extraction)
	}
	// The model returned both codes; the case code has an invalid check digit in the real notice → kept verbatim.
	if strings.Join(p.AllUPCs(), ",") != "00085315054108,085315054105" || strings.Join(p.AllLots(), ",") != "1226183" {
		t.Errorf("ids: %v %v", p.AllUPCs(), p.AllLots())
	}
	if !hasCorrection(p.Extraction, "kept upc 085315054105 verbatim") {
		t.Errorf("corrections: %v", p.Corrections)
	}
	if !strings.Contains(p.Text, "PRODUCT: Sura Tanmen") || !strings.Contains(p.Text, "CODES: Production Lot") {
		t.Errorf("document text: %q", p.Text)
	}
	st := svc.Stats()
	if st.Extracted != 1 || st.Failed != 0 {
		t.Errorf("stats: %+v", st)
	}
}

func TestServiceModelFailureIsRetriedByBus(t *testing.T) {
	fake := &Fake{Err: errors.New("groq: HTTP 503")}
	svc := New(fake, &MemOut{}, nil)
	raw := rawNotice(t, "fda_press", "http://x/notice", "", "Brand X recalls granola bars because of undeclared peanuts", "", "", "", "")
	body, _ := json.Marshal(raw)
	err := svc.Handle(context.Background(), events.Envelope{EventID: events.NewID(), EventType: "ingestion.recall.raw.received.v1", Payload: body})
	if err == nil {
		t.Fatal("model failure must return an error so the bus retries")
	}
	if st := svc.Stats(); st.Failed != 1 || !strings.Contains(st.LastError, "503") {
		t.Errorf("stats: %+v", st)
	}
}

func TestCacheAvoidsRepeatCalls(t *testing.T) {
	fake := &Fake{Answers: map[string]Extraction{"granola": {Products: []Product{{Name: "granola"}}, Hazard: Hazard{Type: "other"}, Confidence: 0.5}}}
	c, err := OpenCache(":memory:", fake)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for i := 0; i < 3; i++ {
		if _, err := c.Extract(context.Background(), "Brand X granola recall"); err != nil {
			t.Fatal(err)
		}
	}
	if fake.Calls != 1 {
		t.Fatalf("model called %d times; cache not used", fake.Calls)
	}
	c.ReadOnly = true
	if _, err := c.Extract(context.Background(), "something never seen"); err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Errorf("read-only miss should error: %v", err)
	}
}

// ---- helpers ------------------------------------------------------------------

func rawNotice(t *testing.T, src, id, firm, product, codes, reason, dist, class string) feedevent.Event {
	t.Helper()
	ptr := func(s string) *string {
		if s == "" {
			return nil
		}
		return &s
	}
	return feedevent.Event{
		EventID: events.NewID(), EventType: "recall.raw.received", Version: 1, OccurredAt: time.Now(), Producer: "ingestion-test",
		Source: src, SourceID: id,
		Normalized: feedevent.Normalized{Title: product, Firm: ptr(firm), ProductDescription: ptr(product), CodeInfo: ptr(codes), Reason: ptr(reason), Distribution: ptr(dist), Classification: ptr(class), Country: "US"},
		Raw:        json.RawMessage(`{"recall_number":"` + id + `","status":"Ongoing","city":"Honolulu","state":"HI","description":"` + product + `"}`),
	}
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

func containsSuffix(xs []string, suffix string) bool {
	for _, y := range xs {
		if strings.HasSuffix(y, suffix) {
			return true
		}
	}
	return false
}

func hasCorrection(e Extraction, sub string) bool {
	for _, c := range e.Corrections {
		if strings.Contains(c, sub) {
			return true
		}
	}
	return false
}
