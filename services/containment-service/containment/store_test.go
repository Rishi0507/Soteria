package containment_test

import (
	"context"
	"testing"
	"time"

	"soteria/libs/core/bus"
	"soteria/libs/core/events"
	"soteria/libs/shopify"
	"soteria/libs/shopify/fake"
	"soteria/libs/shopify/hold"
	"soteria/services/containment-service/containment"
)

const (
	sellingLoc   = "gid://shopify/Location/1"
	quarantine   = "gid://shopify/Location/2"
	productID    = "gid://shopify/Product/1"
	variantID    = "gid://shopify/ProductVariant/1"
	inventoryID  = "gid://shopify/InventoryItem/1"
	recalledGTIN = "00041196910537"
)

// seededStore mirrors a real shop: one product, three lots on the ledger, all
// stock at the selling location.
func seededStore(t *testing.T) (*fake.Store, *hold.Adapter) {
	t.Helper()
	s := fake.New()
	s.AddLocation(shopify.Location{ID: sellingLoc, Name: "Main Warehouse", IsActive: true, Fulfills: true})
	s.AddLocation(shopify.Location{ID: quarantine, Name: "Quarantine", IsActive: true})
	s.AddVariant(shopify.Variant{
		ID: variantID, ProductID: productID, ProductStatus: shopify.StatusActive,
		ProductTitle: "Sunfield Farms Chewy Granola Bars 12 ct",
		SKU:          "SF-GB-12", Barcode: "041196910537", InventoryItemID: inventoryID,
		Inventory: []shopify.InventoryLevel{{LocationID: sellingLoc, Available: 125}},
		Lots: []shopify.Lot{
			{Code: "8H-1132", Units: 40},
			{Code: "8H-1133", Units: 25},
			{Code: "8H-2000", Units: 60},
		},
	})
	selling, quarantined, err := hold.ResolveLocations(context.Background(), s, "Quarantine")
	if err != nil {
		t.Fatalf("resolve locations: %v", err)
	}
	return s, hold.New(s, selling, quarantined)
}

func resolution(t *testing.T, confidence float64, lots []string) events.Envelope {
	t.Helper()
	scope := events.ScopeLot
	if len(lots) == 0 {
		scope = events.ScopeSKU
	}
	env, err := events.NewEnvelope(events.TypeLotResolved, "resolution-service", "inc-test-1", events.LotResolved{
		IncidentID: "inc-test-1", ResolutionID: events.NewID(), ResolvedAt: time.Now().UTC(),
		Confidence: confidence, Scope: scope, Hazard: "undeclared peanut",
		Signal: events.Signal{Kind: "AGENCY_NOTICE", Source: "fda_enforcement", SourceRef: "F-1"},
		Matches: []events.Match{{
			GTIN: recalledGTIN, SKU: "SF-GB-12", ProductTitle: "Sunfield Farms Chewy Granola Bars 12 ct",
			LotCodes: lots, Confidence: confidence,
		}},
	})
	if err != nil {
		t.Fatalf("envelope: %v", err)
	}
	return env
}

// TestAutoHoldAgainstTheRealAdapter is the integration point between this service
// and the shared commerce client: a high-confidence lot-level resolution must move
// only the recalled lots into Quarantine and leave the clean lot sellable.
func TestAutoHoldAgainstTheRealAdapter(t *testing.T) {
	store, adapter := seededStore(t)
	b := bus.NewInMem(nil)
	svc := containment.New(containment.NewStore(containment.DefaultConfig()), adapter, b, nil)
	if err := svc.Register(b); err != nil {
		t.Fatalf("register: %v", err)
	}

	if err := b.Publish(context.Background(), resolution(t, 0.97, []string{"8H-1132", "8H-1133"})); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if dl := b.DeadLetters(); len(dl) > 0 {
		t.Fatalf("dead letters: %v", dl)
	}

	taken := b.PublishedOfType(events.TypeContainmentTaken)
	if len(taken) != 1 {
		t.Fatalf("expected one containment.action.taken.v1, got %d", len(taken))
	}
	var ct events.ContainmentActionTaken
	if err := taken[0].Into(&ct); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if ct.Decision != events.DecisionAutoHold || ct.Results[0].Status != "HELD" {
		t.Fatalf("decision %s, result %+v", ct.Decision, ct.Results[0])
	}
	if ct.Results[0].UnitsHeld != 65 {
		t.Errorf("units held = %d, want the 65 units of the two recalled lots", ct.Results[0].UnitsHeld)
	}
	if ct.Results[0].UnitsLeftSellable != 60 {
		t.Errorf("units left sellable = %d, want the 60 units of lot 8H-2000", ct.Results[0].UnitsLeftSellable)
	}

	// The store itself must agree: stock moved, not deleted, and the clean lot
	// still on sale.
	v, ok := store.GetVariant(variantID)
	if !ok {
		t.Fatal("variant vanished")
	}
	if got := v.AvailableAt(sellingLoc); got != 60 {
		t.Errorf("selling location has %d units, want 60", got)
	}
	if got := v.AvailableAt(quarantine); got != 65 {
		t.Errorf("quarantine has %d units, want 65", got)
	}
	if v.ProductStatus != shopify.StatusActive {
		t.Errorf("product status = %q, a partial hold must not unpublish the product", v.ProductStatus)
	}
	for _, l := range v.Lots {
		if l.Code == "8H-2000" && l.Held {
			t.Error("clean lot 8H-2000 was marked held")
		}
		if l.Code == "8H-1132" && !l.Held {
			t.Error("recalled lot 8H-1132 was not marked held")
		}
	}
}

// TestBelowThresholdTouchesNothingInTheStore: a review-queue item must leave the
// shop exactly as it was.
func TestBelowThresholdTouchesNothingInTheStore(t *testing.T) {
	store, adapter := seededStore(t)
	b := bus.NewInMem(nil)
	svc := containment.New(containment.NewStore(containment.DefaultConfig()), adapter, b, nil)
	if err := svc.Register(b); err != nil {
		t.Fatalf("register: %v", err)
	}

	if err := b.Publish(context.Background(), resolution(t, 0.55, []string{"8H-1132"})); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if n := len(b.PublishedOfType(events.TypeContainmentProposed)); n != 1 {
		t.Fatalf("expected the action to be queued for review, got %d proposals", n)
	}
	if n := len(b.PublishedOfType(events.TypeContainmentTaken)); n != 0 {
		t.Fatalf("nothing may be held before a human confirms, got %d taken events", n)
	}
	v, _ := store.GetVariant(variantID)
	if v.AvailableAt(sellingLoc) != 125 || v.AvailableAt(quarantine) != 0 {
		t.Fatalf("store was mutated while awaiting review: selling=%d quarantine=%d",
			v.AvailableAt(sellingLoc), v.AvailableAt(quarantine))
	}
	if len(store.Calls) != 0 {
		t.Fatalf("expected no store mutations, got %v", store.Calls)
	}
}

// TestSKUScopeHoldNeedsTheHigherThreshold: with no lot codes recovered, the same
// confidence that would auto-hold a lot must instead wait for a human, because
// the blast radius is the entire product line.
func TestSKUScopeHoldNeedsTheHigherThreshold(t *testing.T) {
	_, adapter := seededStore(t)
	b := bus.NewInMem(nil)
	svc := containment.New(containment.NewStore(containment.DefaultConfig()), adapter, b, nil)
	if err := svc.Register(b); err != nil {
		t.Fatalf("register: %v", err)
	}

	// 0.90 clears the lot threshold (0.85) but not the SKU threshold (0.95).
	if err := b.Publish(context.Background(), resolution(t, 0.90, nil)); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if n := len(b.PublishedOfType(events.TypeContainmentTaken)); n != 0 {
		t.Fatalf("a SKU-wide hold at 0.90 must not be automatic, got %d taken events", n)
	}
	if n := len(b.PublishedOfType(events.TypeContainmentProposed)); n != 1 {
		t.Fatalf("expected one review-queue proposal, got %d", n)
	}
}
