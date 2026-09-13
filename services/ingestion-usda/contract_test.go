// Contract test (PRD §6 item 5): every event this producer can emit must
// validate against contracts/events/recall.raw.received.v1.json.
package ingestionusda

import (
	"os"
	"testing"
	"time"

	"soteria/libs/feedkit/contracttest"
	"soteria/libs/feedkit/event"
	"soteria/libs/feedkit/source"
	"soteria/services/ingestion-usda/internal/fsis"
)

func TestEventsMatchContract(t *testing.T) {
	schema := contracttest.Compile(t, contracttest.SchemaPath(t, "recall.raw.received.v1.json"))
	b, err := os.ReadFile("internal/fsis/testdata/recalls.json")
	if err != nil {
		t.Fatal(err)
	}
	items, err := fsis.Parse(b, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) == 0 {
		t.Fatal("fixture yielded no items")
	}
	for _, it := range items {
		body, err := event.New("ingestion-usda", source.USDAFSIS, it, time.Now()).Marshal()
		if err != nil {
			t.Fatalf("%s: %v", it.SourceID, err)
		}
		contracttest.Validate(t, schema, body)
	}
}
