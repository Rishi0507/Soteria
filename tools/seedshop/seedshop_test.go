package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"soteria/libs/shopify"
	"soteria/libs/shopify/fake"
)

// ---------------------------------------------------------------- generate

func TestFirstValidGTINIgnoresNumbersThatAreNotBarcodes(t *testing.T) {
	cases := []struct{ text, want string }{
		{"Sun Noodle ramen, UPC 0 85315 05410 8, 12 oz", "085315054108"},
		{"12 cases of 24 count, net weight 1,272 lbs", ""}, // counts and weights
		{"call 888-674-6854 for questions", ""},            // a phone number
		{"UPC 123456789013 (mistyped)", ""},                // fails the check digit
		{"", ""},
	}
	for _, c := range cases {
		if got := firstValidGTIN(c.text); got != c.want {
			t.Errorf("firstValidGTIN(%q) = %q, want %q", c.text, got, c.want)
		}
	}
}

// TestToSeedProductNeedsBothIdentifiers: a recall with no barcode can never be
// matched, and one with no lot code cannot demonstrate surgical containment.
// Either way the record is skipped rather than patched up with invented data.
func TestToSeedProductNeedsBothIdentifiers(t *testing.T) {
	full := enforcementRecord{
		RecallNumber: "H-1258-2026", Classification: "Class I",
		ProductDescription: "Sura Tanmen Hot and Sour ramen, UPC 0 85315 05410 8",
		CodeInfo:           "Production Lot: 1226183",
		ReasonForRecall:    "undeclared fish",
		RecallingFirm:      "H & U Inc. dba Sun Noodle",
	}
	p, ok := toSeedProduct(full)
	if !ok {
		t.Fatal("a record with a barcode and a lot should be usable")
	}
	if p.Barcode != "085315054108" {
		t.Errorf("barcode = %q", p.Barcode)
	}
	if p.Lots[0].Code != "1226183" {
		t.Errorf("first lot = %q, want the recalled lot", p.Lots[0].Code)
	}
	if len(p.Lots) < 2 {
		t.Error("needs clean lots beside the recalled one, or there is nothing to keep selling")
	}
	if !strings.Contains(p.Provenance, "H-1258-2026") {
		t.Errorf("provenance must name the recall, got %q", p.Provenance)
	}
	if err := p.Validate(); err != nil {
		t.Errorf("generated product should be valid: %v", err)
	}

	noBarcode := full
	noBarcode.ProductDescription = "Ramen noodles, 12 oz"
	if _, ok := toSeedProduct(noBarcode); ok {
		t.Error("a record with no barcode must be skipped")
	}

	noLot := full
	noLot.CodeInfo = ""
	if _, ok := toSeedProduct(noLot); ok {
		t.Error("a record with no lot code must be skipped")
	}
}

func TestSeedProductValidateRejectsWhatBreaksContainment(t *testing.T) {
	valid := SeedProduct{Title: "X", Barcode: "085315054108", Lots: []shopify.Lot{{Code: "A1", Units: 10}}}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid product rejected: %v", err)
	}

	bad := []struct {
		name string
		p    SeedProduct
	}{
		{"no title", SeedProduct{Barcode: "085315054108", Lots: valid.Lots}},
		{"made-up barcode", SeedProduct{Title: "X", Barcode: "123456789013", Lots: valid.Lots}},
		{"blank barcode", SeedProduct{Title: "X", Lots: valid.Lots}},
		{"no lots", SeedProduct{Title: "X", Barcode: "085315054108"}},
		{"duplicate lot", SeedProduct{Title: "X", Barcode: "085315054108",
			Lots: []shopify.Lot{{Code: "A1", Units: 5}, {Code: "a1", Units: 5}}}},
	}
	for _, c := range bad {
		if err := c.p.Validate(); err == nil {
			t.Errorf("%s: expected an error", c.name)
		}
	}
}

// ---------------------------------------------------------------- verify

func shopWith(t *testing.T, quarantine bool, variants ...shopify.Variant) *fake.Store {
	t.Helper()
	s := fake.New()
	s.AddLocation(shopify.Location{ID: "gid://shopify/Location/1", Name: "Main", IsActive: true, Fulfills: true})
	if quarantine {
		s.AddLocation(shopify.Location{ID: "gid://shopify/Location/2", Name: "Quarantine", IsActive: true})
	}
	for _, v := range variants {
		s.AddVariant(v)
	}
	return s
}

func variant(barcode string, stock int, lots ...shopify.Lot) shopify.Variant {
	return shopify.Variant{
		ID: "gid://shopify/ProductVariant/" + barcode, ProductID: "gid://shopify/Product/" + barcode,
		ProductTitle: "Product " + barcode, Barcode: barcode, SKU: "SKU-" + barcode,
		InventoryItemID: "gid://shopify/InventoryItem/" + barcode,
		Inventory:       []shopify.InventoryLevel{{LocationID: "gid://shopify/Location/1", Available: stock}},
		Lots:            lots,
	}
}

func TestVerifyReadyStore(t *testing.T) {
	s := shopWith(t, true, variant("085315054108", 100,
		shopify.Lot{Code: "1226183", Units: 40}, shopify.Lot{Code: "CLEAN-A", Units: 60}))

	rep, err := Verify(context.Background(), s)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !rep.Ready() {
		t.Fatalf("expected ready, problems: %v", rep.Problems)
	}
	if rep.WithBarcode != 1 || rep.WithLots != 1 {
		t.Errorf("counts = %+v", rep)
	}
}

// TestVerifyMissingQuarantineIsFatal: without the location a hold has nowhere to
// move stock, so containment cannot run at all.
func TestVerifyMissingQuarantineIsFatal(t *testing.T) {
	s := shopWith(t, false, variant("085315054108", 100, shopify.Lot{Code: "1226183", Units: 100}))
	rep, _ := Verify(context.Background(), s)
	if rep.Ready() {
		t.Fatal("a store with no Quarantine location is not ready")
	}
	if !strings.Contains(strings.Join(rep.Problems, " "), "Quarantine") {
		t.Errorf("problem must name the missing location: %v", rep.Problems)
	}
}

func TestVerifyReportsTheThreeSilentFailures(t *testing.T) {
	s := shopWith(t, true,
		variant("", 50),             // no barcode
		variant("860864000307", 50), // no lot ledger
		variant("085315054108", 100, shopify.Lot{Code: "1226183", Units: 40}), // ledger drift: 40 vs 100
	)
	rep, _ := Verify(context.Background(), s)
	joined := strings.Join(rep.Problems, " | ")

	for _, want := range []string{"no valid barcode", "ledger", "disagrees with stock"} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected a problem mentioning %q, got: %s", want, joined)
		}
	}
	if rep.WithBarcode != 2 {
		t.Errorf("WithBarcode = %d, want 2", rep.WithBarcode)
	}
}

// ---------------------------------------------------------------- apply

// stubStore records the mutations Apply sends, so idempotency can be asserted
// without a live shop.
type stubStore struct {
	*fake.Store
	calls      []string
	metafields []shopify.Metafield
}

func (s *stubStore) Do(ctx context.Context, query string, vars map[string]any, out any) error {
	switch {
	case strings.Contains(query, "locationAdd"):
		s.calls = append(s.calls, "locationAdd")
		return json.Unmarshal([]byte(`{"locationAdd":{"location":{"id":"gid://shopify/Location/99","name":"Quarantine"}}}`), out)
	case strings.Contains(query, "metafieldDefinitionCreate"):
		s.calls = append(s.calls, "metafieldDefinitionCreate")
		return json.Unmarshal([]byte(`{"metafieldDefinitionCreate":{}}`), out)
	case strings.Contains(query, "productCreate"):
		s.calls = append(s.calls, "productCreate")
		return json.Unmarshal([]byte(`{"productCreate":{"product":{"id":"gid://shopify/Product/9","variants":{"nodes":[{"id":"gid://shopify/ProductVariant/9","inventoryItem":{"id":"gid://shopify/InventoryItem/9"}}]}}}}`), out)
	case strings.Contains(query, "productVariantsBulkUpdate"):
		s.calls = append(s.calls, "variantsUpdate")
		return json.Unmarshal([]byte(`{"productVariantsBulkUpdate":{}}`), out)
	case strings.Contains(query, "inventoryActivate"):
		s.calls = append(s.calls, "inventoryActivate")
		return json.Unmarshal([]byte(`{"inventoryActivate":{}}`), out)
	}
	return nil
}

func (s *stubStore) SetMetafields(ctx context.Context, fields []shopify.Metafield) error {
	s.calls = append(s.calls, "setMetafields")
	s.metafields = append(s.metafields, fields...)
	return nil
}

func (s *stubStore) has(call string) bool {
	for _, c := range s.calls {
		if c == call {
			return true
		}
	}
	return false
}

func seedOf(p ...SeedProduct) Seed { return Seed{Products: p} }

func TestApplyCreatesWhatIsMissing(t *testing.T) {
	s := &stubStore{Store: shopWith(t, false)} // no quarantine, no products
	seed := seedOf(SeedProduct{
		Title: "Sun Noodle ramen", Barcode: "085315054108", SKU: "SOT-054108", Price: "4.99",
		Lots: []shopify.Lot{{Code: "1226183", Units: 40}, {Code: "CLEAN-A", Units: 60}},
	})

	if err := Apply(context.Background(), s, seed, false); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	for _, want := range []string{"locationAdd", "metafieldDefinitionCreate", "productCreate", "variantsUpdate", "inventoryActivate", "setMetafields"} {
		if !s.has(want) {
			t.Errorf("expected %s, calls were %v", want, s.calls)
		}
	}

	// The ledger written must be the one from the seed, and stock must match it.
	var lots []shopify.Lot
	if err := json.Unmarshal([]byte(s.metafields[0].Value), &lots); err != nil {
		t.Fatalf("lots metafield is not JSON: %v", err)
	}
	if len(lots) != 2 || lots[0].Code != "1226183" {
		t.Errorf("lots written = %+v", lots)
	}
	if s.metafields[0].Key != shopify.LotsKey || s.metafields[0].Namespace != shopify.MetafieldNamespace {
		t.Errorf("metafield target = %s.%s", s.metafields[0].Namespace, s.metafields[0].Key)
	}
}

// TestApplyIsIdempotent: re-running must not duplicate the location or the
// product, because a partial failure has to be safe to retry.
func TestApplyIsIdempotent(t *testing.T) {
	s := &stubStore{Store: shopWith(t, true, variant("085315054108", 100, shopify.Lot{Code: "1226183", Units: 100}))}
	seed := seedOf(SeedProduct{
		Title: "Sun Noodle ramen", Barcode: "085315054108", SKU: "SOT-054108", Price: "4.99",
		Lots: []shopify.Lot{{Code: "1226183", Units: 40}, {Code: "CLEAN-A", Units: 60}},
	})

	if err := Apply(context.Background(), s, seed, false); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if s.has("locationAdd") {
		t.Error("Quarantine already existed; it must not be created again")
	}
	if s.has("productCreate") {
		t.Error("the barcode already existed; the product must not be created again")
	}
	if !s.has("setMetafields") {
		t.Error("the lot ledger should still be refreshed on an existing variant")
	}
}

func TestApplyDryRunTouchesNothing(t *testing.T) {
	s := &stubStore{Store: shopWith(t, false)}
	seed := seedOf(SeedProduct{
		Title: "Sun Noodle ramen", Barcode: "085315054108",
		Lots: []shopify.Lot{{Code: "1226183", Units: 40}},
	})

	if err := Apply(context.Background(), s, seed, true); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(s.calls) != 0 {
		t.Fatalf("dry run performed %v", s.calls)
	}
}

// TestApplyRefusesAShopWithNoSellingLocation: stock has to move from somewhere.
func TestApplyRefusesAShopWithNoSellingLocation(t *testing.T) {
	empty := fake.New()
	empty.AddLocation(shopify.Location{ID: "gid://shopify/Location/2", Name: "Quarantine", IsActive: true})
	s := &stubStore{Store: empty}

	err := Apply(context.Background(), s, seedOf(SeedProduct{
		Title: "X", Barcode: "085315054108", Lots: []shopify.Lot{{Code: "A", Units: 1}},
	}), false)
	if err == nil {
		t.Fatal("expected an error when no location fulfils online orders")
	}
}
