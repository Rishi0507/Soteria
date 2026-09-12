package offacts

import (
	"regexp"
	"sort"
	"strings"
	"sync"
)

// knownAllergens covers the FDA "big nine" plus common RASFF wordings, mapped to
// the Open Food Facts allergen names.
var knownAllergens = map[string][]string{
	"peanuts":      {"peanut"},
	"nuts":         {"tree nut", "almond", "cashew", "walnut", "pecan", "pistachio", "hazelnut"},
	"milk":         {"milk", "dairy", "casein", "whey"},
	"eggs":         {"egg"},
	"fish":         {"fish", "cod", "tuna", "salmon"},
	"crustaceans":  {"shellfish", "crustacean", "shrimp", "crab", "lobster"},
	"gluten":       {"wheat", "gluten"},
	"soybeans":     {"soy"},
	"sesame-seeds": {"sesame", "tahini"},
}

// HazardAllergens extracts allergen names from recall text ("undeclared peanut").
// Order-rescue refuses any substitute carrying one of these; resolution-service
// stamps them onto lot.resolved.v1 so the hazard travels with the incident.
func HazardAllergens(text string) []string {
	lower := strings.ToLower(text)
	var out []string
	for canonical, needles := range knownAllergens {
		for _, n := range needles {
			if needleRE(n).MatchString(lower) {
				out = append(out, canonical)
				break
			}
		}
	}
	sort.Strings(out)
	return out
}

var needleCache sync.Map // needle -> *regexp.Regexp

// needleRE matches a needle as a whole word, optionally plural. Substring matching
// is not good enough here: "Lot Code" contains "cod", and a phantom fish allergen
// on an incident would poison every substitute decision downstream.
func needleRE(needle string) *regexp.Regexp {
	if re, ok := needleCache.Load(needle); ok {
		return re.(*regexp.Regexp)
	}
	re := regexp.MustCompile(`\b` + regexp.QuoteMeta(needle) + `(?:s|es)?\b`)
	needleCache.Store(needle, re)
	return re
}
