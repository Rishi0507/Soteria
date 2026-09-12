// Package fake is an in-memory shopify.API for tests and offline
// development of the containment and order-rescue services. It enforces
// the same invariants the real API does where they matter for correctness
// (unknown ids, negative stock, moving more than available).
package fake

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"soteria/libs/shopify"
)

// Store is a mutable in-memory shop.
type Store struct {
	mu         sync.RWMutex
	locations  map[string]shopify.Location
	variants   map[string]*shopify.Variant // by variant id
	orders     map[string]*shopify.Order
	metafields map[string]shopify.Metafield // owner|ns|key
	// Calls records every mutation for assertions, oldest first.
	Calls []string
	// Fail, if set, makes the named operation return this error once.
	Fail map[string]error
}

// New returns an empty store.
func New() *Store {
	return &Store{
		locations:  map[string]shopify.Location{},
		variants:   map[string]*shopify.Variant{},
		orders:     map[string]*shopify.Order{},
		metafields: map[string]shopify.Metafield{},
		Fail:       map[string]error{},
	}
}

func (s *Store) failOnce(op string) error {
	if err, ok := s.Fail[op]; ok {
		delete(s.Fail, op)
		return err
	}
	return nil
}

func (s *Store) record(format string, args ...any) {
	s.Calls = append(s.Calls, fmt.Sprintf(format, args...))
}

// ---- seeding ----------------------------------------------------------------

// AddLocation registers a location.
func (s *Store) AddLocation(l shopify.Location) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.locations[l.ID] = l
}

// AddVariant registers a variant (copy) with its inventory levels.
func (s *Store) AddVariant(v shopify.Variant) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := v
	c.Tags = append([]string(nil), v.Tags...)
	c.Inventory = append([]shopify.InventoryLevel(nil), v.Inventory...)
	c.Lots = append([]shopify.Lot(nil), v.Lots...)
	s.variants[v.ID] = &c
}

// AddOrder registers an order (copy).
func (s *Store) AddOrder(o shopify.Order) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := o
	c.LineItems = append([]shopify.LineItem(nil), o.LineItems...)
	s.orders[o.ID] = &c
}

// GetVariant returns a copy of the current variant state.
func (s *Store) GetVariant(id string) (shopify.Variant, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.variants[id]
	if !ok {
		return shopify.Variant{}, false
	}
	return copyVariant(v), true
}

// GetOrder returns a copy of the current order state.
func (s *Store) GetOrder(id string) (shopify.Order, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	o, ok := s.orders[id]
	if !ok {
		return shopify.Order{}, false
	}
	c := *o
	c.LineItems = append([]shopify.LineItem(nil), o.LineItems...)
	return c, true
}

// GetMetafield returns a metafield if set.
func (s *Store) GetMetafield(ownerID, namespace, key string) (shopify.Metafield, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m, ok := s.metafields[ownerID+"|"+namespace+"|"+key]
	return m, ok
}

func copyVariant(v *shopify.Variant) shopify.Variant {
	c := *v
	c.Tags = append([]string(nil), v.Tags...)
	c.Inventory = append([]shopify.InventoryLevel(nil), v.Inventory...)
	c.Lots = append([]shopify.Lot(nil), v.Lots...)
	return c
}

// ---- shopify.API ------------------------------------------------------------

func (s *Store) Locations(_ context.Context) ([]shopify.Location, error) {
	if err := s.failOnce("Locations"); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]shopify.Location, 0, len(s.locations))
	for _, l := range s.locations {
		out = append(out, l)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *Store) Catalog(_ context.Context) ([]shopify.Variant, error) {
	if err := s.failOnce("Catalog"); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]shopify.Variant, 0, len(s.variants))
	for _, v := range s.variants {
		out = append(out, copyVariant(v))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *Store) VariantsByBarcode(_ context.Context, barcodes []string) ([]shopify.Variant, error) {
	if err := s.failOnce("VariantsByBarcode"); err != nil {
		return nil, err
	}
	want := map[string]bool{}
	for _, b := range barcodes {
		want[strings.TrimSpace(b)] = true
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []shopify.Variant
	for _, v := range s.variants {
		if v.Barcode != "" && want[v.Barcode] {
			out = append(out, copyVariant(v))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *Store) Variant(_ context.Context, variantID string) (shopify.Variant, error) {
	if err := s.failOnce("Variant"); err != nil {
		return shopify.Variant{}, err
	}
	v, ok := s.GetVariant(variantID)
	if !ok {
		return shopify.Variant{}, fmt.Errorf("shopify: variant %s not found", variantID)
	}
	return v, nil
}

func (s *Store) OrdersSince(_ context.Context, since time.Time) ([]shopify.Order, error) {
	if err := s.failOnce("OrdersSince"); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []shopify.Order
	for _, o := range s.orders {
		if !o.CreatedAt.Before(since) {
			c := *o
			c.LineItems = append([]shopify.LineItem(nil), o.LineItems...)
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (s *Store) byInventoryItem(id string) *shopify.Variant {
	for _, v := range s.variants {
		if v.InventoryItemID == id {
			return v
		}
	}
	return nil
}

func level(v *shopify.Variant, locationID string) *shopify.InventoryLevel {
	for i := range v.Inventory {
		if v.Inventory[i].LocationID == locationID {
			return &v.Inventory[i]
		}
	}
	v.Inventory = append(v.Inventory, shopify.InventoryLevel{LocationID: locationID})
	return &v.Inventory[len(v.Inventory)-1]
}

func (s *Store) SetAvailable(_ context.Context, inventoryItemID, locationID string, quantity int, reason string) error {
	if err := s.failOnce("SetAvailable"); err != nil {
		return err
	}
	if quantity < 0 {
		return &shopify.UserErrors{Mutation: "inventorySetQuantities", Errors: []shopify.UserError{{Message: "quantity must be non-negative", Code: "INVALID_QUANTITY_NEGATIVE"}}}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.locations[locationID]; !ok {
		return &shopify.UserErrors{Mutation: "inventorySetQuantities", Errors: []shopify.UserError{{Message: "location not found", Code: "INVALID_LOCATION"}}}
	}
	v := s.byInventoryItem(inventoryItemID)
	if v == nil {
		return &shopify.UserErrors{Mutation: "inventorySetQuantities", Errors: []shopify.UserError{{Message: "inventory item not found", Code: "INVALID_ITEM"}}}
	}
	level(v, locationID).Available = quantity
	s.record("SetAvailable(%s,%s,%d,%s)", inventoryItemID, locationID, quantity, reason)
	return nil
}

func (s *Store) MoveAvailable(_ context.Context, inventoryItemID, fromLocationID, toLocationID string, quantity int, reason string) error {
	if err := s.failOnce("MoveAvailable"); err != nil {
		return err
	}
	if quantity <= 0 {
		return fmt.Errorf("shopify: move quantity must be positive")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, loc := range []string{fromLocationID, toLocationID} {
		if _, ok := s.locations[loc]; !ok {
			return &shopify.UserErrors{Mutation: "inventoryMoveQuantities", Errors: []shopify.UserError{{Message: "location not found: " + loc, Code: "INVALID_LOCATION"}}}
		}
	}
	v := s.byInventoryItem(inventoryItemID)
	if v == nil {
		return &shopify.UserErrors{Mutation: "inventoryMoveQuantities", Errors: []shopify.UserError{{Message: "inventory item not found", Code: "INVALID_ITEM"}}}
	}
	from := level(v, fromLocationID)
	if from.Available < quantity {
		return &shopify.UserErrors{Mutation: "inventoryMoveQuantities", Errors: []shopify.UserError{{Message: fmt.Sprintf("only %d available at source", from.Available), Code: "INVALID_QUANTITY_TOO_LOW"}}}
	}
	from.Available -= quantity
	level(v, toLocationID).Available += quantity
	s.record("MoveAvailable(%s,%s->%s,%d,%s)", inventoryItemID, fromLocationID, toLocationID, quantity, reason)
	return nil
}

func (s *Store) forProduct(productID string, f func(*shopify.Variant)) bool {
	found := false
	for _, v := range s.variants {
		if v.ProductID == productID {
			f(v)
			found = true
		}
	}
	return found
}

func (s *Store) SetProductStatus(_ context.Context, productID, status string) error {
	if err := s.failOnce("SetProductStatus"); err != nil {
		return err
	}
	switch status {
	case shopify.StatusActive, shopify.StatusDraft, shopify.StatusArchived:
	default:
		return fmt.Errorf("shopify: invalid product status %q", status)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.forProduct(productID, func(v *shopify.Variant) { v.ProductStatus = status }) {
		return &shopify.UserErrors{Mutation: "productUpdate", Errors: []shopify.UserError{{Message: "product not found"}}}
	}
	s.record("SetProductStatus(%s,%s)", productID, status)
	return nil
}

func (s *Store) AddTags(_ context.Context, productID string, tags []string) error {
	if err := s.failOnce("AddTags"); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ok := s.forProduct(productID, func(v *shopify.Variant) {
		for _, t := range tags {
			if !contains(v.Tags, t) {
				v.Tags = append(v.Tags, t)
			}
		}
	})
	if !ok {
		return &shopify.UserErrors{Mutation: "tagsAdd", Errors: []shopify.UserError{{Message: "product not found"}}}
	}
	s.record("AddTags(%s,%v)", productID, tags)
	return nil
}

func (s *Store) RemoveTags(_ context.Context, productID string, tags []string) error {
	if err := s.failOnce("RemoveTags"); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ok := s.forProduct(productID, func(v *shopify.Variant) {
		kept := v.Tags[:0]
		for _, t := range v.Tags {
			if !contains(tags, t) {
				kept = append(kept, t)
			}
		}
		v.Tags = kept
	})
	if !ok {
		return &shopify.UserErrors{Mutation: "tagsRemove", Errors: []shopify.UserError{{Message: "product not found"}}}
	}
	s.record("RemoveTags(%s,%v)", productID, tags)
	return nil
}

func (s *Store) SetMetafields(_ context.Context, fields []shopify.Metafield) error {
	if err := s.failOnce("SetMetafields"); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, f := range fields {
		if f.OwnerID == "" || f.Namespace == "" || f.Key == "" {
			return &shopify.UserErrors{Mutation: "metafieldsSet", Errors: []shopify.UserError{{Message: "ownerId, namespace and key are required"}}}
		}
		if f.Type == "" {
			f.Type = "single_line_text_field"
		}
		s.metafields[f.OwnerID+"|"+f.Namespace+"|"+f.Key] = f
		s.record("SetMetafield(%s,%s.%s=%s)", f.OwnerID, f.Namespace, f.Key, f.Value)
		// Mirror the lot ledger onto the variant like the real read path does.
		if f.Namespace == shopify.MetafieldNamespace && f.Key == shopify.LotsKey {
			if v, ok := s.variants[f.OwnerID]; ok {
				var lots []shopify.Lot
				if err := json.Unmarshal([]byte(f.Value), &lots); err != nil {
					return &shopify.UserErrors{Mutation: "metafieldsSet", Errors: []shopify.UserError{{Message: "invalid json: " + err.Error(), Code: "INVALID_VALUE"}}}
				}
				v.Lots = lots
			}
		}
	}
	return nil
}

func (s *Store) ReplaceLineItem(_ context.Context, orderID, lineItemID, newVariantID string, quantity int, notify bool) error {
	if err := s.failOnce("ReplaceLineItem"); err != nil {
		return err
	}
	if quantity <= 0 {
		return fmt.Errorf("shopify: replace quantity must be positive")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	o, ok := s.orders[orderID]
	if !ok {
		return &shopify.UserErrors{Mutation: "orderEditBegin", Errors: []shopify.UserError{{Message: "order not found"}}}
	}
	nv, ok := s.variants[newVariantID]
	if !ok {
		return &shopify.UserErrors{Mutation: "orderEditAddVariant", Errors: []shopify.UserError{{Message: "variant not found"}}}
	}
	idx := -1
	for i, li := range o.LineItems {
		if li.ID == lineItemID {
			idx = i
		}
	}
	if idx < 0 {
		return fmt.Errorf("shopify: line item %s not found on order %s", lineItemID, orderID)
	}
	if quantity > o.LineItems[idx].Quantity {
		return fmt.Errorf("shopify: cannot replace %d units, line item has %d", quantity, o.LineItems[idx].Quantity)
	}
	o.LineItems[idx].Quantity -= quantity
	if o.LineItems[idx].Quantity == 0 {
		o.LineItems = append(o.LineItems[:idx], o.LineItems[idx+1:]...)
	}
	o.LineItems = append(o.LineItems, shopify.LineItem{
		ID: fmt.Sprintf("gid://shopify/LineItem/fake-%d", len(s.Calls)+1), VariantID: nv.ID, SKU: nv.SKU,
		Title: nv.ProductTitle + " - " + nv.Title, Quantity: quantity,
	})
	s.record("ReplaceLineItem(%s,%s->%s,%d,notify=%v)", orderID, lineItemID, newVariantID, quantity, notify)
	return nil
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

var _ shopify.API = (*Store)(nil)
