// Contract tests (PRD §6 item 5): the events this service consumes decode
// into the shared types, and the event it produces validates against
// contracts/events/notification.delivered.v1.json.
package notificationservice

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"soteria/libs/core/events"
	"soteria/libs/feedkit/contracttest"
	"soteria/services/notification-service/internal/notify"
)

func TestDeliveredEventsMatchContract(t *testing.T) {
	schema := contracttest.Compile(t, contracttest.SchemaPath(t, "notification.delivered.v1.json"))
	envelope := contracttest.Compile(t, contracttest.SchemaPath(t, "_envelope.v1.json"))

	b, err := os.ReadFile("testdata/sample_events.json")
	if err != nil {
		t.Fatal(err)
	}
	var envs []events.Envelope
	if err := json.Unmarshal(b, &envs); err != nil {
		t.Fatal(err)
	}
	ledger, _ := notify.OpenLedger(":memory:")
	defer ledger.Close()
	out := &notify.MemOut{}
	slack, email := notify.NewFake("slack"), notify.NewFake("resend")
	email.Fail = []error{notify.Permanent(errTest("422 rejected"))} // one FAILED record too
	svc := notify.New(&notify.Router{OpsRecipient: "ops", ConsentURL: "https://s/{rescue_id}"}, ledger,
		map[string]notify.Channel{notify.ChannelSlack: slack, notify.ChannelEmail: email}, out, nil)
	for i := range envs {
		envs[i].EventID, envs[i].EventVersion, envs[i].OccurredAt = events.NewID(), 1, time.Now().UTC()
		if err := svc.Handle(context.Background(), envs[i]); err != nil {
			t.Fatal(err)
		}
	}
	if len(out.Events) < 4 {
		t.Fatalf("expected delivered events, got %d", len(out.Events))
	}
	for _, e := range out.Events {
		whole, _ := json.Marshal(e)
		contracttest.Validate(t, envelope, whole)
		contracttest.Validate(t, schema, e.Payload)
	}
}

type errTest string

func (e errTest) Error() string { return string(e) }
