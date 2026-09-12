// Package offacts is the allergen seam the order-rescue-service cross-checks
// substitutes against.
//
// The Open Food Facts integration layer itself is owned by the storefront
// workstream and its response shape is a shared schema. This package
// declares the lookup interface order-rescue depends on, an HTTP client against
// the public API, and a Static provider for tests and offline demos.
package offacts

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Product is the enrichment subset order-rescue needs. Mirrors the shared
// Open Food Facts schema in /contracts (allergen tags are normalized to bare
// English names: "peanuts", "milk", ...).
type Product struct {
	GTIN      string   `json:"gtin"`
	Name      string   `json:"name"`
	Brand     string   `json:"brand"`
	Allergens []string `json:"allergens"`
	Traces    []string `json:"traces"`
	Found     bool     `json:"found"`
}

// Provider looks up product enrichment by GTIN.
type Provider interface {
	Lookup(ctx context.Context, gtin string) (Product, error)
}

// AllergenSet is the union of declared allergens and "may contain" traces:
// for a recall substitute, a trace is not an acceptable risk.
func (p Product) AllergenSet() map[string]bool {
	out := map[string]bool{}
	for _, a := range append(append([]string{}, p.Allergens...), p.Traces...) {
		if n := Normalize(a); n != "" {
			out[n] = true
		}
	}
	return out
}

// Normalize strips Open Food Facts tag prefixes ("en:peanuts") and lowercases.
func Normalize(tag string) string {
	t := strings.ToLower(strings.TrimSpace(tag))
	if i := strings.Index(t, ":"); i >= 0 && i <= 3 {
		t = t[i+1:]
	}
	return strings.TrimSpace(strings.ReplaceAll(t, "_", " "))
}

// --------------------------------------------------------------------- static

// Static is an in-memory Provider keyed by GTIN.
type Static struct {
	mu       sync.RWMutex
	products map[string]Product
}

func NewStatic(products ...Product) *Static {
	s := &Static{products: map[string]Product{}}
	for _, p := range products {
		p.Found = true
		s.products[p.GTIN] = p
	}
	return s
}

func (s *Static) Lookup(ctx context.Context, gtin string) (Product, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if p, ok := s.products[gtin]; ok {
		return p, nil
	}
	return Product{GTIN: gtin, Found: false}, nil
}

// --------------------------------------------------------------------- http

// HTTPClient calls the public Open Food Facts API. A lookup that fails or returns
// nothing yields Found=false, which order-rescue treats as "not allergen-safe" -
// missing data must never be read as a clean bill of health.
type HTTPClient struct {
	BaseURL string
	HTTP    *http.Client
}

func NewHTTPClient() *HTTPClient {
	return &HTTPClient{
		BaseURL: "https://world.openfoodfacts.org",
		HTTP:    &http.Client{Timeout: 5 * time.Second},
	}
}

func (c *HTTPClient) Lookup(ctx context.Context, gtin string) (Product, error) {
	url := fmt.Sprintf("%s/api/v2/product/%s.json?fields=code,product_name,brands,allergens_tags,traces_tags", c.BaseURL, gtin)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Product{GTIN: gtin}, err
	}
	req.Header.Set("User-Agent", "Soteria/1.0 (recall-containment)")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return Product{GTIN: gtin}, fmt.Errorf("offacts: lookup %s: %w", gtin, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Product{GTIN: gtin, Found: false}, nil
	}
	var body struct {
		Status  int `json:"status"`
		Product struct {
			Code          string   `json:"code"`
			ProductName   string   `json:"product_name"`
			Brands        string   `json:"brands"`
			AllergensTags []string `json:"allergens_tags"`
			TracesTags    []string `json:"traces_tags"`
		} `json:"product"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return Product{GTIN: gtin}, fmt.Errorf("offacts: decode %s: %w", gtin, err)
	}
	if body.Status != 1 {
		return Product{GTIN: gtin, Found: false}, nil
	}
	return Product{
		GTIN:      gtin,
		Name:      body.Product.ProductName,
		Brand:     body.Product.Brands,
		Allergens: body.Product.AllergensTags,
		Traces:    body.Product.TracesTags,
		Found:     true,
	}, nil
}
