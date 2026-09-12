// Package fsis polls the USDA Food Safety and Inspection Service recall API.
//
//	GET https://www.fsis.usda.gov/fsis/api/recall/v/1
//
// Free, keyless JSON. The endpoint has no `since` parameter and returns the
// full list of recalls and public health alerts for the current year (older
// years via ?field_year_id=), so every poll is diffed against the dedup
// store. FSIS covers meat, poultry and egg products — the highest-fatality
// recall class the FDA feeds never carry.
//
// NOTE: fsis.usda.gov sits behind Akamai and returns 403 to some
// non-US networks. The field names below come from the documented v1
// payload; re-verify against a live response from a US egress once and
// refresh testdata/recalls.json.
package fsis

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"strings"
	"time"

	"soteria/libs/feedkit/httpx"
	"soteria/libs/feedkit/source"
)

const (
	DefaultBaseURL = "https://www.fsis.usda.gov/fsis/api/recall/v/1"
	siteURL        = "https://www.fsis.usda.gov"
)

// Source polls FSIS.
type Source struct {
	Client  *httpx.Client
	BaseURL string
}

// New returns a source with defaults.
func New(c *httpx.Client) *Source { return &Source{Client: c, BaseURL: DefaultBaseURL} }

func (s *Source) Name() string { return source.USDAFSIS }

// Fetch returns every notice in the feed with recall_date >= since (notices
// without a parseable date are always returned; the dedup store filters).
func (s *Source) Fetch(ctx context.Context, since time.Time) ([]source.Item, error) {
	body, err := s.Client.Get(ctx, s.BaseURL, map[string]string{"Accept": "application/json"})
	if err != nil {
		return nil, err
	}
	return Parse(body, since)
}

// Record is the subset of the FSIS payload we project. FSIS serializes
// every field as a string; summary and product_items carry HTML.
type Record struct {
	RecallNumber   string `json:"field_recall_number"`
	Title          string `json:"field_title"`
	RecallDate     string `json:"field_recall_date"`
	Summary        string `json:"field_summary"`
	ProductItems   string `json:"field_product_items"`
	States         string `json:"field_states"`
	Classification string `json:"field_recall_classification"`
	RiskLevel      string `json:"field_risk_level"`
	Establishment  string `json:"field_establishment"`
	RecallReason   string `json:"field_recall_reason"`
	RecallType     string `json:"field_recall_type"`
	PressRelease   string `json:"field_press_release"`
	ActiveNotice   string `json:"field_active_notice"`
	Year           string `json:"field_year"`
}

// Parse decodes the feed body. It accepts either a bare array or an object
// wrapping one (some deployments have returned {"data":[...]}).
func Parse(body []byte, since time.Time) ([]source.Item, error) {
	var raws []json.RawMessage
	if err := json.Unmarshal(body, &raws); err != nil {
		var wrapped struct {
			Data []json.RawMessage `json:"data"`
		}
		if err2 := json.Unmarshal(body, &wrapped); err2 != nil || wrapped.Data == nil {
			return nil, fmt.Errorf("fsis: decode: %w", err)
		}
		raws = wrapped.Data
	}
	out := make([]source.Item, 0, len(raws))
	for _, raw := range raws {
		it, err := Map(raw)
		if err != nil {
			return nil, err
		}
		if it.PublishedAt != nil && it.PublishedAt.Before(since) {
			continue
		}
		out = append(out, it)
	}
	return out, nil
}

// Map projects one FSIS record into an Item.
func Map(raw json.RawMessage) (source.Item, error) {
	var r Record
	if err := json.Unmarshal(raw, &r); err != nil {
		return source.Item{}, fmt.Errorf("fsis: decode record: %w", err)
	}
	if r.RecallNumber == "" {
		return source.Item{}, fmt.Errorf("fsis: record without field_recall_number: %.200s", raw)
	}
	title := strings.TrimSpace(r.Title)
	if title == "" {
		title = "FSIS notice " + r.RecallNumber
	}
	classification := r.RiskLevel
	if classification == "" {
		classification = r.Classification
	}
	// Reason + summary: FSIS puts the category in recall_reason and the
	// narrative (pathogen, allergen, dates) in summary.
	reason := strings.TrimSpace(r.RecallReason)
	if s := stripHTML(r.Summary); s != "" {
		if reason != "" {
			reason += " — "
		}
		reason += s
	}
	var url string
	if r.PressRelease != "" {
		if strings.HasPrefix(r.PressRelease, "http") {
			url = r.PressRelease
		} else {
			url = siteURL + "/" + strings.TrimPrefix(r.PressRelease, "/")
		}
	}
	return source.Item{
		SourceID:    r.RecallNumber,
		SourceURL:   url,
		PublishedAt: parseDate(r.RecallDate),
		Normalized: source.Normalized{
			Title:              title,
			Firm:               strings.TrimSpace(r.Establishment),
			ProductDescription: stripHTML(r.ProductItems),
			Reason:             reason,
			CodeInfo:           "", // lot/establishment codes are embedded in product_items; Resolution extracts them
			Classification:     classification,
			Distribution:       strings.TrimSpace(r.States),
			Country:            "US",
		},
		Raw: raw,
	}, nil
}

var dateLayouts = []string{"2006-01-02", "Jan 2, 2006", "January 2, 2006", "01/02/2006", "2006-01-02T15:04:05"}

func parseDate(s string) *time.Time {
	s = strings.TrimSpace(s)
	for _, l := range dateLayouts {
		if t, err := time.ParseInLocation(l, s, time.UTC); err == nil {
			return &t
		}
	}
	return nil
}

var (
	tagRe   = regexp.MustCompile(`<[^>]*>`)
	spaceRe = regexp.MustCompile(`[ \t]+`)
	nlRe    = regexp.MustCompile(`\n{2,}`)
)

// stripHTML flattens FSIS HTML fragments into readable text, turning list
// items and paragraphs into line breaks so product lines stay separable.
func stripHTML(s string) string {
	if s == "" {
		return ""
	}
	s = strings.NewReplacer("</li>", "\n", "</p>", "\n", "<br>", "\n", "<br/>", "\n", "<br />", "\n").Replace(s)
	s = tagRe.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	s = strings.ReplaceAll(s, " ", " ") // &nbsp; decodes to NBSP
	s = spaceRe.ReplaceAllString(s, " ")
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i])
	}
	s = strings.Join(lines, "\n")
	s = nlRe.ReplaceAllString(s, "\n")
	return strings.TrimSpace(s)
}
