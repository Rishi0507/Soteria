// Package fdarss polls the FDA "Recalls, Market Withdrawals & Safety Alerts"
// RSS feed — press releases that typically precede the openFDA enforcement
// report by days. Items are unstructured (title + summary + link); the
// resolution-service does the parsing.
package fdarss

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"strings"
	"time"

	"soteria/libs/feedkit/httpx"
	"soteria/libs/feedkit/source"
)

// DefaultURL is the FDA recalls RSS feed.
const DefaultURL = "https://www.fda.gov/about-fda/contact-fda/stay-informed/rss-feeds/recalls/rss.xml"

// Source polls the RSS feed.
type Source struct {
	Client *httpx.Client
	URL    string
	// FetchArticles fetches each item's press-release page so the event
	// carries the full announcement (UPCs, lots, states) instead of the
	// 300-character RSS blurb. On by default.
	FetchArticles bool
	cache         articleCache
}

// New returns a source with defaults.
func New(c *httpx.Client) *Source { return &Source{Client: c, URL: DefaultURL, FetchArticles: true} }

func (s *Source) Name() string { return source.FDAPress }

type rss struct {
	Channel struct {
		Items []item `xml:"item"`
	} `xml:"channel"`
}

type item struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	Description string `xml:"description"`
	PubDate     string `xml:"pubDate"`
	Creator     string `xml:"http://purl.org/dc/elements/1.1/ creator"`
	GUID        string `xml:"guid"`
}

// Fetch returns items published at or after since. The feed only carries
// the most recent ~20 notices, so since mostly matters on first boot.
func (s *Source) Fetch(ctx context.Context, since time.Time) ([]source.Item, error) {
	body, err := s.Client.Get(ctx, s.URL, map[string]string{"Accept": "application/rss+xml, application/xml, text/xml"})
	if err != nil {
		return nil, err
	}
	items, err := Parse(body, since)
	if err != nil {
		return nil, err
	}
	if s.FetchArticles {
		s.enrich(ctx, items)
	}
	return items, nil
}

// Parse decodes an RSS document into items.
func Parse(body []byte, since time.Time) ([]source.Item, error) {
	var doc rss
	if err := xml.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("fdarss: decode: %w", err)
	}
	var out []source.Item
	for _, it := range doc.Channel.Items {
		pub := parsePubDate(it.PubDate)
		if pub != nil && pub.Before(since) {
			continue
		}
		id := strings.TrimSpace(it.GUID)
		if id == "" {
			id = strings.TrimSpace(it.Link)
		}
		if id == "" {
			continue
		}
		raw, err := json.Marshal(map[string]string{
			"title":       it.Title,
			"link":        it.Link,
			"description": it.Description,
			"pubDate":     it.PubDate,
			"creator":     it.Creator,
			"guid":        it.GUID,
		})
		if err != nil {
			return nil, err
		}
		out = append(out, source.Item{
			SourceID:    id,
			SourceURL:   strings.TrimSpace(it.Link),
			PublishedAt: pub,
			Normalized: source.Normalized{
				Title:              strings.TrimSpace(it.Title),
				ProductDescription: strings.TrimSpace(it.Description),
				Country:            "US",
			},
			Raw: raw,
		})
	}
	return out, nil
}

// The FDA feed uses RFC 1123 with a US zone abbreviation (EDT/EST).
var pubLayouts = []string{time.RFC1123Z, time.RFC1123, "Mon, 2 Jan 2006 15:04:05 MST", "Mon, 2 Jan 2006 15:04:05 -0700"}

func parsePubDate(s string) *time.Time {
	s = strings.TrimSpace(s)
	for _, l := range pubLayouts {
		if t, err := time.Parse(l, s); err == nil {
			// Go parses an abbreviation it can't resolve locally as offset 0;
			// map US Eastern explicitly so ordering against `since` is right.
			// (If the host is itself in Eastern time the offset is already set.)
			if z, off := t.Zone(); off == 0 && z == "EDT" {
				t = t.Add(4 * time.Hour)
			} else if off == 0 && z == "EST" {
				t = t.Add(5 * time.Hour)
			}
			u := t.UTC()
			return &u
		}
	}
	return nil
}
