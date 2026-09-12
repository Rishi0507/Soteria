// Package shopify is the seam the containment-service writes inventory through.
//
// The real, rate-limit-aware Shopify Admin API client is owned by the integrations
// workstream. This package declares the *interface* containment depends on, so this
// service is never blocked on it, plus an in-memory Fake used by tests and
// the end-to-end demo. When the real client lands it simply satisfies InventoryClient.
package shopify

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// HoldRequest is one surgical containment write.
//
// LotCodes is the whole point of Soteria: when it is non-empty, only inventory
// bearing those lot codes may be zeroed; everything else stays sellable.
type HoldRequest struct {
	GTIN     string
	SKU      string
	LotCodes []string
	Reason   string
}

// HoldResponse reports what the platform actually did.
type HoldResponse struct {
	PlatformRef       string
	UnitsHeld         int
	UnitsLeftSellable int
}

// InventoryClient is implemented by the shared Shopify client library.
type InventoryClient interface {
	// HoldLots zeroes available inventory for the given lots (or the whole SKU
	// when LotCodes is empty) and returns what it held.
	HoldLots(ctx context.Context, req HoldRequest) (HoldResponse, error)
	// ReleaseLots reverses a hold, used when a reviewer rejects a proposed action.
	ReleaseLots(ctx context.Context, req HoldRequest) (HoldResponse, error)
}

// --------------------------------------------------------------------- fake

// InventoryLot is one lot of one product sitting in the store.
type InventoryLot struct {
	GTIN    string `json:"gtin"`
	SKU     string `json:"sku,omitempty"`
	LotCode string `json:"lot_code"`
	Units   int    `json:"units"`
	Held    bool   `json:"held,omitempty"`
}

// Fake is an in-memory InventoryClient. It models lot-level inventory so tests can
// assert the surgical property directly: unaffected lots must remain sellable.
type Fake struct {
	mu   sync.Mutex
	lots []InventoryLot
	// FailFor forces HoldLots to error for a GTIN, to exercise the FAILED path.
	FailFor map[string]error
}

func NewFake(lots ...InventoryLot) *Fake {
	return &Fake{lots: lots, FailFor: map[string]error{}}
}

func (f *Fake) HoldLots(ctx context.Context, req HoldRequest) (HoldResponse, error) {
	return f.apply(req, true)
}

func (f *Fake) ReleaseLots(ctx context.Context, req HoldRequest) (HoldResponse, error) {
	return f.apply(req, false)
}

func (f *Fake) apply(req HoldRequest, hold bool) (HoldResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err, ok := f.FailFor[req.GTIN]; ok && err != nil {
		return HoldResponse{}, err
	}
	target := map[string]bool{}
	for _, l := range req.LotCodes {
		target[strings.ToUpper(strings.TrimSpace(l))] = true
	}
	var affected, sellable int
	for i := range f.lots {
		if f.lots[i].GTIN != req.GTIN {
			continue
		}
		lotMatches := len(target) == 0 || target[strings.ToUpper(f.lots[i].LotCode)]
		if lotMatches {
			f.lots[i].Held = hold
			affected += f.lots[i].Units
		}
		if !f.lots[i].Held {
			sellable += f.lots[i].Units
		}
	}
	return HoldResponse{
		PlatformRef:       fmt.Sprintf("shopify/inventory_item/%s", req.GTIN),
		UnitsHeld:         affected,
		UnitsLeftSellable: sellable,
	}, nil
}

// Lots returns a snapshot of store inventory (test helper).
func (f *Fake) Lots() []InventoryLot {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]InventoryLot, len(f.lots))
	copy(out, f.lots)
	return out
}
