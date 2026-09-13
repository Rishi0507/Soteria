package openfda

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"soteria/libs/feedkit/httpx"
)

// page builds an openFDA response body from raw record JSON.
func page(records ...string) string {
	joined := ""
	for i, r := range records {
		if i > 0 {
			joined += ","
		}
		joined += r
	}
	return fmt.Sprintf(`{"meta":{"results":{"skip":0,"limit":100,"total":%d}},"results":[%s]}`, len(records), joined)
}

const usableRecord = `{"recall_number":"F-1234-2026","event_id":"90210","product_description":"Granola bars",
 "reason_for_recall":"undeclared peanut","code_info":"Lot 8H-1132","report_date":"20260901",
 "recalling_firm":"Sunfield Farms","classification":"Class I","distribution_pattern":"CA, OR","status":"Ongoing"}`

// TestFetchSkipsUnmappableRecords covers the live failure this fixes: openFDA
// returns real enforcement rows with no recall_number, and one of them used to
// abort the entire poll, discarding every usable notice on the page.
func TestFetchSkipsUnmappableRecords(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(page(`{"status":"Terminated","city":"White Salmon","classification":"Class II"}`, usableRecord)))
	}))
	defer srv.Close()

	s := New(httpx.New("test"), "")
	s.BaseURL = srv.URL
	items, err := s.Fetch(context.Background(), time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("one unusable row must not fail the poll: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected the usable record to survive, got %d items", len(items))
	}
	if items[0].SourceID != "F-1234-2026" {
		t.Fatalf("source id = %q", items[0].SourceID)
	}
}

// TestFetchFailsWhenEveryRecordIsUnmappable: a whole page failing is a schema
// break or an outage, not a stray row, and must stay loud.
func TestFetchFailsWhenEveryRecordIsUnmappable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(page(`{"status":"Terminated"}`, `{"city":"Portland"}`)))
	}))
	defer srv.Close()

	s := New(httpx.New("test"), "")
	s.BaseURL = srv.URL
	if _, err := s.Fetch(context.Background(), time.Now().Add(-24*time.Hour)); err == nil {
		t.Fatal("expected an error when no record on the page is mappable")
	}
}

// TestMapStillRejectsRecordsWithoutRecallNumber keeps the mapper's contract: the
// decision to skip belongs to the caller, not to Map.
func TestMapStillRejectsRecordsWithoutRecallNumber(t *testing.T) {
	if _, err := Map(json.RawMessage(`{"status":"Terminated"}`)); err == nil {
		t.Fatal("Map must still report an unusable record")
	}
}
