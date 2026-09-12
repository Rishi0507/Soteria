package resolver

import (
	"context"
	"testing"
	"time"

	"soteria/libs/core/events"
)

// seedResolution records a lot-level incident on one product that carries three
// lots, two of them recalled.
func seedResolution(t *testing.T, withLedger bool) *Store {
	t.Helper()
	s := NewStore()
	if withLedger {
		s.SetLedger(func(ctx context.Context, gtin string) []string {
			if gtin == "00041196910537" {
				return []string{"8H-1132", "8H-1133", "8H-2000"}
			}
			return nil
		})
	}
	s.Put(events.LotResolved{
		IncidentID: "inc-1", ResolutionID: events.NewID(), ResolvedAt: time.Now().UTC(),
		Confidence: 0.99, Scope: events.ScopeLot, Hazard: "undeclared peanut",
		Matches: []events.Match{{
			GTIN: "00041196910537", LotCodes: []string{"8H-1132", "8H-1133"}, Confidence: 0.99,
		}},
	})
	return s
}

func TestLotStatusVerdicts(t *testing.T) {
	s := seedResolution(t, true)
	ctx := context.Background()

	cases := []struct{ lot, want string }{
		{"8H-1132", VerdictAffected},   // recalled
		{"8h-1133", VerdictAffected},   // recalled, case-insensitive
		{"8H-2000", VerdictSafe},       // carried and not recalled
		{"ZZ-0000", VerdictUnknownLot}, // not a lot we carry: never claim safe
		{"", VerdictUnknownLot},        // no code on the pack
	}
	for _, c := range cases {
		if got := s.LotStatus(ctx, "041196910537", c.lot).Verdict; got != c.want {
			t.Errorf("LotStatus(%q) = %s, want %s", c.lot, got, c.want)
		}
	}

	if got := s.LotStatus(ctx, "012345678905", "K221").Verdict; got != VerdictSafe {
		t.Errorf("a product with no incident is SAFE, got %s", got)
	}
}

// TestLotStatusWithoutLedgerNeverClaimsSafe: with no catalog behind it, the store
// cannot recognize a clean lot, so it must say UNKNOWN_LOT rather than guess.
func TestLotStatusWithoutLedgerNeverClaimsSafe(t *testing.T) {
	s := seedResolution(t, false)
	if got := s.LotStatus(context.Background(), "041196910537", "8H-2000").Verdict; got != VerdictUnknownLot {
		t.Fatalf("verdict = %s, want UNKNOWN_LOT", got)
	}
}

// TestSKUScopeTaintsEveryLot: with no lot codes recovered, every unit is affected.
func TestSKUScopeTaintsEveryLot(t *testing.T) {
	s := NewStore()
	s.Put(events.LotResolved{
		IncidentID: "inc-2", ResolvedAt: time.Now().UTC(), Scope: events.ScopeSKU, Confidence: 0.96,
		Matches: []events.Match{{GTIN: "00041196910537", Confidence: 0.96}},
	})
	if got := s.LotStatus(context.Background(), "041196910537", "8H-2000").Verdict; got != VerdictAffected {
		t.Fatalf("verdict = %s, want AFFECTED", got)
	}
}
