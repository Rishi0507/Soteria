package rescue

import (
	"testing"

	"soteria/libs/core/offacts"
)

// TestAllergenSafeFailsClosedOnIncompleteData pins the behaviour that matters:
// an untagged product must never be offered as a substitute just because its
// allergen list happens to be empty.
func TestAllergenSafeFailsClosedOnIncompleteData(t *testing.T) {
	original := offacts.Product{GTIN: "orig", Allergens: []string{"en:gluten"}, Coverage: offacts.CoverageComplete}
	hazard := []string{"peanuts"}

	cases := []struct {
		name string
		sub  offacts.Product
		safe bool
	}{
		{
			name: "complete and compatible",
			sub:  offacts.Product{Allergens: []string{"en:gluten"}, Coverage: offacts.CoverageComplete},
			safe: true,
		},
		{
			name: "complete and genuinely allergen-free",
			sub:  offacts.Product{Coverage: offacts.CoverageComplete},
			safe: true,
		},
		{
			name: "carries the recall hazard",
			sub:  offacts.Product{Allergens: []string{"en:peanuts"}, Coverage: offacts.CoverageComplete},
			safe: false,
		},
		{
			name: "introduces a new allergen",
			sub:  offacts.Product{Allergens: []string{"en:gluten", "en:milk"}, Coverage: offacts.CoverageComplete},
			safe: false,
		},
		{
			name: "trace of the hazard is still unsafe",
			sub:  offacts.Product{Traces: []string{"en:peanuts"}, Coverage: offacts.CoverageComplete},
			safe: false,
		},
		{
			name: "empty allergens but nobody tagged them",
			sub:  offacts.Product{Coverage: offacts.CoveragePartial},
			safe: false,
		},
		{
			name: "no record at all",
			sub:  offacts.Product{Coverage: offacts.CoverageAbsent},
			safe: false,
		},
		{
			name: "coverage unset is treated as absent",
			sub:  offacts.Product{},
			safe: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, why := allergenSafe(original, c.sub, hazard)
			if got != c.safe {
				t.Fatalf("allergenSafe = %v (%s), want %v", got, why, c.safe)
			}
		})
	}
}

// TestAllergenSafeWhenTheRecalledProductIsUnknown: with no authoritative record
// for the original we cannot compare allergens, so only a substitute with no
// allergens at all may be offered.
func TestAllergenSafeWhenTheRecalledProductIsUnknown(t *testing.T) {
	unknown := offacts.Product{GTIN: "orig", Coverage: offacts.CoverageAbsent}

	clean := offacts.Product{Coverage: offacts.CoverageComplete}
	if ok, why := allergenSafe(unknown, clean, nil); !ok {
		t.Fatalf("an allergen-free substitute should still be offerable: %s", why)
	}

	withAllergen := offacts.Product{Allergens: []string{"en:gluten"}, Coverage: offacts.CoverageComplete}
	if ok, _ := allergenSafe(unknown, withAllergen, nil); ok {
		t.Fatal("cannot vouch for an allergen the original may or may not have had")
	}
}
