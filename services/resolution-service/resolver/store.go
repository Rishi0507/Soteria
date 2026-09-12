package resolver

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"soteria/libs/core/events"
	"soteria/libs/core/matching"
)

// Verdicts returned by LotStatus.
const (
	VerdictSafe       = "SAFE"
	VerdictAffected   = "AFFECTED"
	VerdictUnknownLot = "UNKNOWN_LOT"
)

// LedgerFunc reports every lot code the catalog knows for a product. Without it
// the store cannot tell a clean lot from a lot it has never seen, so it answers
// UNKNOWN_LOT for both.
type LedgerFunc func(ctx context.Context, gtin string) []string

// Store holds resolutions and answers the storefront "Verified Safe Lot" query.
// In-memory by design for v1; the interface is narrow enough to back with Postgres
// without touching callers.
type Store struct {
	mu          sync.RWMutex
	resolutions []events.LotResolved
	ledger      LedgerFunc
}

func NewStore() *Store { return &Store{} }

// SetLedger supplies the catalog lookup used to recognize unaffected lots.
func (s *Store) SetLedger(f LedgerFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ledger = f
}

// Put records a resolution, replacing an earlier one for the same incident+GTIN set.
func (s *Store) Put(r events.LotResolved) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, existing := range s.resolutions {
		if existing.IncidentID == r.IncidentID {
			s.resolutions[i] = r
			return
		}
	}
	s.resolutions = append(s.resolutions, r)
}

// List returns resolutions newest-first, optionally filtered.
func (s *Store) List(incidentID, gtin string, limit int) []events.LotResolved {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []events.LotResolved
	for _, r := range s.resolutions {
		if incidentID != "" && r.IncidentID != incidentID {
			continue
		}
		if gtin != "" && !hasGTIN(r, matching.NormalizeGTIN(gtin)) {
			continue
		}
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ResolvedAt.After(out[j].ResolvedAt) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// LotStatus is the Verified Safe Lot verdict for one unit on the shelf.
type LotStatus struct {
	GTIN       string    `json:"gtin"`
	LotCode    string    `json:"lot_code,omitempty"`
	Verdict    string    `json:"verdict"`
	IncidentID string    `json:"incident_id,omitempty"`
	Hazard     string    `json:"hazard,omitempty"`
	Confidence float64   `json:"confidence,omitempty"`
	CheckedAt  time.Time `json:"checked_at"`
}

// LotStatus answers: is this GTIN + printed lot code affected by a live incident?
//
// A SKU-scope resolution taints every unit. A LOT-scope resolution taints only the
// listed lots - that is the surgical promise. A lot the catalog does not recognize
// returns UNKNOWN_LOT rather than SAFE: on a product with a live incident we will
// not vouch for a unit we cannot identify, and a mistyped or unlisted code is
// exactly the case where a false "safe" would be dangerous.
func (s *Store) LotStatus(ctx context.Context, gtin, lotCode string) LotStatus {
	norm := matching.NormalizeGTIN(gtin)
	lot := strings.ToUpper(strings.TrimSpace(lotCode))
	out := LotStatus{GTIN: defaultString(norm, gtin), LotCode: lot, Verdict: VerdictSafe, CheckedAt: time.Now().UTC()}

	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, r := range s.resolutions {
		for _, m := range r.Matches {
			if m.GTIN != norm || norm == "" {
				continue
			}
			if r.Scope == events.ScopeSKU || len(m.LotCodes) == 0 {
				return withIncident(out, r, m, VerdictAffected)
			}
			if lot == "" {
				return withIncident(out, r, m, VerdictUnknownLot)
			}
			known := false
			for _, l := range m.LotCodes {
				if strings.EqualFold(strings.TrimSpace(l), lot) {
					known = true
					break
				}
			}
			if known {
				return withIncident(out, r, m, VerdictAffected)
			}
			// Not a recalled lot. Only the catalog can say whether it is a lot we
			// actually carry (clean) or one we have never heard of.
			if !s.knownToCatalog(ctx, norm, lot) {
				return withIncident(out, r, m, VerdictUnknownLot)
			}
		}
	}
	return out
}

// knownToCatalog reports whether the product's lot ledger contains this code.
// With no ledger configured we cannot confirm the lot, so we report false and let
// the caller answer UNKNOWN_LOT.
func (s *Store) knownToCatalog(ctx context.Context, gtin, lot string) bool {
	if s.ledger == nil {
		return false
	}
	for _, l := range s.ledger(ctx, gtin) {
		if strings.EqualFold(strings.TrimSpace(l), lot) {
			return true
		}
	}
	return false
}

func withIncident(out LotStatus, r events.LotResolved, m events.Match, verdict string) LotStatus {
	out.Verdict = verdict
	out.IncidentID = r.IncidentID
	out.Hazard = r.Hazard
	out.Confidence = m.Confidence
	return out
}

func hasGTIN(r events.LotResolved, gtin string) bool {
	for _, m := range r.Matches {
		if m.GTIN == gtin {
			return true
		}
	}
	return false
}
