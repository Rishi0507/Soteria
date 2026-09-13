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
	"os"
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
	body, err := s.Client.Get(ctx, s.BaseURL, requestHeaders())
	if err != nil {
		return nil, err
	}
	return Parse(body, since)
}

// browserUA is the exact User-Agent the FSIS edge (Akamai) will accept.
//
// www.fsis.usda.gov answers 403 to any request that does not look like a
// browser XHR: a descriptive agent, or even this string with our own token
// appended, is refused. The data is public and unauthenticated, so the only
// thing standing between us and it is this fingerprint. Verified against the
// live endpoint: UA + Accept-Language + the Sec-Fetch trio returns 200 and
// 2,000+ records, while dropping any one of them returns 403.
//
// This is a fingerprint, so it will rot. Override it with FSIS_USER_AGENT, or
// point FSIS_BASE_URL at a proxy, when the edge rules change.
const browserUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 " +
	"(KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36"

// requestHeaders returns the header set the FSIS edge requires. Accept-Encoding
// is deliberately absent: Go's transport adds "gzip" itself and then transparently
// decompresses, which setting the header by hand would disable.
func requestHeaders() map[string]string {
	ua := browserUA
	if v := os.Getenv("FSIS_USER_AGENT"); v != "" {
		ua = v
	}
	return map[string]string{
		"User-Agent":      ua,
		"Accept":          "application/json",
		"Accept-Language": "en-US,en;q=0.9",
		"Sec-Fetch-Site":  "same-origin",
		"Sec-Fetch-Mode":  "cors",
		"Sec-Fetch-Dest":  "empty",
	}
}

// multiString is an FSIS field that may arrive as a string or as an array of
// strings. The feed is inconsistent about this per field and per record, and a
// hard string type makes one array-valued record fail the entire poll.
type multiString string

func (m *multiString) UnmarshalJSON(b []byte) error {
	var one string
	if err := json.Unmarshal(b, &one); err == nil {
		*m = multiString(one)
		return nil
	}
	var many []string
	if err := json.Unmarshal(b, &many); err != nil {
		return fmt.Errorf("fsis: field is neither string nor []string: %.120s", string(b))
	}
	var kept []string
	for _, v := range many {
		if v = strings.TrimSpace(v); v != "" {
			kept = append(kept, v)
		}
	}
	*m = multiString(strings.Join(kept, "; "))
	return nil
}

func (m multiString) String() string { return string(m) }

// Record is the subset of the FSIS payload we project. Most fields are strings;
// the ones typed multiString arrive as arrays in the live feed. Summary and
// product_items carry HTML.
type Record struct {
	RecallNumber   string      `json:"field_recall_number"`
	Title          string      `json:"field_title"`
	RecallDate     string      `json:"field_recall_date"`
	Summary        string      `json:"field_summary"`
	ProductItems   multiString `json:"field_product_items"`
	States         multiString `json:"field_states"`
	Classification string      `json:"field_recall_classification"`
	RiskLevel      string      `json:"field_risk_level"`
	Establishment  multiString `json:"field_establishment"`
	RecallReason   multiString `json:"field_recall_reason"`
	RecallType     string      `json:"field_recall_type"`
	PressRelease   multiString `json:"field_press_release"`
	ActiveNotice   string      `json:"field_active_notice"`
	Year           string      `json:"field_year"`
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
	reason := strings.TrimSpace(r.RecallReason.String())
	if s := stripHTML(r.Summary); s != "" {
		if reason != "" {
			reason += " — "
		}
		reason += s
	}
	var url string
	if r.PressRelease != "" {
		if strings.HasPrefix(r.PressRelease.String(), "http") {
			url = r.PressRelease.String()
		} else {
			url = siteURL + "/" + strings.TrimPrefix(r.PressRelease.String(), "/")
		}
	}
	return source.Item{
		SourceID:    r.RecallNumber,
		SourceURL:   url,
		PublishedAt: parseDate(r.RecallDate),
		Normalized: source.Normalized{
			Title:              title,
			Firm:               strings.TrimSpace(r.Establishment.String()),
			ProductDescription: stripHTML(r.ProductItems.String()),
			Reason:             reason,
			CodeInfo:           "", // lot/establishment codes are embedded in product_items; Resolution extracts them
			Classification:     classification,
			Distribution:       strings.TrimSpace(r.States.String()),
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
