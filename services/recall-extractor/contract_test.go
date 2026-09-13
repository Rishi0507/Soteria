// Contract test (PRD §6 item 5): every recall.extracted.v1 event this
// service produces — here, from the recorded eval answers for all 16 real
// notices — validates against contracts/events/recall.extracted.v1.json and
// the envelope schema. Runs without an API key.
package recallextractor

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"soteria/libs/core/events"
	"soteria/libs/feedkit/contracttest"
	feedevent "soteria/libs/feedkit/event"
	"soteria/services/recall-extractor/internal/extract"
)

func TestExtractedEventsMatchContract(t *testing.T) {
	schema := contracttest.Compile(t, contracttest.SchemaPath(t, "recall.extracted.v1.json"))
	envelope := contracttest.Compile(t, contracttest.SchemaPath(t, "_envelope.v1.json"))

	var cases []struct {
		ID  string          `json:"id"`
		Raw feedevent.Event `json:"raw"`
	}
	b, err := os.ReadFile("eval/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &cases); err != nil {
		t.Fatal(err)
	}
	var rec struct {
		Model   string `json:"model"`
		Entries map[string]struct {
			Text     string             `json:"text"`
			Response extract.Extraction `json:"response"`
		} `json:"entries"`
	}
	b, err = os.ReadFile("eval/recordings.json")
	if err != nil {
		t.Skip("no eval/recordings.json")
	}
	if err := json.Unmarshal(b, &rec); err != nil {
		t.Fatal(err)
	}
	cache, err := extract.OpenCache(":memory:", &extract.Fake{ModelName: rec.Model})
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()
	cache.ReadOnly = true
	for _, e := range rec.Entries {
		if err := cache.Put(e.Text, e.Response); err != nil {
			t.Fatal(err)
		}
	}

	out := &extract.MemOut{}
	svc := extract.New(cache, out, nil)
	for _, c := range cases {
		body, _ := json.Marshal(c.Raw)
		env := events.Envelope{EventID: events.NewID(), EventType: "ingestion.recall.raw.received.v1", EventVersion: 1, OccurredAt: time.Now().UTC(), Producer: c.Raw.Producer, Payload: body}
		if err := svc.Handle(context.Background(), env); err != nil {
			t.Fatalf("%s: %v", c.ID, err)
		}
	}
	if len(out.Events) != len(cases) {
		t.Fatalf("published %d events for %d cases", len(out.Events), len(cases))
	}
	for _, e := range out.Events {
		whole, _ := json.Marshal(e)
		contracttest.Validate(t, envelope, whole)
		contracttest.Validate(t, schema, e.Payload)
	}
}
