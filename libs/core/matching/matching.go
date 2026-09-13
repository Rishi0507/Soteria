// Package matching is the shared matching-confidence library.
//
// It is imported by resolution-service (recall notice -> catalog product) and by
// anti-evasion-service (marketplace listing -> recalled lot), so
// the two never drift into two different definitions of "confident".
//
// Design rule: every score carries Evidence. A number nobody can justify is useless
// in an audit dossier, and a reviewer in the ops console needs to see *why*.
package matching

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// Weights of each signal. They are additive and then clamped, so a strong
// identifier (GTIN) alone clears a sane auto-hold threshold while fuzzy text
// alone never does.
const (
	WeightExactGTIN     = 0.70
	WeightExactUPC      = 0.70
	WeightSKUMatch      = 0.15
	WeightBrandMatch    = 0.12
	WeightTitleMax      = 0.25 // scaled by similarity
	WeightSilentDiffMax = 0.35 // scaled by the diff's own confidence
	MinTitleSimilarity  = 0.35 // below this, title contributes nothing
	MaxConfidence       = 0.99 // we never claim certainty
)

// Evidence is one justified contribution to a score.
type Evidence struct {
	Signal string
	Weight float64
	Detail string
}

// Signals describes what the recall notice (or marketplace listing) told us.
type Signals struct {
	UPCs       []string
	GTINs      []string
	SKUs       []string
	Brands     []string
	Text       string   // product description / notice body / listing title
	LotCodes   []string // lot codes already declared by the agency, if any
	SilentDiff float64  // vanish_confidence when the signal came from catalog diffing
}

// Candidate is a product from the retailer catalog we are scoring against.
type Candidate struct {
	GTIN         string
	UPC          string
	SKU          string
	Brand        string
	ProductTitle string
	LotCodes     []string // lot codes present in inventory for this product
}

// Result is a scored candidate.
type Result struct {
	Candidate  Candidate
	Confidence float64
	LotCodes   []string // intersection of recalled lots and lots we actually hold
	Evidence   []Evidence
}

// Score computes a confidence in [0, MaxConfidence] with full evidence.
func Score(s Signals, c Candidate) Result {
	var total float64
	var ev []Evidence

	candGTIN := NormalizeGTIN(firstNonEmpty(c.GTIN, c.UPC))
	for _, g := range append(append([]string{}, s.GTINs...), s.UPCs...) {
		ng := NormalizeGTIN(g)
		if ng != "" && ng == candGTIN {
			total += WeightExactGTIN
			ev = append(ev, Evidence{
				Signal: "EXACT_GTIN",
				Weight: WeightExactGTIN,
				Detail: fmt.Sprintf("notice GTIN %s == catalog GTIN %s", ng, candGTIN),
			})
			break
		}
	}

	if c.SKU != "" {
		for _, sku := range s.SKUs {
			if strings.EqualFold(strings.TrimSpace(sku), c.SKU) {
				total += WeightSKUMatch
				ev = append(ev, Evidence{Signal: "SKU_MATCH", Weight: WeightSKUMatch,
					Detail: fmt.Sprintf("SKU %s matched", c.SKU)})
				break
			}
		}
	}

	if c.Brand != "" {
		for _, b := range s.Brands {
			if brandMatch(b, c.Brand) {
				total += WeightBrandMatch
				ev = append(ev, Evidence{Signal: "BRAND_MATCH", Weight: WeightBrandMatch,
					Detail: fmt.Sprintf("brand %q matched %q", b, c.Brand)})
				break
			}
		}
	}

	if sim := TitleSimilarity(s.Text, c.ProductTitle); sim >= MinTitleSimilarity {
		w := WeightTitleMax * sim
		total += w
		ev = append(ev, Evidence{Signal: "TITLE_SIMILARITY", Weight: round(w),
			Detail: fmt.Sprintf("description/title similarity %.2f", sim)})
	}

	if s.SilentDiff > 0 {
		w := WeightSilentDiffMax * clamp01(s.SilentDiff)
		total += w
		ev = append(ev, Evidence{Signal: "SILENT_DIFF_SIGNAL", Weight: round(w),
			Detail: fmt.Sprintf("SKU vanished from distributor catalog (diff confidence %.2f)", s.SilentDiff)})
	}

	lots := intersectLots(s.LotCodes, c.LotCodes)
	if len(lots) > 0 {
		ev = append(ev, Evidence{Signal: "LOT_CODE_EXTRACTED", Weight: 0,
			Detail: fmt.Sprintf("recalled lot codes present in inventory: %s", strings.Join(lots, ", "))})
	}

	return Result{
		Candidate:  c,
		Confidence: round(min(total, MaxConfidence)),
		LotCodes:   lots,
		Evidence:   ev,
	}
}

// Rank scores every candidate and returns them best-first, dropping anything
// below floor.
func Rank(s Signals, candidates []Candidate, floor float64) []Result {
	out := make([]Result, 0, len(candidates))
	for _, c := range candidates {
		r := Score(s, c)
		if r.Confidence >= floor {
			out = append(out, r)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Confidence > out[j].Confidence })
	return out
}

// --------------------------------------------------------------------- lot codes

var lotPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\blot\s*(?:code|#|no\.?|number)?\s*[:\-]?\s*([A-Z0-9][A-Z0-9\-]{2,19})`),
	regexp.MustCompile(`(?i)\bbatch\s*(?:code|#|no\.?)?\s*[:\-]?\s*([A-Z0-9][A-Z0-9\-]{2,19})`),
	regexp.MustCompile(`(?i)\b(?:est|establishment)\s*(?:#|no\.?)?\s*[:\-]?\s*([A-Z0-9][A-Z0-9\-]{2,19})`),
}

var lotListSep = regexp.MustCompile(`(?i)\s*(?:,|;|\band\b|/)\s*`)

// ExtractLotCodes pulls lot/batch codes out of free recall text. This is what
// turns a SKU-wide recall into a surgical one, so it is deliberately greedy about
// comma-separated runs ("Lot 1234A, 1234B and 1235C") and strict about shape.
func ExtractLotCodes(text string) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	add := func(code string) {
		code = strings.ToUpper(strings.Trim(strings.TrimSpace(code), ".,;:-"))
		if !plausibleLot(code) || seen[code] {
			return
		}
		seen[code] = true
		out = append(out, code)
	}

	for _, re := range lotPatterns {
		for _, m := range re.FindAllStringSubmatchIndex(text, -1) {
			add(text[m[2]:m[3]])
			// Continue over a comma/and-separated run immediately after the match.
			rest := text[m[3]:]
			for {
				sep := lotListSep.FindStringIndex(rest)
				if sep == nil || sep[0] != 0 {
					break
				}
				rest = rest[sep[1]:]
				tok := leadingToken(rest)
				if !plausibleLot(strings.ToUpper(tok)) {
					break
				}
				add(tok)
				rest = rest[len(tok):]
			}
		}
	}
	return out
}

func leadingToken(s string) string {
	end := 0
	for end < len(s) {
		ch := rune(s[end])
		if unicode.IsLetter(ch) || unicode.IsDigit(ch) || ch == '-' {
			end++
			continue
		}
		break
	}
	return s[:end]
}

// plausibleLot rejects prose that happens to follow the word "lot".
func plausibleLot(code string) bool {
	if len(code) < 3 || len(code) > 20 {
		return false
	}
	var digits, letters int
	for _, r := range code {
		switch {
		case unicode.IsDigit(r):
			digits++
		case unicode.IsLetter(r):
			letters++
		case r == '-':
		default:
			return false
		}
	}
	if digits == 0 {
		return false // "LOT NUMBERS" and friends
	}
	return letters+digits >= 3
}

// --------------------------------------------------------------------- identifiers

var nonDigits = regexp.MustCompile(`\D`)

// NormalizeGTIN converts any GTIN-8/12/13/14 (UPC-A, EAN) to zero-padded GTIN-14,
// returning "" when the input is not a valid GTIN (check digit included).
func NormalizeGTIN(raw string) string {
	d := nonDigits.ReplaceAllString(raw, "")
	switch len(d) {
	case 8, 12, 13, 14:
	default:
		return ""
	}
	if !validCheckDigit(d) {
		return ""
	}
	return strings.Repeat("0", 14-len(d)) + d
}

func validCheckDigit(d string) bool {
	sum := 0
	// Weights alternate 3,1 from the rightmost digit before the check digit.
	for i, w := len(d)-2, 3; i >= 0; i, w = i-1, 4-w {
		sum += int(d[i]-'0') * w
	}
	check := (10 - sum%10) % 10
	return check == int(d[len(d)-1]-'0')
}

var upcCandidate = regexp.MustCompile(`\b\d[\d\s\-]{6,18}\d\b`)

// ExtractGTINs finds every valid GTIN in free text.
func ExtractGTINs(text string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range upcCandidate.FindAllString(text, -1) {
		if g := NormalizeGTIN(m); g != "" && !seen[g] {
			seen[g] = true
			out = append(out, g)
		}
	}
	return out
}

// --------------------------------------------------------------------- text

var stopWords = map[string]bool{
	"the": true, "and": true, "of": true, "with": true, "for": true, "in": true,
	"oz": true, "lb": true, "ct": true, "pack": true, "size": true, "net": true, "wt": true,
}

// Normalize lowercases, strips punctuation and collapses whitespace.
func Normalize(s string) string {
	var b strings.Builder
	prevSpace := true
	for _, r := range strings.ToLower(s) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			prevSpace = false
		case !prevSpace:
			b.WriteRune(' ')
			prevSpace = true
		}
	}
	return strings.TrimSpace(b.String())
}

// Tokens returns normalized, stop-word-filtered tokens.
func Tokens(s string) []string {
	var out []string
	for _, t := range strings.Fields(Normalize(s)) {
		if !stopWords[t] {
			out = append(out, t)
		}
	}
	return out
}

// TitleSimilarity blends token-set overlap (robust to reordering and to the extra
// prose in a recall notice) with edit distance (robust to typos and pluralization).
func TitleSimilarity(a, b string) float64 {
	ta, tb := Tokens(a), Tokens(b)
	if len(ta) == 0 || len(tb) == 0 {
		return 0
	}
	return round(0.7*containmentOverlap(ta, tb) + 0.3*levenshteinRatio(Normalize(a), Normalize(b)))
}

// containmentOverlap measures how much of the *shorter* token set appears in the
// longer one: a 6-word product title inside a 200-word notice should still score high.
func containmentOverlap(a, b []string) float64 {
	short, long := a, b
	if len(long) < len(short) {
		short, long = long, short
	}
	set := make(map[string]bool, len(long))
	for _, t := range long {
		set[t] = true
	}
	hits := 0
	seen := map[string]bool{}
	for _, t := range short {
		if seen[t] {
			continue
		}
		seen[t] = true
		if set[t] {
			hits++
		}
	}
	if len(seen) == 0 {
		return 0
	}
	return float64(hits) / float64(len(seen))
}

func levenshteinRatio(a, b string) float64 {
	if a == "" && b == "" {
		return 1
	}
	d := levenshtein(a, b)
	maxLen := max(len(a), len(b))
	if maxLen == 0 {
		return 0
	}
	return 1 - float64(d)/float64(maxLen)
}

func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(min(cur[j-1]+1, prev[j]+1), prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}

func brandMatch(a, b string) bool {
	na, nb := Normalize(a), Normalize(b)
	if na == "" || nb == "" {
		return false
	}
	return na == nb || strings.Contains(na, nb) || strings.Contains(nb, na) || levenshteinRatio(na, nb) >= 0.85
}

// --------------------------------------------------------------------- helpers

// intersectLots returns the recalled lots we actually carry. When we know the
// recalled lots but hold no lot data, we return the recalled lots as-is so the
// caller can still contain surgically against whatever the platform reports.
func intersectLots(recalled, held []string) []string {
	if len(recalled) == 0 {
		return nil
	}
	if len(held) == 0 {
		return dedupeUpper(recalled)
	}
	heldSet := map[string]bool{}
	for _, h := range held {
		heldSet[strings.ToUpper(strings.TrimSpace(h))] = true
	}
	var out []string
	for _, r := range dedupeUpper(recalled) {
		if heldSet[r] {
			out = append(out, r)
		}
	}
	return out
}

func dedupeUpper(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		u := strings.ToUpper(strings.TrimSpace(s))
		if u == "" || seen[u] {
			continue
		}
		seen[u] = true
		out = append(out, u)
	}
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func clamp01(f float64) float64 { return max(0, min(1, f)) }

func round(f float64) float64 { return float64(int(f*1000+0.5)) / 1000 }
