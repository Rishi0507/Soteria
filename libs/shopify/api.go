package shopify

import (
	"context"
	"time"
)

// API is the operation set Sotería needs from a commerce backend. Client
// implements it against Shopify; fake.Store implements it in memory.
// Person 3's services should depend on this interface, never on Client.
type API interface {
	// ---- read ----

	// Locations lists the shop's locations (including inactive ones).
	Locations(ctx context.Context) ([]Location, error)
	// Catalog pages through every product variant with its per-location
	// available quantity. Expect hundreds of variants, not millions: this is
	// the snapshot the resolution-service matches recalls against.
	Catalog(ctx context.Context) ([]Variant, error)
	// VariantsByBarcode looks up variants whose barcode is in barcodes
	// (exact match). Missing barcodes are simply absent from the result.
	VariantsByBarcode(ctx context.Context, barcodes []string) ([]Variant, error)
	// Variant fetches one variant by id.
	Variant(ctx context.Context, variantID string) (Variant, error)
	// OrdersSince returns orders created at or after since (newest first),
	// with line items, for the affected-customer scan.
	OrdersSince(ctx context.Context, since time.Time) ([]Order, error)

	// ---- inventory ----

	// SetAvailable sets the available quantity of an inventory item at a
	// location to an absolute value. reason is a Shopify inventory reason
	// (e.g. "correction", "damaged", "quality_control", "safety_stock").
	SetAvailable(ctx context.Context, inventoryItemID, locationID string, quantity int, reason string) error
	// MoveAvailable moves quantity units of an inventory item between two
	// locations atomically (from → to). Used to quarantine a contaminated lot
	// without touching the safe stock.
	MoveAvailable(ctx context.Context, inventoryItemID, fromLocationID, toLocationID string, quantity int, reason string) error

	// ---- product ----

	// SetProductStatus sets ACTIVE / DRAFT / ARCHIVED (DRAFT unpublishes).
	SetProductStatus(ctx context.Context, productID, status string) error
	// AddTags / RemoveTags edit product tags (e.g. RECALL_HAZARD).
	AddTags(ctx context.Context, productID string, tags []string) error
	RemoveTags(ctx context.Context, productID string, tags []string) error
	// SetMetafields upserts metafields (e.g. soteria.badge on a product).
	SetMetafields(ctx context.Context, fields []Metafield) error

	// ---- orders ----

	// ReplaceLineItem swaps quantity units of a line item for another
	// variant on an existing order via Shopify order editing. The customer
	// must have confirmed the substitute before this is called (PRD: never
	// silently substitute). notify controls Shopify's own customer email.
	ReplaceLineItem(ctx context.Context, orderID, lineItemID, newVariantID string, quantity int, notify bool) error
}
