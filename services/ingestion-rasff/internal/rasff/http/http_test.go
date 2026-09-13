package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const sampleFeed = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Single market countries RASFF notifications</title>
    <item>
      <title>2026.8025 - Shiga toxin-producing Escherichia coli (STEC) in chopped parsley from France</title>
      <link>https://webgate.ec.europa.eu/rasff-window/screen/notification/871940</link>
      <description>Notified by Luxembourg on 11/09/2026</description>
    </item>
    <item>
      <title>2026.8018 - Mineral oil components (MOSH/MOAH) in rice from India</title>
      <link>https://webgate.ec.europa.eu/rasff-window/screen/notification/871900</link>
      <description>Notified by Poland on 10/09/2026</description>
    </item>
    <item>
      <title>A notification with no reference</title>
      <link>https://webgate.ec.europa.eu/rasff-window/screen/notification/1</link>
      <description>Notified by Spain on 09/09/2026</description>
    </item>
  </channel>
</rss>`

func TestParseRSSMapsTheFeedOntoNotifications(t *testing.T) {
	got, err := ParseRSS([]byte(sampleFeed))
	if err != nil {
		t.Fatalf("ParseRSS: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 notifications, got %d", len(got))
	}

	first := got[0]
	if first.Reference != "2026.8025" {
		t.Errorf("reference = %q", first.Reference)
	}
	if first.Date != "2026-09-11" {
		t.Errorf("date = %q, want ISO form", first.Date)
	}
	if first.NotifyingCountry != "Luxembourg" {
		t.Errorf("notifying country = %q", first.NotifyingCountry)
	}
	if first.Product != "chopped parsley" {
		t.Errorf("product = %q", first.Product)
	}
	if first.Origin != "France" {
		t.Errorf("origin = %q", first.Origin)
	}
	if first.Hazards != "Shiga toxin-producing Escherichia coli (STEC)" {
		t.Errorf("hazards = %q", first.Hazards)
	}
	if first.Subject == "" {
		t.Error("subject must be kept verbatim")
	}

	// A title that does not carry a reference still parses; Fetch drops it.
	if got[2].Reference != "" || got[2].Subject == "" {
		t.Errorf("unreferenced item = %+v", got[2])
	}
}

// TestParseRSSRejectsAnErrorPage: the gateway serves an HTML 404 body for a
// slightly wrong path, and that must not look like an empty feed.
func TestParseRSSRejectsAnErrorPage(t *testing.T) {
	if _, err := ParseRSS([]byte("<HTML><TITLE>Error 404--Not Found</TITLE></HTML>")); err == nil {
		t.Fatal("expected an error for a non-RSS body")
	}
}

func TestFetchFiltersBySinceAndSkipsUnreferenced(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(sampleFeed))
	}))
	defer srv.Close()

	src, err := New(srv.URL)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	items, err := src.Fetch(context.Background(), time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected only the 11 Sep notification, got %d items", len(items))
	}
	if items[0].SourceID != "2026.8025" {
		t.Errorf("source id = %q", items[0].SourceID)
	}

	// The trailing slash is not cosmetic: without it the gateway answers 404.
	if want := "/public/consumer/rss/all/en/"; gotPath != want {
		t.Errorf("requested %q, want %q", gotPath, want)
	}
}

func TestFetchSurfacesTransportErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	src, _ := New(srv.URL)
	if _, err := src.Fetch(context.Background(), time.Time{}); err == nil {
		t.Fatal("a 404 must be reported, not treated as an empty feed")
	}
}
