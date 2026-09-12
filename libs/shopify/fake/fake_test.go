package fake

import (
	"context"
	"errors"
	"testing"
	"time"

	"soteria/libs/shopify"
)

const (
	main_      = "gid://shopify/Location/1"
	quarantine = "gid://shopify/Location/2"
)

func seeded() *Store {
	s := New()
	s.AddLocation(shopify.Location{ID: main_, Name: "Main Warehouse", IsActive: true, Fulfills: true})
	s.AddLocation(shopify.Location{ID: quarantine, Name: "Quarantine", IsActive: true})
	s.AddVariant(shopify.Variant{
		ID: "gid://shopify/ProductVariant/1", ProductID: "gid://shopify/Product/1", ProductTitle: "Brand X Peanut Butter", ProductStatus: shopify.StatusActive,
		Title: "16 oz", SKU: "PB-16", Barcode: "012345678905", InventoryItemID: "gid://shopify/InventoryItem/1",
		Inventory: []shopify.InventoryLevel{{LocationID: main_, Available: 50}},
	})
	s.AddVariant(shopify.Variant{
		ID: "gid://shopify/ProductVariant/2", ProductID: "gid://shopify/Product/2", ProductTitle: "Brand Y Almond Butter", ProductStatus: shopify.StatusActive,
		Title: "16 oz", SKU: "AB-16", Barcode: "012345678912", InventoryItemID: "gid://shopify/InventoryItem/2",
		Inventory: []shopify.InventoryLevel{{LocationID: main_, Available: 20}},
	})
	s.AddOrder(shopify.Order{
		ID: "gid://shopify/Order/10", Name: "#1001", CreatedAt: time.Now().Add(-24 * time.Hour), Email: "a@b.c", FulfillmentStatus: "UNFULFILLED",
		LineItems: []shopify.LineItem{{ID: "gid://shopify/LineItem/100", VariantID: "gid://shopify/ProductVariant/1", SKU: "PB-16", Quantity: 3}},
	})
	s.AddOrder(shopify.Order{
		ID: "gid://shopify/Order/11", Name: "#0900", CreatedAt: time.Now().Add(-30 * 24 * time.Hour),
		LineItems: []shopify.LineItem{{ID: "gid://shopify/LineItem/200", VariantID: "gid://shopify/ProductVariant/2", Quantity: 1}},
	})
	return s
}

func TestQuarantineFlow(t *testing.T) {
	s := seeded()
	ctx := context.Background()

	vs, err := s.VariantsByBarcode(ctx, []string{"012345678905", "nope"})
	if err != nil || len(vs) != 1 || vs[0].SKU != "PB-16" {
		t.Fatalf("lookup: %v %v", vs, err)
	}
	item := vs[0].InventoryItemID

	// Move the contaminated lot (12 units) to Quarantine; 38 stay sellable.
	if err := s.MoveAvailable(ctx, item, main_, quarantine, 12, "quality_control"); err != nil {
		t.Fatal(err)
	}
	v, _ := s.GetVariant("gid://shopify/ProductVariant/1")
	if v.Inventory[0].Available != 38 || v.Inventory[1].LocationID != quarantine || v.Inventory[1].Available != 12 {
		t.Fatalf("inventory after move: %+v", v.Inventory)
	}
	// Cannot move more than available.
	err = s.MoveAvailable(ctx, item, main_, quarantine, 100, "")
	var ue *shopify.UserErrors
	if !errors.As(err, &ue) || ue.Errors[0].Code != "INVALID_QUANTITY_TOO_LOW" {
		t.Fatalf("over-move: %v", err)
	}

	if err := s.AddTags(ctx, "gid://shopify/Product/1", []string{"RECALL_LOT:2026-X89"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetMetafields(ctx, []shopify.Metafield{{OwnerID: "gid://shopify/Product/1", Namespace: "soteria", Key: "badge", Value: "Verified Safe Lot"}}); err != nil {
		t.Fatal(err)
	}
	v, _ = s.GetVariant("gid://shopify/ProductVariant/1")
	if len(v.Tags) != 1 || v.Tags[0] != "RECALL_LOT:2026-X89" {
		t.Errorf("tags: %v", v.Tags)
	}
	if mf, ok := s.GetMetafield("gid://shopify/Product/1", "soteria", "badge"); !ok || mf.Value != "Verified Safe Lot" || mf.Type != "single_line_text_field" {
		t.Errorf("metafield: %+v ok=%v", mf, ok)
	}

	// Full-SKU hold: zero at every location + draft.
	if err := s.SetAvailable(ctx, item, main_, 0, "quality_control"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetProductStatus(ctx, "gid://shopify/Product/1", shopify.StatusDraft); err != nil {
		t.Fatal(err)
	}
	v, _ = s.GetVariant("gid://shopify/ProductVariant/1")
	if v.Inventory[0].Available != 0 || v.ProductStatus != shopify.StatusDraft {
		t.Errorf("hold: %+v", v)
	}
	// Untouched product unaffected.
	other, _ := s.GetVariant("gid://shopify/ProductVariant/2")
	if other.Available() != 20 || other.ProductStatus != shopify.StatusActive || len(other.Tags) != 0 {
		t.Errorf("collateral damage: %+v", other)
	}
	if len(s.Calls) != 5 {
		t.Errorf("calls recorded: %v", s.Calls)
	}
}

func TestOrdersAndRescue(t *testing.T) {
	s := seeded()
	ctx := context.Background()

	recent, err := s.OrdersSince(ctx, time.Now().Add(-7*24*time.Hour))
	if err != nil || len(recent) != 1 || recent[0].Name != "#1001" {
		t.Fatalf("recent: %v %v", recent, err)
	}
	if !recent[0].Contains("gid://shopify/ProductVariant/1") {
		t.Fatal("order should contain recalled variant")
	}

	// Swap 2 of 3 units for the allergen-safe substitute.
	if err := s.ReplaceLineItem(ctx, "gid://shopify/Order/10", "gid://shopify/LineItem/100", "gid://shopify/ProductVariant/2", 2, true); err != nil {
		t.Fatal(err)
	}
	o, _ := s.GetOrder("gid://shopify/Order/10")
	if len(o.LineItems) != 2 || o.LineItems[0].Quantity != 1 || o.LineItems[1].VariantID != "gid://shopify/ProductVariant/2" || o.LineItems[1].Quantity != 2 {
		t.Fatalf("after replace: %+v", o.LineItems)
	}
	// Replace the last unit → original line disappears.
	if err := s.ReplaceLineItem(ctx, "gid://shopify/Order/10", "gid://shopify/LineItem/100", "gid://shopify/ProductVariant/2", 1, false); err != nil {
		t.Fatal(err)
	}
	o, _ = s.GetOrder("gid://shopify/Order/10")
	if len(o.LineItems) != 2 || o.Contains("gid://shopify/ProductVariant/1") {
		t.Fatalf("original line should be gone: %+v", o.LineItems)
	}
	if err := s.ReplaceLineItem(ctx, "gid://shopify/Order/10", "gid://shopify/LineItem/100", "gid://shopify/ProductVariant/2", 1, false); err == nil {
		t.Fatal("replacing a removed line must fail")
	}
}

func TestFailOnce(t *testing.T) {
	s := seeded()
	s.Fail["Catalog"] = errors.New("simulated outage")
	if _, err := s.Catalog(context.Background()); err == nil {
		t.Fatal("expected injected failure")
	}
	if vs, err := s.Catalog(context.Background()); err != nil || len(vs) != 2 {
		t.Fatalf("second call should succeed: %v %v", vs, err)
	}
}

func TestUnknownIdsAreUserErrors(t *testing.T) {
	s := seeded()
	ctx := context.Background()
	var ue *shopify.UserErrors
	if err := s.SetAvailable(ctx, "gid://shopify/InventoryItem/999", main_, 1, ""); !errors.As(err, &ue) {
		t.Errorf("unknown item: %v", err)
	}
	if err := s.SetAvailable(ctx, "gid://shopify/InventoryItem/1", "gid://shopify/Location/999", 1, ""); !errors.As(err, &ue) {
		t.Errorf("unknown location: %v", err)
	}
	if err := s.SetProductStatus(ctx, "gid://shopify/Product/999", shopify.StatusDraft); !errors.As(err, &ue) {
		t.Errorf("unknown product: %v", err)
	}
	if _, err := s.Variant(ctx, "gid://shopify/ProductVariant/999"); err == nil {
		t.Error("unknown variant")
	}
}
