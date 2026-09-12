// Contract test (PRD §6 item 5): every event this producer can emit must
// validate against contracts/events/recall.raw.received.v1.json.
package ingestionfda

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"soteria/libs/feedkit/contracttest"
	"soteria/libs/feedkit/event"
	"soteria/libs/feedkit/source"
	"soteria/services/ingestion-fda/internal/fdarss"
	"soteria/services/ingestion-fda/internal/openfda"
)

func TestEventsMatchContract(t *testing.T) {
	schema := contracttest.Compile(t, contracttest.SchemaPath(t, "recall.raw.received.v1.json"))
	now := time.Now()

	var items []struct {
		src string
		it  source.Item
	}

	b, err := os.ReadFile("internal/openfda/testdata/enforcement_page.json")
	if err != nil {
		t.Fatal(err)
	}
	var page struct {
		Results []json.RawMessage `json:"results"`
	}
	if err := json.Unmarshal(b, &page); err != nil {
		t.Fatal(err)
	}
	for _, raw := range page.Results {
		it, err := openfda.Map(raw)
		if err != nil {
			t.Fatal(err)
		}
		items = append(items, struct {
			src string
			it  source.Item
		}{source.FDAEnforcement, it})
	}

	b, err = os.ReadFile("internal/fdarss/testdata/recalls.xml")
	if err != nil {
		t.Fatal(err)
	}
	rssItems, err := fdarss.Parse(b, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range rssItems {
		items = append(items, struct {
			src string
			it  source.Item
		}{source.FDAPress, it})
	}

	if len(items) < 20 {
		t.Fatalf("expected fixtures to yield many items, got %d", len(items))
	}
	for _, x := range items {
		body, err := event.New("ingestion-fda", x.src, x.it, now).Marshal()
		if err != nil {
			t.Fatalf("%s/%s: %v", x.src, x.it.SourceID, err)
		}
		contracttest.Validate(t, schema, body)
	}
}

// A deliberately broken payload must be rejected, proving the schema has teeth.
func TestContractRejectsDrift(t *testing.T) {
	schema := contracttest.Compile(t, contracttest.SchemaPath(t, "recall.raw.received.v1.json"))
	bad := []byte(`{"event_id":"not-a-uuid","event_type":"recall.raw.received","version":1,
		"occurred_at":"2026-09-12T00:00:00Z","producer":"ingestion-fda","source":"cdc","source_id":"x",
		"published_at":null,"normalized":{"title":"t","country":"US"},"raw":{}}`)
	if err := contracttest.Check(schema, bad); err == nil {
		t.Fatal("schema accepted an invalid source/event_id")
	}
}
