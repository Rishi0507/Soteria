// Package shopify is Sotería's shared client for the Shopify Admin GraphQL
// API. It is the only code that talks to Shopify: the containment-service
// and order-rescue-service (Person 3) consume the API interface, and the
// storefront BFF may too.
//
// Design:
//   - One interface (API) with an HTTP implementation (Client) and an
//     in-memory implementation (package fake) so consumers can develop and
//     test without a store.
//   - Cost-aware throttling: Shopify's GraphQL API is a leaky bucket of
//     query-cost points, not requests/second. The client reads the cost
//     extension on every response and waits for the bucket to refill before
//     a request that would be throttled, and retries on THROTTLED.
//   - Every mutation surfaces Shopify userErrors as a typed error.
package shopify

import (
	"fmt"
	"time"
)

// Location is a fulfillment location (warehouse, store, or the Quarantine
// location Sotería moves contaminated lots into).
type Location struct {
	ID       string `json:"id"` // gid://shopify/Location/123
	Name     string `json:"name"`
	IsActive bool   `json:"is_active"`
	Fulfills bool   `json:"fulfills_online_orders"`
}

// InventoryLevel is available quantity of one inventory item at one location.
type InventoryLevel struct {
	LocationID string `json:"location_id"`
	Available  int    `json:"available"`
}

// Lot is one production lot of a variant as recorded in the variant's
// soteria.lots metafield (JSON list). Shopify has no native lot tracking;
// this metafield is Sotería's lot ledger, kept on the store so every
// service and the storefront read the same truth.
type Lot struct {
	Code   string `json:"code"`
	Units  int    `json:"units"`
	Expiry string `json:"expiry,omitempty"` // free text, e.g. 2027-03
	Held   bool   `json:"held,omitempty"`   // moved to Quarantine by a containment action
}

// LotsNamespace / LotsKey locate the lot ledger metafield on a variant.
const (
	MetafieldNamespace = "soteria"
	LotsKey            = "lots"
	BadgeKey           = "badge"
)

// Variant is a sellable SKU with the fields containment needs. Barcode is
// the UPC/EAN/GTIN — the key the resolution-service matches recalls on.
type Variant struct {
	ID              string           `json:"id"` // gid://shopify/ProductVariant/123
	ProductID       string           `json:"product_id"`
	ProductTitle    string           `json:"product_title"`
	ProductStatus   string           `json:"product_status"` // ACTIVE | DRAFT | ARCHIVED
	ProductHandle   string           `json:"product_handle"`
	Vendor          string           `json:"vendor"`
	Tags            []string         `json:"tags"`
	Title           string           `json:"title"` // variant title, e.g. "16 oz"
	SKU             string           `json:"sku"`
	Barcode         string           `json:"barcode"`
	Price           string           `json:"price"`
	InventoryItemID string           `json:"inventory_item_id"` // gid://shopify/InventoryItem/123
	Inventory       []InventoryLevel `json:"inventory"`
	Lots            []Lot            `json:"lots,omitempty"` // from metafield soteria.lots; nil when the store has no lot data
}

// AvailableAt returns available quantity at one location.
func (v Variant) AvailableAt(locationID string) int {
	for _, l := range v.Inventory {
		if l.LocationID == locationID {
			return l.Available
		}
	}
	return 0
}

// Available sums available quantity across locations.
func (v Variant) Available() int {
	n := 0
	for _, l := range v.Inventory {
		n += l.Available
	}
	return n
}

// LineItem is one line of an order.
type LineItem struct {
	ID        string `json:"id"`
	VariantID string `json:"variant_id"`
	SKU       string `json:"sku"`
	Title     string `json:"title"`
	Quantity  int    `json:"quantity"`
}

// Order is the subset of an order the rescue flow needs.
type Order struct {
	ID                string     `json:"id"` // gid://shopify/Order/123
	Name              string     `json:"name"`
	CreatedAt         time.Time  `json:"created_at"`
	Email             string     `json:"email"`
	CustomerID        string     `json:"customer_id"`
	FulfillmentStatus string     `json:"fulfillment_status"` // UNFULFILLED | PARTIALLY_FULFILLED | FULFILLED | ...
	FinancialStatus   string     `json:"financial_status"`
	LineItems         []LineItem `json:"line_items"`
}

// Contains reports whether the order has a line for any of the variant ids.
func (o Order) Contains(variantIDs ...string) bool {
	for _, li := range o.LineItems {
		for _, id := range variantIDs {
			if li.VariantID == id {
				return true
			}
		}
	}
	return false
}

// Metafield is a typed key/value attached to a product or variant, e.g. the
// "Verified Safe Lot" badge the storefront renders.
type Metafield struct {
	OwnerID   string `json:"owner_id"`
	Namespace string `json:"namespace"`
	Key       string `json:"key"`
	Type      string `json:"type"` // single_line_text_field | json | boolean | ...
	Value     string `json:"value"`
}

// Product statuses.
const (
	StatusActive   = "ACTIVE"
	StatusDraft    = "DRAFT"
	StatusArchived = "ARCHIVED"
)

// UserError is a validation error returned by a Shopify mutation.
type UserError struct {
	Field   []string `json:"field"`
	Message string   `json:"message"`
	Code    string   `json:"code,omitempty"`
}

// UserErrors wraps the userErrors of one mutation.
type UserErrors struct {
	Mutation string
	Errors   []UserError
}

func (e *UserErrors) Error() string {
	if len(e.Errors) == 1 {
		return fmt.Sprintf("shopify: %s: %s (%v)", e.Mutation, e.Errors[0].Message, e.Errors[0].Field)
	}
	return fmt.Sprintf("shopify: %s: %d user errors, first: %s (%v)", e.Mutation, len(e.Errors), e.Errors[0].Message, e.Errors[0].Field)
}

// GraphQLError is a top-level GraphQL error (syntax, auth, throttling…).
type GraphQLError struct {
	Message    string         `json:"message"`
	Extensions map[string]any `json:"extensions,omitempty"`
}

// Code returns extensions.code if present (e.g. THROTTLED, ACCESS_DENIED).
func (e GraphQLError) Code() string {
	c, _ := e.Extensions["code"].(string)
	return c
}

// GraphQLErrors wraps top-level errors of one request.
type GraphQLErrors []GraphQLError

func (e GraphQLErrors) Error() string {
	if len(e) == 0 {
		return "shopify: graphql error"
	}
	if c := e[0].Code(); c != "" {
		return fmt.Sprintf("shopify: graphql: %s: %s", c, e[0].Message)
	}
	return "shopify: graphql: " + e[0].Message
}

// HTTPError is a non-2xx response that is not a GraphQL error document.
type HTTPError struct {
	Status int
	Body   string
}

func (e *HTTPError) Error() string { return fmt.Sprintf("shopify: HTTP %d: %s", e.Status, e.Body) }
