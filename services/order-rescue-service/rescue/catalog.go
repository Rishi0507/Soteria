package rescue

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"soteria/libs/core/events"
)

// Substitute is a product that could replace an affected line.
type Substitute struct {
	GTIN         string       `json:"gtin"`
	SKU          string       `json:"sku,omitempty"`
	ProductTitle string       `json:"product_title"`
	Category     string       `json:"category"`
	UnitPrice    events.Money `json:"unit_price"`
	InStock      int          `json:"in_stock"`
}

// SubstituteSource supplies replacement candidates for a product.
type SubstituteSource interface {
	// Alternatives returns in-stock products in the same category as gtin,
	// excluding gtin itself.
	Alternatives(ctx context.Context, gtin string) ([]Substitute, error)
}

// MemoryCatalog is a JSON-backed SubstituteSource.
type MemoryCatalog struct {
	mu    sync.RWMutex
	items []Substitute
}

func NewMemoryCatalog(items ...Substitute) *MemoryCatalog { return &MemoryCatalog{items: items} }

func LoadCatalog(path string) (*MemoryCatalog, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("rescue: read %s: %w", path, err)
	}
	var items []Substitute
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("rescue: parse %s: %w", path, err)
	}
	return NewMemoryCatalog(items...), nil
}

func (c *MemoryCatalog) Alternatives(ctx context.Context, gtin string) ([]Substitute, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var category string
	for _, it := range c.items {
		if it.GTIN == gtin {
			category = it.Category
			break
		}
	}
	if category == "" {
		return nil, nil
	}
	var out []Substitute
	for _, it := range c.items {
		if it.GTIN != gtin && it.Category == category && it.InStock > 0 {
			out = append(out, it)
		}
	}
	return out, nil
}
