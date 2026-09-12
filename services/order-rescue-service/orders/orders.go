// Package orders models the in-flight orders order-rescue scans, and the swap it
// applies once (and only once) the customer has confirmed.
package orders

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	"soteria/libs/core/events"
)

// Fulfillment states. Only in-flight orders are rescuable: once shipped, the
// customer gets a notification instead, handled by the notification service.
const (
	StatusPending   = "PENDING"
	StatusPicking   = "PICKING"
	StatusShipped   = "SHIPPED"
	StatusCancelled = "CANCELLED"
)

// LineItem is one order line, carrying the lot code actually allocated to it.
type LineItem struct {
	LineItemID   string       `json:"line_item_id"`
	GTIN         string       `json:"gtin"`
	SKU          string       `json:"sku,omitempty"`
	ProductTitle string       `json:"product_title,omitempty"`
	LotCode      string       `json:"lot_code"`
	Quantity     int          `json:"quantity"`
	UnitPrice    events.Money `json:"unit_price"`
	SwappedTo    string       `json:"swapped_to,omitempty"`
}

type Order struct {
	OrderID   string          `json:"order_id"`
	Status    string          `json:"status"`
	Customer  events.Customer `json:"customer"`
	LineItems []LineItem      `json:"line_items"`
}

// InFlight reports whether the order can still be altered before it ships.
func (o Order) InFlight() bool {
	return o.Status == StatusPending || o.Status == StatusPicking
}

// Repository is the order seam. Backed by a JSON snapshot in v1; the Shopify
// Orders API implementation drops in behind the same interface.
type Repository interface {
	// AffectedLines returns in-flight lines matching the GTIN and (when lotCodes
	// is non-empty) one of those lot codes.
	AffectedLines(ctx context.Context, gtin string, lotCodes []string) ([]Affected, error)
	// ApplySwap replaces the line's product after customer confirmation.
	ApplySwap(ctx context.Context, orderID, lineItemID, substituteGTIN string) error
	// Cancel marks an order cancelled (customer chose refund/cancel).
	Cancel(ctx context.Context, orderID string) error
}

// Affected pairs an order with the line that must be rescued.
type Affected struct {
	Order Order
	Line  LineItem
}

// Memory is an in-memory Repository.
type Memory struct {
	mu     sync.RWMutex
	orders []Order
}

func NewMemory(orders ...Order) *Memory { return &Memory{orders: orders} }

// LoadFile reads a JSON array of Order.
func LoadFile(path string) (*Memory, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("orders: read %s: %w", path, err)
	}
	var orders []Order
	if err := json.Unmarshal(raw, &orders); err != nil {
		return nil, fmt.Errorf("orders: parse %s: %w", path, err)
	}
	return NewMemory(orders...), nil
}

func (m *Memory) AffectedLines(ctx context.Context, gtin string, lotCodes []string) ([]Affected, error) {
	lots := map[string]bool{}
	for _, l := range lotCodes {
		lots[strings.ToUpper(strings.TrimSpace(l))] = true
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []Affected
	for _, o := range m.orders {
		if !o.InFlight() {
			continue
		}
		for _, li := range o.LineItems {
			if li.GTIN != gtin || li.SwappedTo != "" {
				continue
			}
			// Empty lots == SKU-scope containment: every unit of the product is affected.
			if len(lots) > 0 && !lots[strings.ToUpper(strings.TrimSpace(li.LotCode))] {
				continue
			}
			out = append(out, Affected{Order: o, Line: li})
		}
	}
	return out, nil
}

func (m *Memory) ApplySwap(ctx context.Context, orderID, lineItemID, substituteGTIN string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.orders {
		if m.orders[i].OrderID != orderID {
			continue
		}
		for j := range m.orders[i].LineItems {
			if m.orders[i].LineItems[j].LineItemID == lineItemID {
				m.orders[i].LineItems[j].SwappedTo = substituteGTIN
				return nil
			}
		}
	}
	return fmt.Errorf("orders: line %s not found on order %s", lineItemID, orderID)
}

func (m *Memory) Cancel(ctx context.Context, orderID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.orders {
		if m.orders[i].OrderID == orderID {
			m.orders[i].Status = StatusCancelled
			return nil
		}
	}
	return fmt.Errorf("orders: order %s not found", orderID)
}

// Orders returns a snapshot (test helper).
func (m *Memory) Orders() []Order {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Order, len(m.orders))
	copy(out, m.orders)
	return out
}
