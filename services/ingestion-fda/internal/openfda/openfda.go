// Package openfda polls the openFDA food enforcement endpoint.
//
//	GET https://api.fda.gov/food/enforcement.json
//	  ?search=report_date:[YYYYMMDD+TO+YYYYMMDD]&sort=report_date:desc&limit=100&skip=N
//
// Limits: 40 req/min keyless, 240 req/min with a free api.data.gov key.
// Enforcement reports lag the real-world recall by days–weeks; the press
// RSS (fdarss) is the earlier signal.
package openfda

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"time"

	"soteria/libs/feedkit/httpx"
	"soteria/libs/feedkit/source"
)

const (
	DefaultBaseURL = "https://api.fda.gov/food/enforcement.json"
	pageSize       = 100
	maxPages       = 50 // 5,000 records per poll is far beyond any realistic window
)

// Source polls openFDA.
type Source struct {
	Client  *httpx.Client
	BaseURL string
	APIKey  string
	Now     func() time.Time
}

// New returns a source with defaults.
func New(c *httpx.Client, apiKey string) *Source {
	return &Source{Client: c, BaseURL: DefaultBaseURL, APIKey: apiKey, Now: time.Now}
}

func (s *Source) Name() string { return source.FDAEnforcement }

type response struct {
	Meta struct {
		Results struct {
			Skip  int `json:"skip"`
			Limit int `json:"limit"`
			Total int `json:"total"`
		} `json:"results"`
	} `json:"meta"`
	Results []json.RawMessage `json:"results"`
	Error   *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// Fetch pages through every enforcement report with report_date in [since, now].
func (s *Source) Fetch(ctx context.Context, since time.Time) ([]source.Item, error) {
	var items []source.Item
	for page := 0; page < maxPages; page++ {
		u := s.query(since, page*pageSize)
		body, err := s.Client.Get(ctx, u, nil)
		if err != nil {
			// openFDA answers 404 with {"error":{"code":"NOT_FOUND"}} when a
			// search window has zero matches — that is an empty result, not an outage.
			var se *httpx.StatusError
			if errors.As(err, &se) && se.Status == 404 {
				return items, nil
			}
			return nil, err
		}
		var resp response
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("openfda: decode: %w", err)
		}
		if resp.Error != nil {
			if resp.Error.Code == "NOT_FOUND" {
				return items, nil
			}
			return nil, fmt.Errorf("openfda: %s: %s", resp.Error.Code, resp.Error.Message)
		}
		var skipped int
		for _, raw := range resp.Results {
			it, err := Map(raw)
			if err != nil {
				// openFDA really does return rows the schema cannot carry, such as
				// an enforcement report with no recall_number. One unusable row
				// must not discard the whole page: skip it, count it, and keep the
				// notices we can act on.
				skipped++
				slog.Warn("skipping unmappable record", "source", s.Name(), "page", page, "err", err)
				continue
			}
			items = append(items, it)
		}
		if skipped > 0 && skipped == len(resp.Results) {
			// A whole page failing is a schema break or an outage, not a stray
			// row, and it should be loud.
			return nil, fmt.Errorf("openfda: all %d records on page %d were unmappable", skipped, page)
		}
		if len(resp.Results) < pageSize || resp.Meta.Results.Skip+len(resp.Results) >= resp.Meta.Results.Total {
			break
		}
	}
	return items, nil
}

func (s *Source) query(since time.Time, skip int) string {
	now := s.Now()
	q := url.Values{}
	// openFDA's search syntax needs the literal '+TO+' and brackets unescaped.
	search := fmt.Sprintf("report_date:[%s+TO+%s]", since.UTC().Format("20060102"), now.UTC().Format("20060102"))
	q.Set("sort", "report_date:desc")
	q.Set("limit", fmt.Sprint(pageSize))
	q.Set("skip", fmt.Sprint(skip))
	if s.APIKey != "" {
		q.Set("api_key", s.APIKey)
	}
	return s.BaseURL + "?search=" + search + "&" + q.Encode()
}

// Record is the subset of an enforcement report we project. Every field is
// a string in openFDA, dates are YYYYMMDD.
type Record struct {
	RecallNumber        string `json:"recall_number"`
	EventID             string `json:"event_id"`
	Status              string `json:"status"`
	Classification      string `json:"classification"`
	ProductType         string `json:"product_type"`
	RecallingFirm       string `json:"recalling_firm"`
	ProductDescription  string `json:"product_description"`
	ReasonForRecall     string `json:"reason_for_recall"`
	CodeInfo            string `json:"code_info"`
	DistributionPattern string `json:"distribution_pattern"`
	ReportDate          string `json:"report_date"`
	RecallInitiation    string `json:"recall_initiation_date"`
	City                string `json:"city"`
	State               string `json:"state"`
	Country             string `json:"country"`
}

// Map projects one raw enforcement report into an Item.
func Map(raw json.RawMessage) (source.Item, error) {
	var r Record
	if err := json.Unmarshal(raw, &r); err != nil {
		return source.Item{}, fmt.Errorf("openfda: decode record: %w", err)
	}
	if r.RecallNumber == "" {
		return source.Item{}, fmt.Errorf("openfda: record without recall_number: %s", truncate(raw, 200))
	}
	title := r.ProductDescription
	if len(title) > 160 {
		title = title[:157] + "..."
	}
	if title == "" {
		title = "FDA enforcement report " + r.RecallNumber
	}
	return source.Item{
		SourceID:    r.RecallNumber,
		SourceURL:   "https://www.accessdata.fda.gov/scripts/ires/index.cfm?Event=" + url.PathEscape(r.EventID),
		PublishedAt: parseDate(r.ReportDate),
		Normalized: source.Normalized{
			Title:              title,
			Firm:               r.RecallingFirm,
			ProductDescription: r.ProductDescription,
			Reason:             r.ReasonForRecall,
			CodeInfo:           r.CodeInfo,
			Classification:     r.Classification,
			Distribution:       r.DistributionPattern,
			Country:            "US",
		},
		Raw: raw,
	}, nil
}

func parseDate(yyyymmdd string) *time.Time {
	t, err := time.ParseInLocation("20060102", yyyymmdd, time.UTC)
	if err != nil {
		return nil
	}
	return &t
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}
