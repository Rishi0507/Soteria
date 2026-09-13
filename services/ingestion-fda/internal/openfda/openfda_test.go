package openfda

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"soteria/libs/feedkit/event"
	"soteria/libs/feedkit/httpx"
)

func loadPage(t *testing.T) response {
	t.Helper()
	b, err := os.ReadFile("testdata/enforcement_page.json")
	if err != nil {
		t.Fatal(err)
	}
	var r response
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestMapRecordedReport(t *testing.T) {
	page := loadPage(t)
	it, err := Map(page.Results[0])
	if err != nil {
		t.Fatal(err)
	}
	if it.SourceID != "H-1258-2026" {
		t.Errorf("source_id: %q", it.SourceID)
	}
	if it.PublishedAt == nil || !it.PublishedAt.Equal(time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("published_at: %v", it.PublishedAt)
	}
	n := it.Normalized
	if n.Firm != "H & U Inc. dba Sun Noodle" || n.Classification != "Class I" || n.Country != "US" {
		t.Errorf("normalized: %+v", n)
	}
	if !strings.Contains(n.ProductDescription, "UPC 085315054108") {
		t.Errorf("product_description lost UPC text: %q", n.ProductDescription)
	}
	if len(n.Title) > 160 || !strings.HasPrefix(n.Title, "Sura Tanmen") {
		t.Errorf("title: %q", n.Title)
	}
	if !strings.Contains(it.SourceURL, "Event=99571") {
		t.Errorf("source_url: %q", it.SourceURL)
	}
	// Raw must be the untouched record.
	var raw map[string]any
	if err := json.Unmarshal(it.Raw, &raw); err != nil || raw["recall_number"] != "H-1258-2026" || raw["voluntary_mandated"] == nil {
		t.Errorf("raw not preserved: %v", err)
	}
	// And the whole thing must produce a valid event.
	ev := event.New("ingestion-fda", "fda_enforcement", it, time.Now())
	if err := ev.Validate(); err != nil {
		t.Errorf("event invalid: %v", err)
	}
}

func TestMapRejectsMissingRecallNumber(t *testing.T) {
	if _, err := Map(json.RawMessage(`{"product_description":"x"}`)); err == nil {
		t.Fatal("expected error")
	}
}

// TestFetchPaginates serves a 250-record feed in pages of 100 and checks the
// source walks every page and stops.
func TestFetchPaginates(t *testing.T) {
	const total = 250
	var hits []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, r.URL.RawQuery)
		if !strings.Contains(r.URL.RawQuery, "search=report_date:[20260101+TO+20260912]") {
			t.Errorf("bad search window: %s", r.URL.RawQuery)
		}
		skip, _ := strconv.Atoi(r.URL.Query().Get("skip"))
		n := min(pageSize, total-skip)
		var results []string
		for i := 0; i < n; i++ {
			results = append(results, fmt.Sprintf(`{"recall_number":"F-%04d-2026","product_description":"p","report_date":"20260901"}`, skip+i))
		}
		fmt.Fprintf(w, `{"meta":{"results":{"skip":%d,"limit":%d,"total":%d}},"results":[%s]}`, skip, pageSize, total, strings.Join(results, ","))
	}))
	defer srv.Close()

	src := New(httpx.New("test"), "k")
	src.BaseURL = srv.URL
	src.Now = func() time.Time { return time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC) }
	items, err := src.Fetch(context.Background(), time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != total || len(hits) != 3 {
		t.Fatalf("items=%d requests=%d", len(items), len(hits))
	}
	if !strings.Contains(hits[0], "api_key=k") {
		t.Errorf("api key not sent: %s", hits[0])
	}
}

func TestFetchTreatsNotFoundAsEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		w.Write([]byte(`{"error":{"code":"NOT_FOUND","message":"No matches found!"}}`))
	}))
	defer srv.Close()
	src := New(httpx.New("test"), "")
	src.BaseURL = srv.URL
	items, err := src.Fetch(context.Background(), time.Now().Add(-time.Hour))
	if err != nil || len(items) != 0 {
		t.Fatalf("want empty result, got %d items err=%v", len(items), err)
	}
}
