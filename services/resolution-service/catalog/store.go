package catalog

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"soteria/libs/core/matching"
	"soteria/libs/shopify"
)

// DefaultRefresh is how long a catalog snapshot is reused before the store is
// polled again. Recalls arrive in minutes, not seconds, and scoring runs against
// the whole catalog on every event, so a cached snapshot is the right shape.
const DefaultRefresh = 5 * time.Minute

// Store is a Catalog backed by the live commerce platform. Variants carry the
// lot ledger (metafield soteria.lots), which is what lets a resolution name real
// lot codes instead of guessing at them.
type Store struct {
	api     shopify.API
	refresh time.Duration
	logger  *slog.Logger
	now     func() time.Time

	mu        sync.RWMutex
	products  []Product
	fetchedAt time.Time
}

// NewStore builds a store-backed catalog. The first Candidates call populates it.
func NewStore(api shopify.API, refresh time.Duration, logger *slog.Logger) *Store {
	if refresh <= 0 {
		refresh = DefaultRefresh
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Store{api: api, refresh: refresh, logger: logger, now: time.Now}
}

func (s *Store) Candidates(ctx context.Context) ([]matching.Candidate, error) {
	products, err := s.snapshot(ctx)
	if err != nil {
		return nil, err
	}
	return toCandidates(products), nil
}

func (s *Store) ByGTIN(ctx context.Context, gtin string) (Product, bool) {
	products, err := s.snapshot(ctx)
	if err != nil {
		s.logger.Error("catalog lookup failed", "gtin", gtin, "err", err)
		return Product{}, false
	}
	want := matching.NormalizeGTIN(gtin)
	for _, p := range products {
		if matching.NormalizeGTIN(firstNonEmpty(p.GTIN, p.UPC)) == want {
			return p, true
		}
	}
	return Product{}, false
}

// snapshot returns a fresh-enough catalog, refreshing it when stale.
//
// A refresh failure falls back to the last good snapshot, because a recall
// notice scored against an empty catalog resolves to "we do not carry this",
// which is the one wrong answer we must never give by accident. With no snapshot
// at all we return the error, so the message is retried rather than silently
// treated as a miss.
func (s *Store) snapshot(ctx context.Context) ([]Product, error) {
	s.mu.RLock()
	fresh := s.fetchedAt.Add(s.refresh).After(s.now()) && len(s.products) > 0
	cached := s.products
	s.mu.RUnlock()
	if fresh {
		return cached, nil
	}

	variants, err := s.api.Catalog(ctx)
	if err != nil {
		if len(cached) > 0 {
			s.logger.Warn("catalog refresh failed, scoring against the last good snapshot",
				"age", s.now().Sub(s.fetchedAt).Truncate(time.Second), "err", err)
			return cached, nil
		}
		return nil, fmt.Errorf("catalog: no snapshot available: %w", err)
	}

	products := fromVariants(variants)
	s.mu.Lock()
	s.products, s.fetchedAt = products, s.now()
	s.mu.Unlock()
	s.logger.Info("catalog snapshot refreshed", "products", len(products))
	return products, nil
}

// fromVariants maps store variants onto catalog products, dropping variants with
// no usable barcode: without a GTIN there is nothing a recall notice can match on.
func fromVariants(variants []shopify.Variant) []Product {
	out := make([]Product, 0, len(variants))
	for _, v := range variants {
		gtin := matching.NormalizeGTIN(v.Barcode)
		if gtin == "" {
			continue
		}
		var lots []string
		for _, l := range v.Lots {
			if l.Code != "" {
				lots = append(lots, l.Code)
			}
		}
		out = append(out, Product{
			GTIN:         gtin,
			UPC:          v.Barcode,
			SKU:          v.SKU,
			Brand:        v.Vendor,
			ProductTitle: strings.TrimSpace(v.ProductTitle + " " + v.Title),
			LotCodes:     lots,
		})
	}
	return out
}
