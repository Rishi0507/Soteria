package orders

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"soteria/libs/shopify"
	"soteria/libs/shopify/fake"
)

const variantID = "gid://shopify/ProductVariant/1"

func seeded(lots ...shopify.Lot) *fake.Store {
	s := fake.New()
	s.AddVariant(shopify.Variant{
		ID: variantID, ProductID: "gid://shopify/Product/1",
		ProductTitle: "Sunfield Farms Chewy Granola Bars", Title: "12 ct",
		SKU: "SF-GB-12", Barcode: "041196910537", Price: "4.49", Lots: lots,
	})
	s.AddVariant(shopify.Variant{
		ID: "gid://shopify/ProductVariant/2", ProductID: "gid://shopify/Product/2",
		ProductTitle: "Harvest Lane Granola Bars", SKU: "HL-GB-12",
		Barcode: "012345678905", Price: "4.49",
	})
	now := time.Now().UTC()
	s.AddOrder(shopify.Order{
		ID: "gid://shopify/Order/1001", Name: "#1001", CreatedAt: now.Add(-time.Hour),
		Email: "buyer@example.com", CustomerID: "cust-77", FulfillmentStatus: "UNFULFILLED",
		LineItems: []shopify.LineItem{{ID: "li-1", VariantID: variantID, SKU: "SF-GB-12", Quantity: 2}},
	})
	s.AddOrder(shopify.Order{
		ID: "gid://shopify/Order/1003", Name: "#1003", CreatedAt: now.Add(-2 * time.Hour),
		Email: "gone@example.com", CustomerID: "cust-12", FulfillmentStatus: "FULFILLED",
		LineItems: []shopify.LineItem{{ID: "li-3", VariantID: variantID, Quantity: 3}},
	})
	return s
}

func TestAffectedLinesOnlyCoversInFlightOrders(t *testing.T) {
	repo := NewStore(seeded(shopify.Lot{Code: "8H-1132", Units: 40}), "USD", 0, nil)
	got, err := repo.AffectedLines(context.Background(), "00041196910537", []string{"8H-1132"})
	if err != nil {
		t.Fatalf("affected lines: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected only the unfulfilled order, got %d lines", len(got))
	}
	if got[0].Order.OrderID != "gid://shopify/Order/1001" {
		t.Errorf("order = %q", got[0].Order.OrderID)
	}
	if got[0].Line.LotCode != "8H-1132" {
		t.Errorf("lot code = %q, want the single held lot", got[0].Line.LotCode)
	}
	if got[0].Line.UnitPrice.AmountMinor != 449 || got[0].Line.UnitPrice.Currency != "USD" {
		t.Errorf("unit price = %+v, want 449 USD", got[0].Line.UnitPrice)
	}
	if got[0].Line.Quantity != 2 {
		t.Errorf("quantity = %d", got[0].Line.Quantity)
	}
}

// TestAffectedLinesMarksUnattributableLots: the platform records a variant per
// line, not a lot, so when only part of the ledger is held we cannot name the
// lot. We must include the line and label it honestly rather than imply
// certainty we do not have.
func TestAffectedLinesMarksUnattributableLots(t *testing.T) {
	store := seeded(
		shopify.Lot{Code: "8H-1132", Units: 40},
		shopify.Lot{Code: "8H-1133", Units: 25},
		shopify.Lot{Code: "8H-2000", Units: 60},
	)
	repo := NewStore(store, "USD", 0, nil)
	got, err := repo.AffectedLines(context.Background(), "00041196910537", []string{"8H-1132", "8H-1133"})
	if err != nil {
		t.Fatalf("affected lines: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected the in-flight line to still be included, got %d", len(got))
	}
	if got[0].Line.LotCode != LotUnattributed {
		t.Errorf("lot code = %q, want %q", got[0].Line.LotCode, LotUnattributed)
	}
}

func TestAffectedLinesWholeSKUHold(t *testing.T) {
	repo := NewStore(seeded(), "USD", 0, nil)
	got, err := repo.AffectedLines(context.Background(), "00041196910537", nil)
	if err != nil {
		t.Fatalf("affected lines: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("a whole-SKU hold must still find in-flight lines, got %d", len(got))
	}
}

func TestAffectedLinesUnknownProduct(t *testing.T) {
	repo := NewStore(seeded(), "USD", 0, nil)
	got, err := repo.AffectedLines(context.Background(), "00073123456788", []string{"P908"})
	if err != nil {
		t.Fatalf("a product the store does not carry is not an error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no lines, got %d", len(got))
	}
}

func TestApplySwapReplacesTheLine(t *testing.T) {
	store := seeded(shopify.Lot{Code: "8H-1132", Units: 40})
	repo := NewStore(store, "USD", 0, nil)
	if err := repo.ApplySwap(context.Background(), "gid://shopify/Order/1001", "li-1", "00012345678905"); err != nil {
		t.Fatalf("apply swap: %v", err)
	}
	var replaced bool
	for _, c := range store.Calls {
		if strings.Contains(c, "ReplaceLineItem") {
			replaced = true
		}
	}
	if !replaced {
		t.Fatalf("expected a ReplaceLineItem call, got %v", store.Calls)
	}
}

func TestApplySwapRejectsUnknownSubstitute(t *testing.T) {
	repo := NewStore(seeded(), "USD", 0, nil)
	if err := repo.ApplySwap(context.Background(), "gid://shopify/Order/1001", "li-1", "00099999999999"); err == nil {
		t.Fatal("expected an error for a substitute that is not in the store")
	}
}

// TestCancelIsReportedAsUnsupported: the shared client has no cancellation
// mutation yet. The gap must be a typed error the caller can recognize, so the
// customer's decision is recorded instead of retried forever.
func TestCancelIsReportedAsUnsupported(t *testing.T) {
	err := NewStore(seeded(), "USD", 0, nil).Cancel(context.Background(), "gid://shopify/Order/1001")
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("err = %v, want ErrUnsupported", err)
	}
}
