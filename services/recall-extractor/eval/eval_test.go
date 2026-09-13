// Package eval is the extraction evaluation suite: 16 real notices from all
// four feeds with hand-labelled expectations. It measures what the LLM +
// guardrail actually get right, per field, and fails CI below thresholds.
//
//	go test ./eval -v                          # replay recorded model answers (no key needed)
//	EVAL_RECORD=1 GROQ_API_KEY=... go test ./eval -v   # call the model, rewrite recordings.json
package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	feedevent "soteria/libs/feedkit/event"
	"soteria/services/recall-extractor/internal/extract"
)

type expected struct {
	UPCs          []string `json:"upcs"`
	UPCsVerbatim  []string `json:"upcs_verbatim_ok"`
	Lots          []string `json:"lots"` // nil → not scored
	LotsOptional  []string `json:"lots_optional"`
	HazardType    string   `json:"hazard_type"`
	AgentContains string   `json:"agent_contains"`
	Allergens     []string `json:"allergens"`
	States        []string `json:"states"`
	Nationwide    *bool    `json:"nationwide"`
	CountriesAny  []string `json:"countries_any"`
	BrandAny      []string `json:"brand_any"`
	MinProducts   int      `json:"min_products"`
	Note          string   `json:"note"`
}

type testCase struct {
	ID       string          `json:"id"`
	Raw      feedevent.Event `json:"raw"`
	Expected expected        `json:"expected"`
}

type recordings struct {
	Model   string                    `json:"model"`
	Entries map[string]recordingEntry `json:"entries"`
}

type recordingEntry struct {
	CaseID   string             `json:"case_id"`
	Text     string             `json:"text"`
	Response extract.Extraction `json:"response"`
}

// Thresholds the suite must clear. UPC recall is guaranteed by the regex net;
// the interesting numbers are UPC precision (no junk), lots, hazard, allergens.
const (
	minUPCRecall    = 0.95
	minUPCPrecision = 0.95
	minLotRecall    = 0.80
	minLotPrecision = 0.80
	minHazardAcc    = 0.90
	minAllergenAcc  = 0.90
	minStatesAcc    = 0.85
	minBrandAcc     = 0.85
)

func TestExtractionSuite(t *testing.T) {
	cases := loadCases(t)
	ex, save := extractor(t)

	type row struct {
		id                      string
		upcTP, upcFP, upcFN     int
		lotTP, lotFP, lotFN     int
		lotScored               bool
		hazard, allergen, state bool
		stateScored, brand      bool
		conf                    float64
		corrections             int
		problems                []string
	}
	var rows []row
	ctx := context.Background()
	for _, c := range cases {
		text := extract.DocumentFor(c.Raw).Text()
		ext, err := ex.Extract(ctx, text)
		if err != nil {
			t.Fatalf("%s: %v", c.ID, err)
		}
		save(c.ID, text, ext)
		ext.Source, ext.SourceID = c.Raw.Source, c.Raw.SourceID
		extract.Validate(&ext, text)
		e := c.Expected
		r := row{id: c.ID, conf: ext.Confidence, corrections: len(ext.Corrections)}

		// UPCs: verbatim-kept codes are neither TP nor FP unless expected.
		verbatim := map[string]bool{}
		for _, corr := range ext.Corrections {
			if strings.HasPrefix(corr, "kept upc ") {
				verbatim[strings.Fields(corr)[2]] = true
			}
		}
		okVerbatim := set(e.UPCsVerbatim)
		expUPC := set(e.UPCs)
		for _, u := range ext.AllUPCs() {
			switch {
			case expUPC[u]:
				r.upcTP++
			case verbatim[u] && okVerbatim[u]:
				// fine
			case verbatim[u]:
				// unlabelled typo code: not counted
			default:
				r.upcFP++
				r.problems = append(r.problems, "extra upc "+u)
			}
		}
		for u := range expUPC {
			if !contains(ext.AllUPCs(), u) {
				r.upcFN++
				r.problems = append(r.problems, "missed upc "+u)
			}
		}

		// Lots.
		if e.Lots != nil {
			r.lotScored = true
			expLot, optLot := setFold(e.Lots), setFold(e.LotsOptional)
			for _, l := range ext.AllLots() {
				k := fold(l)
				switch {
				case expLot[k]:
					r.lotTP++
				case optLot[k]:
				default:
					r.lotFP++
					r.problems = append(r.problems, "extra lot "+l)
				}
			}
			got := setFold(ext.AllLots())
			for _, l := range e.Lots {
				if !got[fold(l)] {
					r.lotFN++
					r.problems = append(r.problems, "missed lot "+l)
				}
			}
		}

		// Hazard.
		r.hazard = ext.Hazard.Type == e.HazardType && (e.AgentContains == "" || strings.Contains(strings.ToLower(ext.Hazard.Agent), e.AgentContains))
		if !r.hazard {
			r.problems = append(r.problems, fmt.Sprintf("hazard %s/%q want %s/%q", ext.Hazard.Type, ext.Hazard.Agent, e.HazardType, e.AgentContains))
		}
		r.allergen = sameSet(ext.Hazard.Allergens, e.Allergens)
		if !r.allergen {
			r.problems = append(r.problems, fmt.Sprintf("allergens %v want %v", ext.Hazard.Allergens, e.Allergens))
		}

		// Distribution.
		switch {
		case e.Nationwide != nil:
			r.stateScored, r.state = true, ext.Distribution.Nationwide == *e.Nationwide
		case len(e.States) > 0:
			r.stateScored = true
			r.state = jaccard(ext.Distribution.States, e.States) >= 0.9
		case len(e.CountriesAny) > 0:
			r.stateScored = true
			for _, c := range ext.Distribution.Countries {
				for _, want := range e.CountriesAny {
					if strings.Contains(strings.ToLower(c), want) {
						r.state = true
					}
				}
			}
		}
		if r.stateScored && !r.state {
			r.problems = append(r.problems, fmt.Sprintf("distribution states=%v nationwide=%v countries=%v", ext.Distribution.States, ext.Distribution.Nationwide, ext.Distribution.Countries))
		}

		// Brand / products.
		hay := strings.ToLower(ext.Brand + " " + ext.Firm)
		for _, p := range ext.Products {
			hay += " " + strings.ToLower(p.Brand+" "+p.Name)
		}
		r.brand = len(e.BrandAny) == 0
		for _, b := range e.BrandAny {
			if strings.Contains(hay, b) {
				r.brand = true
			}
		}
		if !r.brand {
			r.problems = append(r.problems, fmt.Sprintf("brand %q not in %v", ext.Brand, e.BrandAny))
		}
		if e.MinProducts > 0 && len(ext.Products) < e.MinProducts {
			r.problems = append(r.problems, fmt.Sprintf("only %d products, want >= %d", len(ext.Products), e.MinProducts))
		}
		rows = append(rows, r)
	}

	// ---- report
	var upcTP, upcFP, upcFN, lotTP, lotFP, lotFN, hz, al, st, stN, br int
	t.Logf("%-20s %-11s %-11s %-6s %-6s %-6s %-6s %-5s %s", "case", "upc P/R", "lot P/R", "hazard", "allerg", "states", "brand", "conf", "problems")
	for _, r := range rows {
		upcTP, upcFP, upcFN = upcTP+r.upcTP, upcFP+r.upcFP, upcFN+r.upcFN
		lotTP, lotFP, lotFN = lotTP+r.lotTP, lotFP+r.lotFP, lotFN+r.lotFN
		hz += b2i(r.hazard)
		al += b2i(r.allergen)
		if r.stateScored {
			stN++
			st += b2i(r.state)
		}
		br += b2i(r.brand)
		lot := "  -  "
		if r.lotScored {
			lot = fmt.Sprintf("%.2f/%.2f", prec(r.lotTP, r.lotFP), rec(r.lotTP, r.lotFN))
		}
		t.Logf("%-20s %.2f/%.2f   %-11s %-6s %-6s %-6s %-6s %.2f  %s", r.id, prec(r.upcTP, r.upcFP), rec(r.upcTP, r.upcFN), lot,
			mark(r.hazard), mark(r.allergen), markOpt(r.stateScored, r.state), mark(r.brand), r.conf, strings.Join(r.problems, "; "))
	}
	n := float64(len(rows))
	upcP, upcR := prec(upcTP, upcFP), rec(upcTP, upcFN)
	lotP, lotR := prec(lotTP, lotFP), rec(lotTP, lotFN)
	hzA, alA, brA := float64(hz)/n, float64(al)/n, float64(br)/n
	stA := 1.0
	if stN > 0 {
		stA = float64(st) / float64(stN)
	}
	t.Logf("TOTAL (%d cases, model %s): UPC P=%.3f R=%.3f | lots P=%.3f R=%.3f | hazard %.3f | allergens %.3f | distribution %.3f | brand %.3f",
		len(rows), ex.Name(), upcP, upcR, lotP, lotR, hzA, alA, stA, brA)

	check := func(name string, got, min float64) {
		if got < min {
			t.Errorf("%s %.3f below threshold %.2f", name, got, min)
		}
	}
	check("upc recall", upcR, minUPCRecall)
	check("upc precision", upcP, minUPCPrecision)
	check("lot recall", lotR, minLotRecall)
	check("lot precision", lotP, minLotPrecision)
	check("hazard accuracy", hzA, minHazardAcc)
	check("allergen accuracy", alA, minAllergenAcc)
	check("distribution accuracy", stA, minStatesAcc)
	check("brand accuracy", brA, minBrandAcc)
}

// extractor returns the model under test: recordings by default, live when
// EVAL_RECORD=1 (then recordings.json is rewritten on exit).
func extractor(t *testing.T) (extract.Extractor, func(id, text string, ext extract.Extraction)) {
	t.Helper()
	recordPath := "recordings.json"
	if os.Getenv("EVAL_RECORD") == "1" {
		key := os.Getenv("GROQ_API_KEY")
		if key == "" {
			t.Fatal("EVAL_RECORD=1 needs GROQ_API_KEY")
		}
		groq := extract.NewGroq(key, os.Getenv("GROQ_MODEL"))
		rec := recordings{Model: groq.Name(), Entries: map[string]recordingEntry{}}
		cache, err := extract.OpenCache(":memory:", groq)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			cache.Close()
			b, _ := json.MarshalIndent(rec, "", " ")
			if err := os.WriteFile(recordPath, b, 0o644); err != nil {
				t.Errorf("write recordings: %v", err)
			}
			t.Logf("recorded %d model answers to %s", len(rec.Entries), recordPath)
		})
		return cache, func(id, text string, ext extract.Extraction) {
			rec.Entries[cache.Key(text)] = recordingEntry{CaseID: id, Text: text, Response: ext}
		}
	}
	b, err := os.ReadFile(recordPath)
	if err != nil {
		t.Skip("no recordings.json yet; run with EVAL_RECORD=1 GROQ_API_KEY=... to create it")
	}
	var rec recordings
	if err := json.Unmarshal(b, &rec); err != nil {
		t.Fatal(err)
	}
	cache, err := extract.OpenCache(":memory:", &extract.Fake{ModelName: rec.Model})
	if err != nil {
		t.Fatal(err)
	}
	cache.ReadOnly = true
	t.Cleanup(func() { cache.Close() })
	for _, e := range rec.Entries {
		if err := cache.Put(e.Text, e.Response); err != nil {
			t.Fatal(err)
		}
	}
	return cache, func(string, string, extract.Extraction) {}
}

func loadCases(t *testing.T) []testCase {
	t.Helper()
	b, err := os.ReadFile("cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cs []testCase
	if err := json.Unmarshal(b, &cs); err != nil {
		t.Fatal(err)
	}
	if len(cs) < 15 {
		t.Fatalf("expected at least 15 cases, got %d", len(cs))
	}
	_ = time.Now
	return cs
}

// ---- helpers

func set(xs []string) map[string]bool {
	m := map[string]bool{}
	for _, x := range xs {
		m[x] = true
	}
	return m
}

func fold(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }

func setFold(xs []string) map[string]bool {
	m := map[string]bool{}
	for _, x := range xs {
		m[fold(x)] = true
	}
	return m
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

func sameSet(a, b []string) bool {
	as, bs := append([]string(nil), a...), append([]string(nil), b...)
	sort.Strings(as)
	sort.Strings(bs)
	return strings.Join(as, ",") == strings.Join(bs, ",")
}

func jaccard(a, b []string) float64 {
	as, bs := set(a), set(b)
	inter, union := 0, len(bs)
	for x := range as {
		if bs[x] {
			inter++
		} else {
			union++
		}
	}
	if union == 0 {
		return 1
	}
	return float64(inter) / float64(union)
}

func prec(tp, fp int) float64 {
	if tp+fp == 0 {
		return 1
	}
	return float64(tp) / float64(tp+fp)
}

func rec(tp, fn int) float64 {
	if tp+fn == 0 {
		return 1
	}
	return float64(tp) / float64(tp+fn)
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func mark(b bool) string {
	if b {
		return "ok"
	}
	return "FAIL"
}

func markOpt(scored, b bool) string {
	if !scored {
		return "-"
	}
	return mark(b)
}
