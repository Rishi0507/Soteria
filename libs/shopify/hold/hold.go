// Package hold adapts the Shopify client to the containment-service's seam,
// core/shopify.InventoryClient: "hold these lots of this GTIN, or the whole
// SKU when no lots are given".
//
// Shopify has no lot tracking, so the adapter composes three primitives:
//
//   - the variant's soteria.lots metafield is the lot ledger (code → units);
//   - inventoryMoveQuantities moves the held lots' units from the selling
//     location to the Quarantine location, so unaffected lots stay sellable
//     and the movement is reversible;
//   - product tags and the soteria.badge metafield tell the storefront what
//     happened: RECALL_HAZARD tag and DRAFT status for a full hold; a
//     RECALL_LOT:<code> tag and the "Verified Safe Lot" badge for a partial one.
//
// A variant with no lot data is held as a whole SKU. Lot codes are compared
// case-insensitively after trimming, matching core/shopify.Fake.
package hold

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	core "soteria/libs/core/shopify"
	"soteria/libs/shopify"
)

// Tags and badge text written to the store.
const (
	TagHazard    = "RECALL_HAZARD"
	TagLotPrefix = "RECALL_LOT:"
	BadgeSafeLot = "Verified Safe Lot — remaining stock checked against the recall notice"
	reason       = "quality_control"
)

// Adapter implements core/shopify.InventoryClient over shopify.API.
type Adapter struct {
	API        shopify.API
	SellingLoc string // location online orders are fulfilled from
	QuarantLoc string // the Quarantine location
	// ManageStatus also unpublishes (DRAFT) on a full hold and republishes
	// (ACTIVE) on a full release. On by default via New.
	ManageStatus bool
}

// New returns an adapter for the two locations.
func New(api shopify.API, sellingLocationID, quarantineLocationID string) *Adapter {
	return &Adapter{API: api, SellingLoc: sellingLocationID, QuarantLoc: quarantineLocationID, ManageStatus: true}
}

// ResolveLocations finds the selling location (first active one that
// fulfills online orders) and the location named quarantineName.
func ResolveLocations(ctx context.Context, api shopify.API, quarantineName string) (selling, quarantine string, err error) {
	locs, err := api.Locations(ctx)
	if err != nil {
		return "", "", err
	}
	for _, l := range locs {
		switch {
		case strings.EqualFold(l.Name, quarantineName):
			quarantine = l.ID
		case selling == "" && l.IsActive && l.Fulfills:
			selling = l.ID
		}
	}
	if selling == "" {
		return "", "", errors.New("hold: no active location that fulfills online orders")
	}
	if quarantine == "" {
		return "", "", fmt.Errorf("hold: no location named %q — create it in Shopify admin", quarantineName)
	}
	return selling, quarantine, nil
}

// HoldLots moves the requested lots (or everything) to Quarantine.
func (a *Adapter) HoldLots(ctx context.Context, req core.HoldRequest) (core.HoldResponse, error) {
	return a.apply(ctx, req, true)
}

// ReleaseLots moves previously held lots back to the selling location.
func (a *Adapter) ReleaseLots(ctx context.Context, req core.HoldRequest) (core.HoldResponse, error) {
	return a.apply(ctx, req, false)
}

func (a *Adapter) apply(ctx context.Context, req core.HoldRequest, hold bool) (core.HoldResponse, error) {
	v, err := a.find(ctx, req)
	if err != nil {
		return core.HoldResponse{}, err
	}
	target := normalize(req.LotCodes)
	wholeSKU := len(target) == 0 || len(v.Lots) == 0

	// Which lots change state, and how many units that is.
	var units int
	var changed []string
	lots := append([]shopify.Lot(nil), v.Lots...)
	for i := range lots {
		code := strings.ToUpper(strings.TrimSpace(lots[i].Code))
		if !wholeSKU && !target[code] {
			continue
		}
		if lots[i].Held == hold {
			continue // already in the requested state; idempotent
		}
		lots[i].Held = hold
		units += lots[i].Units
		changed = append(changed, lots[i].Code)
	}
	if len(v.Lots) == 0 {
		// No ledger: the whole sellable (or whole quarantined) quantity moves.
		if hold {
			units = v.AvailableAt(a.SellingLoc)
		} else {
			units = v.AvailableAt(a.QuarantLoc)
		}
	} else if !wholeSKU && len(changed) == 0 && !anyKnown(lots, target) {
		return core.HoldResponse{}, fmt.Errorf("hold: none of lots %v exist on %s (ledger has %v)", req.LotCodes, v.Barcode, codes(lots))
	}

	// Never move more than is physically at the source.
	from, to := a.SellingLoc, a.QuarantLoc
	if !hold {
		from, to = a.QuarantLoc, a.SellingLoc
	}
	if avail := v.AvailableAt(from); units > avail {
		units = avail
	}
	if units > 0 {
		if err := a.API.MoveAvailable(ctx, v.InventoryItemID, from, to, units, reason); err != nil {
			return core.HoldResponse{}, err
		}
	}

	// Persist the ledger state and storefront signals.
	if len(v.Lots) > 0 {
		b, _ := json.Marshal(lots)
		if err := a.API.SetMetafields(ctx, []shopify.Metafield{{OwnerID: v.ID, Namespace: shopify.MetafieldNamespace, Key: shopify.LotsKey, Type: "json", Value: string(b)}}); err != nil {
			return core.HoldResponse{}, err
		}
	}
	allHeld := len(lots) > 0 && countHeld(lots) == len(lots) || (len(lots) == 0 && hold)
	if err := a.signal(ctx, v, lots, changed, hold, allHeld); err != nil {
		return core.HoldResponse{}, err
	}

	sellable := v.AvailableAt(a.SellingLoc)
	if hold {
		sellable -= units
	} else {
		sellable += units
	}
	return core.HoldResponse{PlatformRef: v.InventoryItemID, UnitsHeld: units, UnitsLeftSellable: sellable}, nil
}

// signal writes tags, badge and status so the storefront reflects the hold.
func (a *Adapter) signal(ctx context.Context, v shopify.Variant, lots []shopify.Lot, changed []string, hold, allHeld bool) error {
	lotTags := make([]string, 0, len(changed))
	for _, c := range changed {
		lotTags = append(lotTags, TagLotPrefix+c)
	}
	switch {
	case hold && allHeld:
		if err := a.API.AddTags(ctx, v.ProductID, append([]string{TagHazard}, lotTags...)); err != nil {
			return err
		}
		if err := a.API.SetMetafields(ctx, []shopify.Metafield{badge(v.ProductID, "")}); err != nil {
			return err
		}
		if a.ManageStatus {
			return a.API.SetProductStatus(ctx, v.ProductID, shopify.StatusDraft)
		}
	case hold:
		if err := a.API.AddTags(ctx, v.ProductID, lotTags); err != nil {
			return err
		}
		return a.API.SetMetafields(ctx, []shopify.Metafield{badge(v.ProductID, BadgeSafeLot)})
	default: // release
		if len(lotTags) > 0 {
			if err := a.API.RemoveTags(ctx, v.ProductID, lotTags); err != nil {
				return err
			}
		}
		if countHeld(lots) == 0 {
			if err := a.API.RemoveTags(ctx, v.ProductID, []string{TagHazard}); err != nil {
				return err
			}
			if err := a.API.SetMetafields(ctx, []shopify.Metafield{badge(v.ProductID, "")}); err != nil {
				return err
			}
			if a.ManageStatus && v.ProductStatus == shopify.StatusDraft {
				return a.API.SetProductStatus(ctx, v.ProductID, shopify.StatusActive)
			}
		} else {
			return a.API.SetMetafields(ctx, []shopify.Metafield{badge(v.ProductID, BadgeSafeLot)})
		}
	}
	return nil
}

func badge(productID, text string) shopify.Metafield {
	return shopify.Metafield{OwnerID: productID, Namespace: shopify.MetafieldNamespace, Key: shopify.BadgeKey, Type: "single_line_text_field", Value: text}
}

// find locates the variant by GTIN (barcode), falling back to SKU via the
// catalog when the barcode is unknown to the store.
// barcodeForms lists the equivalent ways one GTIN may be written in a store's
// Barcode field.
//
// Barcode lookup is an exact string match, but a GTIN-14 ("00041196910537"), an
// EAN-13 and the UPC-A printed on the pack ("041196910537") are the same
// product. Callers normalize to GTIN-14 while merchants type the printed form,
// so asking for only one spelling finds nothing and the hold silently falls
// through to the SKU match, or fails outright when the SKU is blank.
func barcodeForms(gtin string) []string {
	digits := nonDigits.ReplaceAllString(gtin, "")
	if digits == "" {
		return []string{gtin}
	}
	seen := map[string]bool{}
	var forms []string
	add := func(v string) {
		if v != "" && !seen[v] {
			seen[v] = true
			forms = append(forms, v)
		}
	}
	add(gtin)
	add(digits)
	for _, width := range []int{14, 13, 12, 8} {
		switch {
		case len(digits) == width:
		case len(digits) > width:
			// Only leading zeros may be dropped: losing a significant digit would
			// name a different product.
			if strings.Trim(digits[:len(digits)-width], "0") == "" {
				add(digits[len(digits)-width:])
			}
		default:
			add(strings.Repeat("0", width-len(digits)) + digits)
		}
	}
	return forms
}

var nonDigits = regexp.MustCompile(`\D`)

func (a *Adapter) find(ctx context.Context, req core.HoldRequest) (shopify.Variant, error) {
	if req.GTIN != "" {
		vs, err := a.API.VariantsByBarcode(ctx, barcodeForms(req.GTIN))
		if err != nil {
			return shopify.Variant{}, err
		}
		if len(vs) == 1 {
			return vs[0], nil
		}
		if len(vs) > 1 {
			return shopify.Variant{}, fmt.Errorf("hold: barcode %s matches %d variants; refusing to guess", req.GTIN, len(vs))
		}
	}
	if req.SKU != "" {
		all, err := a.API.Catalog(ctx)
		if err != nil {
			return shopify.Variant{}, err
		}
		for _, v := range all {
			if strings.EqualFold(v.SKU, req.SKU) {
				return v, nil
			}
		}
	}
	return shopify.Variant{}, fmt.Errorf("hold: no variant with barcode %q or sku %q", req.GTIN, req.SKU)
}

func normalize(codes []string) map[string]bool {
	m := map[string]bool{}
	for _, c := range codes {
		if c = strings.ToUpper(strings.TrimSpace(c)); c != "" {
			m[c] = true
		}
	}
	return m
}

func anyKnown(lots []shopify.Lot, target map[string]bool) bool {
	for _, l := range lots {
		if target[strings.ToUpper(strings.TrimSpace(l.Code))] {
			return true
		}
	}
	return false
}

func countHeld(lots []shopify.Lot) int {
	n := 0
	for _, l := range lots {
		if l.Held {
			n++
		}
	}
	return n
}

func codes(lots []shopify.Lot) []string {
	out := make([]string, 0, len(lots))
	for _, l := range lots {
		out = append(out, l.Code)
	}
	return out
}

var _ core.InventoryClient = (*Adapter)(nil)
