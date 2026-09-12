package notify

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"soteria/libs/core/events"
)

func fixtures(t *testing.T) []events.Envelope {
	t.Helper()
	b, err := os.ReadFile("../../testdata/sample_events.json")
	if err != nil {
		t.Fatal(err)
	}
	var envs []events.Envelope
	if err := json.Unmarshal(b, &envs); err != nil {
		t.Fatal(err)
	}
	for i := range envs {
		envs[i].EventID = events.NewID()
		envs[i].EventVersion = 1
		envs[i].OccurredAt = time.Now().UTC()
	}
	return envs
}

func byType(t *testing.T, typ string) events.Envelope {
	t.Helper()
	for _, e := range fixtures(t) {
		if e.EventType == typ {
			return e
		}
	}
	t.Fatalf("no fixture for %s", typ)
	return events.Envelope{}
}

func newService(t *testing.T) (*Service, *FakeChannel, *FakeChannel, *MemOut) {
	t.Helper()
	ledger, err := OpenLedger(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ledger.Close() })
	slack, email := NewFake("slack-fake"), NewFake("resend-fake")
	out := &MemOut{}
	svc := New(&Router{OpsRecipient: "ops", ConsentURL: "https://shop.test/rescue/{rescue_id}", RetailerName: "Metro Market"}, ledger,
		map[string]Channel{ChannelSlack: slack, ChannelEmail: email}, out, nil)
	svc.Sleep = func(context.Context, time.Duration) error { return nil } // no real backoff in tests
	return svc, slack, email, out
}

// ---- router -------------------------------------------------------------------

func TestRouterContainment(t *testing.T) {
	r := &Router{OpsRecipient: "ops"}
	msgs, err := r.Route(byType(t, events.TypeContainmentTaken))
	if err != nil || len(msgs) != 1 {
		t.Fatalf("msgs=%d err=%v", len(msgs), err)
	}
	m := msgs[0]
	if m.Channel != ChannelSlack || m.Recipient != "ops" || !strings.Contains(m.Subject, "Recall contained") {
		t.Errorf("message: %+v", m)
	}
	for _, want := range []string{"Listeria", "8H-1132, 8H-1133", "75 units quarantined, 25 left sellable", "AUTO_HOLD"} {
		if !strings.Contains(m.Text, want) {
			t.Errorf("text missing %q:\n%s", want, m.Text)
		}
	}
}

func TestRouterRescueEmailsCustomerAndCopiesOps(t *testing.T) {
	r := &Router{OpsRecipient: "ops", ConsentURL: "https://shop.test/rescue/{rescue_id}", RetailerName: "Metro Market"}
	msgs, err := r.Route(byType(t, events.TypeOrderRescueProposed))
	if err != nil || len(msgs) != 2 {
		t.Fatalf("msgs=%d err=%v", len(msgs), err)
	}
	email, ops := msgs[0], msgs[1]
	if email.Channel != ChannelEmail || email.Recipient != "customer@example.com" {
		t.Errorf("email: %+v", email)
	}
	for _, want := range []string{"Metro Market", "#1001", "https://shop.test/rescue/rsc-01J9", "Harvest Lane", "USD 5.99", "allergen-safe", "Full refund", "lot 8H-1132"} {
		if !strings.Contains(email.Text, want) {
			t.Errorf("email text missing %q", want)
		}
		if !strings.Contains(email.HTML, strings.ReplaceAll(want, "#", "#")) && !strings.Contains(email.HTML, want) {
			t.Errorf("email html missing %q", want)
		}
	}
	if ops.Channel != ChannelSlack || strings.Contains(ops.Text, "customer@example.com") || !strings.Contains(ops.Text, "cu***@example.com") {
		t.Errorf("ops copy must mask the customer address: %s", ops.Text)
	}
}

func TestRouterRescueFallsBackToSMSAndThenNothing(t *testing.T) {
	r := &Router{OpsRecipient: "ops", ConsentURL: "https://s/{rescue_id}"}
	env := byType(t, events.TypeOrderRescueProposed)
	var p events.OrderRescueProposed
	_ = json.Unmarshal(env.Payload, &p)

	p.Customer.Email, p.Customer.Phone = "", "+15551234567"
	env.Payload, _ = json.Marshal(p)
	msgs, _ := r.Route(env)
	if len(msgs) != 2 || msgs[0].Channel != ChannelSMS || msgs[0].Recipient != "+15551234567" || !strings.Contains(msgs[0].Text, "https://s/rsc-01J9") {
		t.Errorf("sms fallback: %+v", msgs)
	}

	p.Customer.Phone = ""
	env.Payload, _ = json.Marshal(p)
	msgs, _ = r.Route(env)
	if len(msgs) != 1 || msgs[0].Channel != ChannelSlack || !strings.Contains(msgs[0].Text, "none (no contact details)") {
		t.Errorf("no-contact case must still alert ops: %+v", msgs)
	}
}

func TestRouterIgnoresUnknownAndRejectsGarbage(t *testing.T) {
	r := &Router{}
	if msgs, err := r.Route(events.Envelope{EventType: "audit.dossier.generated.v1", Payload: json.RawMessage(`{}`)}); msgs != nil || err != nil {
		t.Errorf("unknown type: %v %v", msgs, err)
	}
	_, err := r.Route(events.Envelope{EventType: events.TypeContainmentTaken, Payload: json.RawMessage(`{"targets":"nope"}`)})
	if !IsPermanent(err) {
		t.Errorf("garbage payload should be permanent, got %v", err)
	}
}

// ---- service ------------------------------------------------------------------

func TestHandleSendsRecordsAndEmits(t *testing.T) {
	svc, slack, email, out := newService(t)
	ctx := context.Background()
	for _, env := range fixtures(t) {
		if err := svc.Handle(ctx, env); err != nil {
			t.Fatalf("%s: %v", env.EventType, err)
		}
	}
	if len(slack.Sent) != 3 || len(email.Sent) != 1 {
		t.Fatalf("slack=%d email=%d", len(slack.Sent), len(email.Sent))
	}
	ds, _ := svc.Ledger.ByIncident(ctx, "inc-2026-0912-001")
	if len(ds) != 4 {
		t.Fatalf("ledger rows: %d", len(ds))
	}
	for _, d := range ds {
		if d.Status != StatusSent || d.Attempts != 1 || d.SentAt == nil || d.ProviderRef == "" {
			t.Errorf("ledger row: %+v", d)
		}
	}
	if len(out.Events) != 4 {
		t.Fatalf("delivered events: %d", len(out.Events))
	}
	ev := out.Events[0]
	var d Delivered
	_ = json.Unmarshal(ev.Payload, &d)
	if ev.EventType != TypeDelivered || ev.CorrelationID != "inc-2026-0912-001" || ev.CausationID == "" || d.Status != StatusSent || d.Provider != "slack-fake" {
		t.Errorf("delivered envelope: %+v payload=%+v", ev, d)
	}
	st := svc.Stats().Snapshot()
	if st.Received != 3 || st.Sent != 4 || st.Failed != 0 {
		t.Errorf("stats: %+v", st)
	}
}

func TestRedeliveryDoesNotResend(t *testing.T) {
	svc, slack, _, _ := newService(t)
	env := byType(t, events.TypeContainmentTaken)
	for i := 0; i < 3; i++ {
		if err := svc.Handle(context.Background(), env); err != nil {
			t.Fatal(err)
		}
	}
	if len(slack.Sent) != 1 {
		t.Fatalf("redelivery resent: %d sends", len(slack.Sent))
	}
}

func TestTransientFailureRetriesInProcessThenSucceeds(t *testing.T) {
	svc, slack, _, _ := newService(t)
	slack.Fail = []error{errors.New("502"), errors.New("503"), nil}
	env := byType(t, events.TypeContainmentTaken)
	if err := svc.Handle(context.Background(), env); err != nil {
		t.Fatal(err)
	}
	ds, _ := svc.Ledger.ByIncident(context.Background(), "inc-2026-0912-001")
	if len(slack.Sent) != 1 || ds[0].Attempts != 3 || ds[0].Status != StatusSent {
		t.Fatalf("sent=%d row=%+v", len(slack.Sent), ds[0])
	}
}

func TestTransientExhaustionDefersToBus(t *testing.T) {
	svc, slack, _, out := newService(t)
	slack.Fail = []error{errors.New("502"), errors.New("502"), errors.New("502")}
	env := byType(t, events.TypeContainmentTaken)
	err := svc.Handle(context.Background(), env)
	if err == nil {
		t.Fatal("expected error so the bus redelivers")
	}
	ds, _ := svc.Ledger.ByIncident(context.Background(), "inc-2026-0912-001")
	if ds[0].Status != StatusRetry || ds[0].Attempts != 3 || !strings.Contains(ds[0].LastError, "502") {
		t.Errorf("row: %+v", ds[0])
	}
	if len(out.Events) != 0 {
		t.Error("no delivered event while still retrying")
	}
	// Bus redelivers; provider recovered; the same row completes.
	if err := svc.Handle(context.Background(), env); err != nil {
		t.Fatal(err)
	}
	ds, _ = svc.Ledger.ByIncident(context.Background(), "inc-2026-0912-001")
	if ds[0].Status != StatusSent || ds[0].Attempts != 4 || len(slack.Sent) != 1 {
		t.Errorf("after redelivery: %+v sends=%d", ds[0], len(slack.Sent))
	}
}

func TestPermanentFailureIsRecordedAndAcked(t *testing.T) {
	svc, _, email, out := newService(t)
	email.Fail = []error{Permanent(errors.New("422 invalid recipient"))}
	env := byType(t, events.TypeOrderRescueProposed)
	if err := svc.Handle(context.Background(), env); err != nil {
		t.Fatalf("permanent failure must ack, got %v", err)
	}
	ds, _ := svc.Ledger.ByIncident(context.Background(), "inc-2026-0912-001")
	var emailRow Delivery
	for _, d := range ds {
		if d.Channel == ChannelEmail {
			emailRow = d
		}
	}
	if emailRow.Status != StatusFailed || emailRow.Attempts != 1 {
		t.Errorf("email row: %+v", emailRow)
	}
	var failed int
	for _, e := range out.Events {
		var d Delivered
		_ = json.Unmarshal(e.Payload, &d)
		if d.Status == StatusFailed && d.Channel == ChannelEmail && strings.Contains(d.Error, "422") {
			failed++
		}
	}
	if failed != 1 {
		t.Errorf("expected one FAILED delivered event, got %d", failed)
	}
}

func TestMissingChannelIsPermanent(t *testing.T) {
	svc, _, _, _ := newService(t)
	delete(svc.Channels, ChannelEmail)
	if err := svc.Handle(context.Background(), byType(t, events.TypeOrderRescueProposed)); err != nil {
		t.Fatal(err)
	}
	counts, _ := svc.Ledger.Counts(context.Background())
	if counts[StatusFailed] != 1 || counts[StatusSent] != 1 {
		t.Errorf("counts: %v", counts)
	}
}

// ---- providers ----------------------------------------------------------------

func TestSlackAndResendClassifyStatuses(t *testing.T) {
	var status int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer re_test" {
			w.WriteHeader(status)
			w.Write([]byte(`{"id":"em_123"}`))
			return
		}
		w.WriteHeader(status)
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	slack := NewSlack(srv.URL)
	resend := NewResend("re_test", "Sotería <onboarding@resend.dev>")
	resend.URL = srv.URL
	msg := Message{Channel: ChannelEmail, Recipient: "a@b.c", Subject: "s", Text: "t"}

	status = 200
	if ref, err := resend.Send(context.Background(), msg); err != nil || ref != "em_123" {
		t.Errorf("200: ref=%q err=%v", ref, err)
	}
	if _, err := slack.Send(context.Background(), msg); err != nil {
		t.Errorf("slack 200: %v", err)
	}
	status = 429
	if _, err := resend.Send(context.Background(), msg); err == nil || IsPermanent(err) {
		t.Errorf("429 must be transient: %v", err)
	}
	status = 503
	if _, err := slack.Send(context.Background(), msg); err == nil || IsPermanent(err) {
		t.Errorf("503 must be transient: %v", err)
	}
	status = 422
	if _, err := resend.Send(context.Background(), msg); !IsPermanent(err) {
		t.Errorf("422 must be permanent: %v", err)
	}
	if _, err := resend.Send(context.Background(), Message{Recipient: "not-an-email"}); !IsPermanent(err) {
		t.Errorf("bad recipient must be permanent: %v", err)
	}
	if _, err := NewSlack("").Send(context.Background(), msg); !IsPermanent(err) {
		t.Errorf("unconfigured webhook must be permanent: %v", err)
	}
}

func TestMask(t *testing.T) {
	for in, want := range map[string]string{"customer@example.com": "cu***@example.com", "a@b.c": "***@b.c", "+15551234567": "***4567", "ops": "***", "": ""} {
		if got := Mask(in); got != want {
			t.Errorf("Mask(%q)=%q want %q", in, got, want)
		}
	}
}
