// Contract test (PRD §6 item 5): every event this producer can emit must
// validate against contracts/events/recall.raw.received.v1.json.
package ingestionrasff

import (
	"context"
	"testing"
	"time"

	"soteria/libs/feedkit/contracttest"
	"soteria/libs/feedkit/event"
	"soteria/libs/feedkit/source"
	rasffile "soteria/services/ingestion-rasff/internal/rasff/file"
)

func TestEventsMatchContract(t *testing.T) {
	schema := contracttest.Compile(t, contracttest.SchemaPath(t, "recall.raw.received.v1.json"))
	items, err := rasffile.New("fixtures").Fetch(context.Background(), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) == 0 {
		t.Fatal("fixtures yielded no items")
	}
	for _, it := range items {
		body, err := event.New("ingestion-rasff", source.EURASFF, it, time.Now()).Marshal()
		if err != nil {
			t.Fatalf("%s: %v", it.SourceID, err)
		}
		contracttest.Validate(t, schema, body)
	}
}
