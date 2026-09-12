package catalog

import (
	"context"
	"errors"
	"testing"
	"time"

	"soteria/libs/shopify"
	"soteria/libs/shopify/fake"
)

func seeded() *fake.Store {
	s := fake.New()
	s.AddVariant(shopify.Variant{
		ID: "gid://shopify/ProductVariant/1", ProductID: "gid://shopify/Product/1",
		ProductTitle: "Sunfield Farms Chewy Granola Bars", Title: "12 ct",
		Vendor: "Sunfield Farms", SKU: "SF-GB-12", Barcode: "041196910537",
		Lots: []shopify.Lot{{Code: "8H-1132", Units: 40}, {Code: "8H-2000", Units: 60}},
	})
	// No barcode: nothing a recall notice could match on.
	s.AddVariant(shopify.Variant{
		ID: "gid://shopify/ProductVariant/2", ProductTitle: "Loose Oats", SKU: "BULK-1",
	})
	return s
}

func TestStoreMapsVariantsToCandidates(t *testing.T) {
	c := NewStore(seeded(), time.Minute, nil)
	got, err := c.Candidates(context.Background())
	if err != nil {
		t.Fatalf("candidates: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected the barcode-less variant to be dropped, got %d candidates", len(got))
	}
	if got[0].GTIN != "00041196910537" {
		t.Errorf("gtin = %q, want the normalized GTIN-14", got[0].GTIN)
	}
	if got[0].Brand != "Sunfield Farms" || got[0].SKU != "SF-GB-12" {
		t.Errorf("brand/sku = %q/%q", got[0].Brand, got[0].SKU)
	}
	if got[0].ProductTitle != "Sunfield Farms Chewy Granola Bars 12 ct" {
		t.Errorf("title = %q, want product and variant title joined", got[0].ProductTitle)
	}
	if len(got[0].LotCodes) != 2 {
		t.Errorf("lot codes = %v, want the store's lot ledger", got[0].LotCodes)
	}
}

func TestStoreCachesUntilRefreshIsDue(t *testing.T) {
	store := seeded()
	c := NewStore(store, time.Hour, nil)
	if _, err := c.Candidates(context.Background()); err != nil {
		t.Fatalf("first call: %v", err)
	}
	before := len(store.Calls)
	if _, err := c.Candidates(context.Background()); err != nil {
		t.Fatalf("second call: %v", err)
	}
	if len(store.Calls) != before {
		t.Fatalf("second call within the refresh window should not hit the store")
	}
}

// TestStoreFallsBackToLastGoodSnapshot: an empty catalog would resolve every
// recall to "we do not carry this", so a refresh failure must reuse the last
// snapshot instead.
func TestStoreFallsBackToLastGoodSnapshot(t *testing.T) {
	store := seeded()
	c := NewStore(store, time.Nanosecond, nil)
	if _, err := c.Candidates(context.Background()); err != nil {
		t.Fatalf("warm up: %v", err)
	}
	store.Fail["Catalog"] = errors.New("shopify: 503")

	got, err := c.Candidates(context.Background())
	if err != nil {
		t.Fatalf("a refresh failure must not fail the lookup: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected the cached snapshot, got %d candidates", len(got))
	}
}

// TestStoreWithNoSnapshotReturnsError: with nothing cached we must surface the
// error so the message is retried, never report an empty catalog as a clean miss.
func TestStoreWithNoSnapshotReturnsError(t *testing.T) {
	store := fake.New()
	store.Fail["Catalog"] = errors.New("shopify: 503")
	if _, err := NewStore(store, time.Minute, nil).Candidates(context.Background()); err == nil {
		t.Fatal("expected an error when no snapshot has ever been fetched")
	}
}

func TestStoreByGTIN(t *testing.T) {
	c := NewStore(seeded(), time.Minute, nil)
	if _, ok := c.ByGTIN(context.Background(), "0 41196 91053 7"); !ok {
		t.Error("expected a hit for the same GTIN in a different format")
	}
	if _, ok := c.ByGTIN(context.Background(), "012345678905"); ok {
		t.Error("expected a miss for a product the store does not carry")
	}
}
