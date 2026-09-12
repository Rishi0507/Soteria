// Package http is the RASFF Window portal adapter.
//
// There is still no documented RASFF API, but the portal publishes a public RSS
// feed that its own UI links to, and that is a contract stable enough to poll:
//
//	GET /rasff-window/backend/public/consumer/rss/{market}/{lang}/
//	    market: "all" (every single-market country) or a numeric organization id
//	    lang:   "en"
//	    the trailing slash is required; without it the gateway answers 404
//
// Found by reading the SPA's lazy-loaded chunk, which builds the link as
// `./backend/public/consumer/rss/${market}/${lang}/`. The search endpoints the
// UI uses for its table remain undocumented and unreachable without a session,
// so this feed is the honest public source.
//
// One item looks like:
//
//	<title>2026.8025 - Shiga toxin-producing Escherichia coli (STEC) in chopped parsley from France</title>
//	<link>https://webgate.ec.europa.eu/rasff-window/screen/notification/871940</link>
//	<description>Notified by Luxembourg on 11/09/2026</description>
//
// That is less detail than a portal export, so file mode stays the richer
// source: this one trades fields for being live and unattended.
package http

import (
	"context"
	"encoding/xml"
	"fmt"
	"regexp"
	"strings"
	"time"

	"soteria/libs/feedkit/httpx"
	"soteria/libs/feedkit/source"
	"soteria/services/ingestion-rasff/internal/rasff"
)

// DefaultBaseURL is the portal's API base. Everything below it is a fixed path.
const DefaultBaseURL = "https://webgate.ec.europa.eu/rasff-window/backend"

// Source polls the portal's public RSS feed.
type Source struct {
	Client  *httpx.Client
	BaseURL string
	Market  string // "all", or a numeric organization id
	Lang    string
}

// New returns a live RASFF source rooted at baseURL.
func New(baseURL string) (source.Source, error) {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = DefaultBaseURL
	}
	return &Source{
		Client:  httpx.New("soteria-ingestion-rasff/1.0 (+https://soteria.dev)"),
		BaseURL: strings.TrimRight(baseURL, "/"),
		Market:  "all",
		Lang:    "en",
	}, nil
}

func (s *Source) Name() string { return source.EURASFF }

// URL builds the feed URL, trailing slash included.
func (s *Source) URL() string {
	return fmt.Sprintf("%s/public/consumer/rss/%s/%s/", s.BaseURL, s.Market, s.Lang)
}

// Fetch returns notifications dated at or after since. Undated ones are always
// returned and left to the dedup store, matching file mode.
func (s *Source) Fetch(ctx context.Context, since time.Time) ([]source.Item, error) {
	body, err := s.Client.Get(ctx, s.URL(), map[string]string{"Accept": "application/rss+xml, application/xml, text/xml"})
	if err != nil {
		return nil, err
	}
	notifications, err := ParseRSS(body)
	if err != nil {
		return nil, err
	}

	var items []source.Item
	for _, n := range notifications {
		it, err := rasff.ToItem(n)
		if err != nil {
			// A feed item without a reference is not a notification we can
			// address; skip it rather than lose the rest of the feed.
			continue
		}
		if it.PublishedAt != nil && it.PublishedAt.Before(since) {
			continue
		}
		items = append(items, it)
	}
	return items, nil
}

// feed is the subset of RSS 2.0 the portal emits.
type feed struct {
	Items []struct {
		Title       string `xml:"title"`
		Link        string `xml:"link"`
		Description string `xml:"description"`
	} `xml:"channel>item"`
}

var (
	// "2026.8025 - Shiga toxin-producing E. coli (STEC) in chopped parsley from France"
	titlePattern = regexp.MustCompile(`^\s*([0-9]{4}\.[0-9]+)\s*-\s*(.+)$`)
	// "Notified by Luxembourg on 11/09/2026"
	descPattern = regexp.MustCompile(`(?i)notified\s+by\s+(.+?)\s+on\s+([0-9]{1,2}/[0-9]{1,2}/[0-9]{4})`)
	// "<hazard> in <product> from <origin>", the portal's own subject convention.
	subjectPattern = regexp.MustCompile(`(?i)^(.+?)\s+in\s+(.+?)(?:\s+from\s+(.+))?$`)
)

// ParseRSS projects the feed onto rasff.Notification, so both modes share one
// model and one ToItem.
func ParseRSS(body []byte) ([]rasff.Notification, error) {
	var f feed
	if err := xml.Unmarshal(body, &f); err != nil {
		return nil, fmt.Errorf("rasff/http: decode rss: %w", err)
	}
	if len(f.Items) == 0 {
		// An empty feed is possible, but empty *and* undecodable usually means we
		// were served the portal's HTML error page instead.
		if !strings.Contains(strings.ToLower(string(body)), "<rss") {
			return nil, fmt.Errorf("rasff/http: response is not an RSS feed: %.120s", string(body))
		}
		return nil, nil
	}

	out := make([]rasff.Notification, 0, len(f.Items))
	for _, item := range f.Items {
		n := rasff.Notification{URL: strings.TrimSpace(item.Link)}

		title := strings.TrimSpace(unescape(item.Title))
		if m := titlePattern.FindStringSubmatch(title); m != nil {
			n.Reference = m[1]
			n.Subject = strings.TrimSpace(m[2])
		} else {
			n.Subject = title
		}

		if m := descPattern.FindStringSubmatch(unescape(item.Description)); m != nil {
			n.NotifyingCountry = strings.TrimSpace(m[1])
			n.Date = normalizeDate(m[2])
		}

		// The subject follows "<hazard> in <product> from <origin>". Splitting it
		// gives downstream matching a product string to work with; the subject is
		// kept verbatim either way, so a subject that does not fit the shape
		// simply yields no split rather than a wrong one.
		if m := subjectPattern.FindStringSubmatch(n.Subject); m != nil {
			n.Hazards = strings.TrimSpace(m[1])
			n.Product = strings.TrimSpace(m[2])
			n.Origin = strings.TrimSpace(m[3])
		}
		out = append(out, n)
	}
	return out, nil
}

// normalizeDate turns the feed's DD/MM/YYYY into the ISO form the shared parser
// prefers, leaving anything unexpected untouched for it to reject.
func normalizeDate(s string) string {
	t, err := time.Parse("02/01/2006", s)
	if err != nil {
		return s
	}
	return t.Format("2006-01-02")
}

func unescape(s string) string {
	r := strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`, "&#39;", "'", "&apos;", "'")
	return r.Replace(s)
}
