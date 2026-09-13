package hold

import (
	"context"
	"strings"
	"testing"

	core "soteria/libs/core/shopify"
	"soteria/libs/shopify"
	"soteria/libs/shopify/fake"
)

const (
	selling    = "gid://shopify/Location/1"
	quarantine = "gid://shopify/Location/2"
	product    = "gid://shopify/Product/1"
	variantID  = "gid://shopify/ProductVariant/1"
	item       = "gid://shopify/InventoryItem/1"
)

func seeded(t *testing.T) (*fake.Store, *Adapter) {
	t.Helper()
	s := fake.New()
	s.AddLocation(shopify.Location{ID: selling, Name: "Main Warehouse", IsActive: true, Fulfills: true})
	s.AddLocation(shopify.Location{ID: quarantine, Name: "Quarantine", IsActive: true})
	s.AddVariant(shopify.Variant{
		ID: variantID, ProductID: product, ProductTitle: "Sunfield Farms Chewy Granola Bars 12 ct", ProductStatus: shopify.StatusActive,
		SKU: "SF-GB-12", Barcode: "041196910537", InventoryItemID: item,
		Inventory: []shopify.InventoryLevel{{LocationID: selling, Available: 100}},
		Lots:      []shopify.Lot{{Code: "8H-1132", Units: 40}, {Code: "8H-1133", Units: 35}, {Code: "8H-2000", Units: 25}},
	})
	s.AddVariant(shopify.Variant{
		ID: "gid://shopify/ProductVariant/2", ProductID: "gid://shopify/Product/2", ProductTitle: "Nutty Trail Peanut Crunch", ProductStatus: shopify.StatusActive,
		SKU: "NT-GB-12", Barcode: "073123456788", InventoryItemID: "gid://shopify/InventoryItem/2",
		Inventory: []shopify.InventoryLevel{{LocationID: selling, Available: 12}},
	})
	sel, q, err := ResolveLocations(context.Background(), s, "Quarantine")
	if err != nil || sel != selling || q != quarantine {
		t.Fatalf("ResolveLocations: %s %s %v", sel, q, err)
	}
	return s, New(s, sel, q)
}

func TestPartialHoldKeepsSafeLotsSellable(t *testing.T) {
	s, a := seeded(t)
	ctx := context.Background()

	resp, err := a.HoldLots(ctx, core.HoldRequest{GTIN: "041196910537", LotCodes: []string{" 8h-1132 ", "8H-1133"}, Reason: "Listeria"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.UnitsHeld != 75 || resp.UnitsLeftSellable != 25 || resp.PlatformRef != item {
		t.Fatalf("resp: %+v", resp)
	}
	v, _ := s.GetVariant(variantID)
	if v.AvailableAt(selling) != 25 || v.AvailableAt(quarantine) != 75 {
		t.Errorf("inventory: %+v", v.Inventory)
	}
	if !v.Lots[0].Held || !v.Lots[1].Held || v.Lots[2].Held {
		t.Errorf("ledger: %+v", v.Lots)
	}
	if v.ProductStatus != shopify.StatusActive {
		t.Error("partial hold must not unpublish the product")
	}
	if strings.Join(v.Tags, ",") != "RECALL_LOT:8H-1132,RECALL_LOT:8H-1133" {
		t.Errorf("tags: %v", v.Tags)
	}
	if mf, _ := s.GetMetafield(product, "soteria", "badge"); mf.Value != BadgeSafeLot {
		t.Errorf("badge: %q", mf.Value)
	}

	// Idempotent: holding the same lots again moves nothing.
	resp, err = a.HoldLots(ctx, core.HoldRequest{GTIN: "041196910537", LotCodes: []string{"8H-1132"}})
	if err != nil || resp.UnitsHeld != 0 || resp.UnitsLeftSellable != 25 {
		t.Fatalf("idempotent hold: %+v %v", resp, err)
	}

	// Holding the last lot completes the SKU: hazard tag + DRAFT.
	resp, err = a.HoldLots(ctx, core.HoldRequest{GTIN: "041196910537", LotCodes: []string{"8H-2000"}})
	if err != nil || resp.UnitsHeld != 25 || resp.UnitsLeftSellable != 0 {
		t.Fatalf("final hold: %+v %v", resp, err)
	}
	v, _ = s.GetVariant(variantID)
	if v.ProductStatus != shopify.StatusDraft || !contains(v.Tags, TagHazard) {
		t.Errorf("full hold state: status=%s tags=%v", v.ProductStatus, v.Tags)
	}

	// Release two lots: product republished? No — one lot still held.
	resp, err = a.ReleaseLots(ctx, core.HoldRequest{GTIN: "041196910537", LotCodes: []string{"8H-1132", "8H-1133"}})
	if err != nil || resp.UnitsHeld != 75 || resp.UnitsLeftSellable != 75 {
		t.Fatalf("release: %+v %v", resp, err)
	}
	v, _ = s.GetVariant(variantID)
	if v.ProductStatus != shopify.StatusDraft || contains(v.Tags, "RECALL_LOT:8H-1132") || !contains(v.Tags, "RECALL_LOT:8H-2000") {
		t.Errorf("partial release: status=%s tags=%v", v.ProductStatus, v.Tags)
	}
	// Release the last: back to ACTIVE, no hazard tag, badge cleared.
	if _, err := a.ReleaseLots(ctx, core.HoldRequest{GTIN: "041196910537", LotCodes: []string{"8H-2000"}}); err != nil {
		t.Fatal(err)
	}
	v, _ = s.GetVariant(variantID)
	if v.ProductStatus != shopify.StatusActive || len(v.Tags) != 0 || v.AvailableAt(selling) != 100 || v.AvailableAt(quarantine) != 0 {
		t.Errorf("full release: %+v tags=%v", v.Inventory, v.Tags)
	}
	if mf, _ := s.GetMetafield(product, "soteria", "badge"); mf.Value != "" {
		t.Errorf("badge should be cleared, got %q", mf.Value)
	}
}

func TestWholeSKUHoldWhenNoLotsGiven(t *testing.T) {
	s, a := seeded(t)
	resp, err := a.HoldLots(context.Background(), core.HoldRequest{GTIN: "041196910537"})
	if err != nil || resp.UnitsHeld != 100 || resp.UnitsLeftSellable != 0 {
		t.Fatalf("resp: %+v %v", resp, err)
	}
	v, _ := s.GetVariant(variantID)
	if v.ProductStatus != shopify.StatusDraft || v.AvailableAt(quarantine) != 100 {
		t.Errorf("state: %+v", v)
	}
	for _, l := range v.Lots {
		if !l.Held {
			t.Errorf("all lots should be held: %+v", v.Lots)
		}
	}
}

func TestVariantWithoutLedgerIsHeldWhole(t *testing.T) {
	s, a := seeded(t)
	resp, err := a.HoldLots(context.Background(), core.HoldRequest{GTIN: "073123456788", LotCodes: []string{"P908"}})
	if err != nil || resp.UnitsHeld != 12 || resp.UnitsLeftSellable != 0 {
		t.Fatalf("resp: %+v %v", resp, err)
	}
	v, _ := s.GetVariant("gid://shopify/ProductVariant/2")
	if v.ProductStatus != shopify.StatusDraft || v.AvailableAt(quarantine) != 12 {
		t.Errorf("state: %+v", v)
	}
	resp, err = a.ReleaseLots(context.Background(), core.HoldRequest{GTIN: "073123456788"})
	if err != nil || resp.UnitsHeld != 12 || resp.UnitsLeftSellable != 12 {
		t.Fatalf("release: %+v %v", resp, err)
	}
	v, _ = s.GetVariant("gid://shopify/ProductVariant/2")
	if v.ProductStatus != shopify.StatusActive || v.AvailableAt(selling) != 12 {
		t.Errorf("released: %+v", v)
	}
}

func TestUnknownLotAndUnknownProduct(t *testing.T) {
	_, a := seeded(t)
	if _, err := a.HoldLots(context.Background(), core.HoldRequest{GTIN: "041196910537", LotCodes: []string{"NOPE"}}); err == nil {
		t.Error("unknown lot must error rather than silently hold nothing")
	}
	if _, err := a.HoldLots(context.Background(), core.HoldRequest{GTIN: "000000000000"}); err == nil {
		t.Error("unknown GTIN must error")
	}
	// SKU fallback when the barcode is unknown to the store.
	resp, err := a.HoldLots(context.Background(), core.HoldRequest{GTIN: "000000000000", SKU: "nt-gb-12"})
	if err != nil || resp.UnitsHeld != 12 {
		t.Errorf("sku fallback: %+v %v", resp, err)
	}
}

func TestLedgerCapsAtPhysicalStock(t *testing.T) {
	s, a := seeded(t)
	// Ledger says 100 units across lots, but only 60 are physically available.
	s.SetAvailable(context.Background(), item, selling, 60, "correction")
	resp, err := a.HoldLots(context.Background(), core.HoldRequest{GTIN: "041196910537"})
	if err != nil || resp.UnitsHeld != 60 || resp.UnitsLeftSellable != 0 {
		t.Fatalf("resp: %+v %v", resp, err)
	}
}

func TestFailureLeavesLedgerUntouched(t *testing.T) {
	s, a := seeded(t)
	s.Fail["MoveAvailable"] = &shopify.UserErrors{Mutation: "inventoryMoveQuantities", Errors: []shopify.UserError{{Message: "boom"}}}
	if _, err := a.HoldLots(context.Background(), core.HoldRequest{GTIN: "041196910537", LotCodes: []string{"8H-1132"}}); err == nil {
		t.Fatal("expected error")
	}
	v, _ := s.GetVariant(variantID)
	if v.Lots[0].Held || len(v.Tags) != 0 || v.AvailableAt(selling) != 100 {
		t.Errorf("state changed despite failed move: %+v", v)
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
