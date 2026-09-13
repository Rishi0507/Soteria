package orders

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"

	"soteria/libs/core/events"
	"soteria/libs/core/matching"
	"soteria/libs/shopify"
)

// ErrUnsupported is returned for an action the commerce platform client does not
// expose yet. The rescue service records the customer's decision regardless: the
// confirmation event is the instruction of record, and swallowing the gap would
// be worse than reporting it.
var ErrUnsupported = errors.New("orders: operation not supported by the commerce client")

// DefaultLookback bounds the affected-order scan. Orders older than this are
// assumed shipped and belong to the notification path, not the rescue path.
const DefaultLookback = 30 * 24 * time.Hour

// Store is a Repository backed by the live commerce platform.
type Store struct {
	api      shopify.API
	currency string
	lookback time.Duration
	logger   *slog.Logger
	now      func() time.Time
}

func NewStore(api shopify.API, currency string, lookback time.Duration, logger *slog.Logger) *Store {
	if currency == "" {
		currency = "USD"
	}
	if lookback <= 0 {
		lookback = DefaultLookback
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Store{api: api, currency: currency, lookback: lookback, logger: logger, now: time.Now}
}

// AffectedLines finds in-flight order lines for the recalled product.
//
// Per-line lot attribution does not exist in the commerce platform: an order line
// records a variant, not the lot that was picked for it. So when the held lots are
// only part of the variant's ledger, we cannot prove which orders got a bad unit.
// We include them all and say so in the lot code, because a customer offered an
// optional substitute can decline, while a customer we skip may eat a recalled
// product. Over-inclusion is recoverable; under-inclusion is not.
func (s *Store) AffectedLines(ctx context.Context, gtin string, lotCodes []string) ([]Affected, error) {
	variants, err := s.api.VariantsByBarcode(ctx, barcodeForms(gtin))
	if err != nil {
		return nil, fmt.Errorf("orders: look up variant for %s: %w", gtin, err)
	}
	variant, ok := pickVariant(variants, gtin)
	if !ok {
		s.logger.Warn("no variant matches the contained product, no orders to rescue", "gtin", gtin)
		return nil, nil
	}

	attributed, lotLabel := attributeLots(variant, lotCodes)
	if !attributed {
		s.logger.Info("lot attribution unavailable for order lines, including every in-flight line",
			"gtin", gtin, "held_lots", strings.Join(lotCodes, ","))
	}

	orders, err := s.api.OrdersSince(ctx, s.now().Add(-s.lookback))
	if err != nil {
		return nil, fmt.Errorf("orders: scan orders for %s: %w", gtin, err)
	}

	unitPrice := events.Money{AmountMinor: minorUnits(variant.Price), Currency: s.currency}
	var out []Affected
	for _, o := range orders {
		if !inFlight(o) {
			continue
		}
		for _, li := range o.LineItems {
			if li.VariantID != variant.ID {
				continue
			}
			out = append(out, Affected{
				Order: Order{
					OrderID:  o.ID,
					Status:   fulfillmentToStatus(o.FulfillmentStatus),
					Customer: events.Customer{CustomerID: o.CustomerID, Email: o.Email},
				},
				Line: LineItem{
					LineItemID:   li.ID,
					GTIN:         matching.NormalizeGTIN(variant.Barcode),
					SKU:          firstNonEmpty(li.SKU, variant.SKU),
					ProductTitle: firstNonEmpty(li.Title, variant.ProductTitle),
					LotCode:      lotLabel,
					Quantity:     li.Quantity,
					UnitPrice:    unitPrice,
				},
			})
		}
	}
	return out, nil
}

// ApplySwap replaces the affected line with the substitute the customer chose.
func (s *Store) ApplySwap(ctx context.Context, orderID, lineItemID, substituteGTIN string) error {
	variants, err := s.api.VariantsByBarcode(ctx, barcodeForms(substituteGTIN))
	if err != nil {
		return fmt.Errorf("orders: look up substitute %s: %w", substituteGTIN, err)
	}
	variant, ok := pickVariant(variants, substituteGTIN)
	if !ok {
		return fmt.Errorf("orders: substitute %s is not a variant in this store", substituteGTIN)
	}
	quantity, err := s.lineQuantity(ctx, orderID, lineItemID)
	if err != nil {
		return err
	}
	// notify=true: the customer already asked for this swap, so the platform's
	// own confirmation email is wanted, not a surprise.
	if err := s.api.ReplaceLineItem(ctx, orderID, lineItemID, variant.ID, quantity, true); err != nil {
		return fmt.Errorf("orders: replace line %s on %s: %w", lineItemID, orderID, err)
	}
	return nil
}

// Cancel is not available: the shared commerce client exposes no order
// cancellation or refund mutation yet. The customer's choice is still recorded
// and published, and the retailer acts on it through their payment flow.
func (s *Store) Cancel(ctx context.Context, orderID string) error {
	return fmt.Errorf("orders: cancel order %s: %w", orderID, ErrUnsupported)
}

func (s *Store) lineQuantity(ctx context.Context, orderID, lineItemID string) (int, error) {
	orders, err := s.api.OrdersSince(ctx, s.now().Add(-s.lookback))
	if err != nil {
		return 0, fmt.Errorf("orders: reload order %s: %w", orderID, err)
	}
	for _, o := range orders {
		if o.ID != orderID {
			continue
		}
		for _, li := range o.LineItems {
			if li.ID == lineItemID {
				return li.Quantity, nil
			}
		}
	}
	return 0, fmt.Errorf("orders: line %s not found on order %s", lineItemID, orderID)
}

// attributeLots reports whether a single held lot can be named for every line of
// this variant, and the label to record on the rescue.
func attributeLots(v shopify.Variant, heldLots []string) (attributed bool, label string) {
	held := map[string]bool{}
	for _, l := range heldLots {
		if c := strings.ToUpper(strings.TrimSpace(l)); c != "" {
			held[c] = true
		}
	}
	switch {
	case len(held) == 0, len(v.Lots) == 0:
		// Whole-SKU containment, or a store with no lot ledger: every unit of the
		// variant is affected, which needs no attribution.
		return true, wholeSKULabel(heldLots)
	case len(held) == 1:
		for c := range held {
			return true, c
		}
	}
	// More than one lot held and the ledger has lots we did not hold: which lot
	// reached which order is unknowable from the platform.
	ledgerFullyHeld := true
	for _, l := range v.Lots {
		if !held[strings.ToUpper(strings.TrimSpace(l.Code))] {
			ledgerFullyHeld = false
			break
		}
	}
	if ledgerFullyHeld {
		return true, wholeSKULabel(heldLots)
	}
	return false, LotUnattributed
}

// LotUnattributed marks a rescue whose affected lot could not be pinned down.
// Downstream (notification copy, audit dossier) must read this as "one of the
// recalled lots", never as a specific lot.
const LotUnattributed = "UNATTRIBUTED"

func wholeSKULabel(heldLots []string) string {
	if len(heldLots) == 1 {
		return strings.ToUpper(strings.TrimSpace(heldLots[0]))
	}
	return LotUnattributed
}

// barcodeForms lists the equivalent ways a store may have typed one GTIN.
// Barcode lookup is an exact string match, but a GTIN-14 ("00041196910537"), an
// EAN-13 and a UPC-A ("041196910537") are the same product, so we ask for every
// zero-padded width rather than guessing which one the merchant used.
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
	add(digits)
	for _, width := range []int{14, 13, 12, 8} {
		switch {
		case len(digits) == width:
		case len(digits) > width:
			// Only drop leading zeros: dropping a significant digit would be a
			// different product.
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

func pickVariant(variants []shopify.Variant, gtin string) (shopify.Variant, bool) {
	want := matching.NormalizeGTIN(gtin)
	for _, v := range variants {
		if matching.NormalizeGTIN(v.Barcode) == want {
			return v, true
		}
	}
	return shopify.Variant{}, false
}

// inFlight keeps orders that can still be altered before they ship.
func inFlight(o shopify.Order) bool {
	switch strings.ToUpper(o.FulfillmentStatus) {
	case "UNFULFILLED", "PARTIALLY_FULFILLED", "IN_PROGRESS", "SCHEDULED", "ON_HOLD", "":
		return true
	default:
		return false
	}
}

func fulfillmentToStatus(fulfillment string) string {
	if strings.EqualFold(fulfillment, "PARTIALLY_FULFILLED") {
		return StatusPicking
	}
	return StatusPending
}

// minorUnits converts Shopify's decimal string price ("4.49") to minor units.
func minorUnits(price string) int {
	p := strings.TrimSpace(price)
	if p == "" {
		return 0
	}
	f, err := strconv.ParseFloat(p, 64)
	if err != nil {
		return 0
	}
	return int(f*100 + 0.5)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
