package hold

import (
	"context"
	"testing"

	core "soteria/libs/core/shopify"
	"soteria/libs/shopify"
	"soteria/libs/shopify/fake"
)

func TestBarcodeFormsCoversEveryEquivalentWidth(t *testing.T) {
	got := barcodeForms("00041196910537")
	want := map[string]bool{"00041196910537": true, "0041196910537": true, "041196910537": true}
	for w := range want {
		found := false
		for _, g := range got {
			if g == w {
				found = true
			}
		}
		if !found {
			t.Errorf("missing form %q in %v", w, got)
		}
	}

	// A significant digit may never be dropped: that would be another product.
	for _, f := range barcodeForms("10041196910537") {
		if f == "041196910537" {
			t.Errorf("dropped a significant leading digit: %v", barcodeForms("10041196910537"))
		}
	}

	if got := barcodeForms(""); len(got) != 1 || got[0] != "" {
		t.Errorf("barcodeForms(\"\") = %v", got)
	}
}

// TestHoldFindsAPrintedUPCFromANormalizedGTIN is the regression for the seam
// between containment and the store: containment normalizes to GTIN-14, while a
// merchant types the 12-digit UPC printed on the pack. With no SKU to fall back
// on, asking for a single spelling found nothing and the hold failed outright.
func TestHoldFindsAPrintedUPCFromANormalizedGTIN(t *testing.T) {
	s := fake.New()
	s.AddLocation(shopify.Location{ID: "l1", Name: "Main", IsActive: true, Fulfills: true})
	s.AddLocation(shopify.Location{ID: "l2", Name: "Quarantine", IsActive: true})
	s.AddVariant(shopify.Variant{
		ID: "v1", ProductID: "p1", ProductStatus: shopify.StatusActive,
		Barcode: "041196910537", InventoryItemID: "i1", // printed UPC-A, no SKU
		Inventory: []shopify.InventoryLevel{{LocationID: "l1", Available: 100}},
		Lots:      []shopify.Lot{{Code: "8H-1132", Units: 40}, {Code: "CLEAN", Units: 60}},
	})
	selling, quarantine, err := ResolveLocations(context.Background(), s, "Quarantine")
	if err != nil {
		t.Fatalf("ResolveLocations: %v", err)
	}

	resp, err := New(s, selling, quarantine).HoldLots(context.Background(), core.HoldRequest{
		GTIN: "00041196910537", LotCodes: []string{"8H-1132"},
	})
	if err != nil {
		t.Fatalf("hold by normalized GTIN against a printed UPC: %v", err)
	}
	if resp.UnitsHeld != 40 {
		t.Errorf("units held = %d, want 40", resp.UnitsHeld)
	}
	if resp.UnitsLeftSellable != 60 {
		t.Errorf("units left sellable = %d, want 60", resp.UnitsLeftSellable)
	}

	v, _ := s.GetVariant("v1")
	if v.AvailableAt("l2") != 40 {
		t.Errorf("quarantine holds %d units, want 40", v.AvailableAt("l2"))
	}
	if v.ProductStatus != shopify.StatusActive {
		t.Errorf("a partial hold must not unpublish the product, status = %q", v.ProductStatus)
	}
}

// TestHoldStillWorksWhenTheStoreUsesGTIN14: the fix must not break stores that
// happen to type the padded form.
func TestHoldStillWorksWhenTheStoreUsesGTIN14(t *testing.T) {
	s := fake.New()
	s.AddLocation(shopify.Location{ID: "l1", Name: "Main", IsActive: true, Fulfills: true})
	s.AddLocation(shopify.Location{ID: "l2", Name: "Quarantine", IsActive: true})
	s.AddVariant(shopify.Variant{
		ID: "v1", ProductID: "p1", ProductStatus: shopify.StatusActive,
		Barcode: "00041196910537", InventoryItemID: "i1",
		Inventory: []shopify.InventoryLevel{{LocationID: "l1", Available: 50}},
		Lots:      []shopify.Lot{{Code: "L1", Units: 50}},
	})
	selling, quarantine, _ := ResolveLocations(context.Background(), s, "Quarantine")

	if _, err := New(s, selling, quarantine).HoldLots(context.Background(), core.HoldRequest{
		GTIN: "041196910537", LotCodes: []string{"L1"},
	}); err != nil {
		t.Fatalf("hold by printed UPC against a GTIN-14 barcode: %v", err)
	}
}
