package fdarss

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"soteria/libs/feedkit/event"
	"soteria/libs/feedkit/httpx"
)

func TestParseArticleFromRealPage(t *testing.T) {
	page, err := os.ReadFile("testdata/press_cabricharme.html")
	if err != nil {
		t.Fatal(err)
	}
	a := ParseArticle(page)
	if a.Company != "Whole Foods Market" || a.Brand != "Whole Foods Market" || a.Product != "Cheese" || a.Reason != "Undeclared Egg" {
		t.Errorf("summary: %+v", a)
	}
	for _, want := range []string{
		"egg lysozyme",
		"18 Whole Foods Market stores in Arizona, California, Connecticut, Massachusetts, New Jersey, New York, and Rhode Island",
		"57953", "54670", // PLUs — only on the page, not in the RSS blurb
		"10/7/2026",
	} {
		if !strings.Contains(a.Text, want) {
			t.Errorf("article text missing %q", want)
		}
	}
	for _, chrome := range []string{"Follow FDA", "Flickr", "<div", "&nbsp;"} {
		if strings.Contains(a.Text, chrome) {
			t.Errorf("article text still contains %q", chrome)
		}
	}
	if !strings.HasPrefix(a.Text, "Company Announcement Date") {
		t.Errorf("text should start at the summary: %.80q", a.Text)
	}
}

func TestFetchEnrichesItemsAndCachesPages(t *testing.T) {
	page, _ := os.ReadFile("testdata/press_cabricharme.html")
	var pageHits int32
	var srvURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rss.xml":
			w.Write([]byte(`<?xml version="1.0"?><rss version="2.0"><channel>
<item><title>Whole Foods Market Issues Allergy Alert</title><link>` + srvURL + `/press/cheese</link><description>blurb</description><pubDate>Fri, 11 Sep 2026 18:15:00 EDT</pubDate><guid>` + srvURL + `/press/cheese</guid></item>
<item><title>Broken page</title><link>` + srvURL + `/press/missing</link><description>only the blurb</description><pubDate>Thu, 10 Sep 2026 16:37:00 EDT</pubDate><guid>` + srvURL + `/press/missing</guid></item>
</channel></rss>`))
		case "/press/cheese":
			atomic.AddInt32(&pageHits, 1)
			w.Write(page)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	srvURL = srv.URL

	c := httpx.New("test")
	c.MaxRetries = 0
	s := New(c)
	s.URL = srv.URL + "/rss.xml"

	items, err := s.Fetch(context.Background(), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("items: %d", len(items))
	}
	enriched, fallback := items[0], items[1]
	if enriched.Normalized.Firm != "Whole Foods Market" || enriched.Normalized.Reason != "Undeclared Egg" {
		t.Errorf("normalized not enriched: %+v", enriched.Normalized)
	}
	if !strings.Contains(enriched.Normalized.ProductDescription, "57953") {
		t.Error("product_description should be the full article")
	}
	var raw map[string]any
	if err := json.Unmarshal(enriched.Raw, &raw); err != nil {
		t.Fatalf("raw is not valid JSON after enrichment: %v", err)
	}
	if raw["description"] != "blurb" || !strings.Contains(raw["article"].(string), "egg lysozyme") || raw["summary"].(map[string]any)["brand"] != "Whole Foods Market" {
		t.Errorf("raw: %v", raw)
	}
	if err := event.New("ingestion-fda", "fda_press", enriched, time.Now()).Validate(); err != nil {
		t.Errorf("event invalid: %v", err)
	}
	// A 404 page must not lose the notice.
	if fallback.Normalized.ProductDescription != "only the blurb" || fallback.Normalized.Firm != "" {
		t.Errorf("fallback item altered: %+v", fallback.Normalized)
	}

	// Second poll: the page is cached; no extra request.
	if _, err := s.Fetch(context.Background(), time.Time{}); err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&pageHits) != 1 {
		t.Errorf("page fetched %d times; expected once (cached)", pageHits)
	}
}

func TestJSONStringEscapes(t *testing.T) {
	got := jsonString("a\"b\\c\nd\te\x01")
	var back string
	if err := json.Unmarshal([]byte(got), &back); err != nil || back != "a\"b\\c\nd\te\x01" {
		t.Errorf("round trip: %q → %q (%v)", got, back, err)
	}
}
