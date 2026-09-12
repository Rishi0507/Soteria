// Package catalog is the retailer product catalog the resolution-service scores
// recall signals against.
package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"soteria/libs/core/matching"
)

// Product is one catalog line, including the lot codes currently in inventory -
// lot data is what makes surgical containment possible.
type Product struct {
	GTIN         string   `json:"gtin"`
	UPC          string   `json:"upc,omitempty"`
	SKU          string   `json:"sku"`
	Brand        string   `json:"brand"`
	ProductTitle string   `json:"product_title"`
	LotCodes     []string `json:"lot_codes,omitempty"`
}

// Catalog supplies matching candidates. Backed by a JSON snapshot today; swap for
// the retailer's product DB without touching the resolver.
type Catalog interface {
	Candidates(ctx context.Context) ([]matching.Candidate, error)
	ByGTIN(ctx context.Context, gtin string) (Product, bool)
}

// Static is an in-memory Catalog.
type Static struct {
	mu       sync.RWMutex
	products []Product
}

func NewStatic(products ...Product) *Static {
	return &Static{products: products}
}

// LoadFile reads a JSON array of Product.
func LoadFile(path string) (*Static, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("catalog: read %s: %w", path, err)
	}
	var products []Product
	if err := json.Unmarshal(raw, &products); err != nil {
		return nil, fmt.Errorf("catalog: parse %s: %w", path, err)
	}
	return NewStatic(products...), nil
}

func (s *Static) Candidates(ctx context.Context) ([]matching.Candidate, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]matching.Candidate, 0, len(s.products))
	for _, p := range s.products {
		out = append(out, matching.Candidate{
			GTIN:         matching.NormalizeGTIN(firstNonEmpty(p.GTIN, p.UPC)),
			UPC:          p.UPC,
			SKU:          p.SKU,
			Brand:        p.Brand,
			ProductTitle: p.ProductTitle,
			LotCodes:     p.LotCodes,
		})
	}
	return out, nil
}

func (s *Static) ByGTIN(ctx context.Context, gtin string) (Product, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	want := matching.NormalizeGTIN(gtin)
	for _, p := range s.products {
		if matching.NormalizeGTIN(firstNonEmpty(p.GTIN, p.UPC)) == want {
			return p, true
		}
	}
	return Product{}, false
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
