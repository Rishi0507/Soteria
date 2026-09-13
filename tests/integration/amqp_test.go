// Package integration exercises the whole event chain against a real RabbitMQ
// broker, which is the one thing the in-process bus cannot prove.
//
// The in-memory bus implements the same routing rules, but it cannot tell you
// whether the queues actually get declared, whether the bindings match the keys
// services publish on, whether a failing handler really dead-letters, or whether
// publisher confirms behave. Those are exactly the failures that only appear in
// production.
//
// Run it:
//
//	cd infra && docker compose up -d rabbitmq
//	RABBITMQ_URL="amqp://$RABBITMQ_USER:$RABBITMQ_PASSWORD@localhost:5672/" go test ./tests/integration/...
//
// Without RABBITMQ_URL every test skips, so CI without a broker stays green and
// nobody is tempted to delete them.
package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"soteria/libs/core/bus"
	"soteria/libs/core/events"
)

// brokerURL returns the broker to test against, or skips.
func brokerURL(t *testing.T) string {
	t.Helper()
	url := os.Getenv("RABBITMQ_URL")
	if url == "" {
		t.Skip("RABBITMQ_URL not set: start infra/docker-compose.yml to run the broker tests")
	}
	return url
}

// unique suffixes queue names so parallel runs and repeated runs never collide
// on a shared broker.
func unique(prefix string) string {
	return fmt.Sprintf("%s.test-%d", prefix, time.Now().UnixNano())
}

func dial(t *testing.T) *bus.AMQP {
	t.Helper()
	b, err := bus.DialAMQP(brokerURL(t), nil)
	if err != nil {
		t.Fatalf("dial broker: %v", err)
	}
	t.Cleanup(func() { _ = b.Close() })
	return b
}

// TestPublishAndConsumeOverRealBroker: the routing key a service publishes on
// must reach the queue another service binds with.
func TestPublishAndConsumeOverRealBroker(t *testing.T) {
	b := dial(t)

	received := make(chan events.Envelope, 1)
	sub := bus.Subscription{
		Queue:       unique("resolution.recall-raw"),
		Exchange:    events.ExchangeIngestion,
		BindingKeys: []string{"ingestion.recall.raw.received.*"},
	}
	if err := b.Subscribe(sub, func(ctx context.Context, env events.Envelope) error {
		received <- env
		return nil
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	// The ingestion contract is flat, not enveloped: publishing it here proves
	// the boundary adapter works across a real broker, not just in memory.
	notice := events.RecallRawReceived{
		EventID: events.NewID(), EventType: "recall.raw.received", Version: 1,
		OccurredAt: time.Now().UTC(), Producer: "ingestion-fda",
		Source: "fda_enforcement", SourceID: "F-INTEGRATION-1",
		Normalized: events.RecallNormalized{Title: "Integration test notice", Country: "US"},
	}
	body, err := marshal(notice)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	env, err := events.InboundEnvelope(events.TypeRecallRawReceived, body)
	if err != nil {
		t.Fatalf("inbound envelope: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := b.Publish(ctx, env); err != nil {
		t.Fatalf("publish: %v", err)
	}

	select {
	case got := <-received:
		if got.EventID != env.EventID {
			t.Errorf("event id = %s, want %s", got.EventID, env.EventID)
		}
		var decoded events.RecallRawReceived
		if err := got.Into(&decoded); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		if decoded.SourceID != "F-INTEGRATION-1" {
			t.Errorf("payload did not survive the broker: %+v", decoded)
		}
	case <-ctx.Done():
		t.Fatal("nothing arrived: the binding key does not match the routing key services publish on")
	}
}

// TestEnvelopedEventRoundTrip covers the events this layer produces, which carry
// an envelope rather than being flat.
func TestEnvelopedEventRoundTrip(t *testing.T) {
	b := dial(t)

	received := make(chan events.Envelope, 1)
	sub := bus.Subscription{
		Queue:       unique("containment.lot-resolved"),
		Exchange:    events.ExchangeResolution,
		BindingKeys: []string{events.TypeLotResolved},
	}
	if err := b.Subscribe(sub, func(ctx context.Context, env events.Envelope) error {
		received <- env
		return nil
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	incident := "inc-integration-1"
	env, err := events.NewEnvelope(events.TypeLotResolved, "resolution-service", incident, events.LotResolved{
		IncidentID: incident, ResolvedAt: time.Now().UTC(), Confidence: 0.99, Scope: events.ScopeLot,
		Matches: []events.Match{{GTIN: "00041196910537", LotCodes: []string{"8H-1132"}, Confidence: 0.99}},
	})
	if err != nil {
		t.Fatalf("envelope: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := b.Publish(ctx, env); err != nil {
		t.Fatalf("publish: %v", err)
	}

	select {
	case got := <-received:
		if got.CorrelationID != incident {
			t.Errorf("correlation id lost in transit: %q", got.CorrelationID)
		}
		var lr events.LotResolved
		if err := got.Into(&lr); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(lr.Matches) != 1 || lr.Matches[0].LotCodes[0] != "8H-1132" {
			t.Errorf("payload = %+v", lr)
		}
	case <-ctx.Done():
		t.Fatal("enveloped event never arrived")
	}
}

// TestFailingHandlerDeadLetters is the guarantee the audit trail rests on: a
// message a handler cannot process is retried a bounded number of times and then
// ends up somewhere findable, never dropped and never retried forever.
//
// This is the test that caught the original bug. The consumer counted x-death,
// which the broker only sets once a message has actually been dead-lettered, so
// with requeue the count stayed at zero and a poison message cycled forever.
func TestFailingHandlerDeadLetters(t *testing.T) {
	b := dial(t)

	suffix := time.Now().UnixNano()
	queue := fmt.Sprintf("rescue.containment-taken.test-%d", suffix)

	attempts := make(chan int, 64)
	count := 0
	if err := b.Subscribe(bus.Subscription{
		Queue:       queue,
		Exchange:    events.ExchangeContainment,
		BindingKeys: []string{events.TypeContainmentTaken},
	}, func(ctx context.Context, env events.Envelope) error {
		count++
		attempts <- count
		return fmt.Errorf("deliberate failure")
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	// Watch the dead-letter exchange this queue is configured to use, so we can
	// assert the message arrives rather than merely that retries stopped.
	dead := make(chan events.Envelope, 4)
	if err := b.Subscribe(bus.Subscription{
		Queue:       fmt.Sprintf("rescue.deadletter-watch.test-%d", suffix),
		Exchange:    "rescue.dlx",
		BindingKeys: []string{"#"},
	}, func(ctx context.Context, env events.Envelope) error {
		dead <- env
		return nil
	}); err != nil {
		t.Fatalf("subscribe to dlx: %v", err)
	}

	env, err := events.NewEnvelope(events.TypeContainmentTaken, "containment-service", "inc-dlq-1",
		events.ContainmentActionTaken{IncidentID: "inc-dlq-1", Decision: events.DecisionAutoHold})
	if err != nil {
		t.Fatalf("envelope: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := b.Publish(ctx, env); err != nil {
		t.Fatalf("publish: %v", err)
	}

	select {
	case got := <-dead:
		if got.EventID != env.EventID {
			t.Errorf("a different message was dead-lettered: %s", got.EventID)
		}
	case <-ctx.Done():
		t.Fatalf("the message was never dead-lettered after %d attempts: a handler that always fails would retry forever", count)
	}

	// Drain what the handler saw and check the budget was respected. The broker
	// counts deliveries, so the exact number can vary by one; what must not
	// happen is unbounded retrying.
	drained := 0
	for {
		select {
		case n := <-attempts:
			drained = n
		default:
			if drained == 0 {
				t.Fatal("the failing handler was never invoked")
			}
			if drained > bus.MaxRetries+2 {
				t.Errorf("handler ran %d times against a limit of %d", drained, bus.MaxRetries)
			}
			t.Logf("handler attempted %d time(s), then the message was dead-lettered", drained)
			return
		}
	}
}

// TestQueueDeclarationIsIdempotent: services declare their own queues on boot,
// so a redeploy must not fail against a queue that already exists.
func TestQueueDeclarationIsIdempotent(t *testing.T) {
	queue := unique("audit.ledger")

	for i := range 2 {
		b, err := bus.DialAMQP(brokerURL(t), nil)
		if err != nil {
			t.Fatalf("dial %d: %v", i, err)
		}
		err = b.Subscribe(bus.Subscription{
			Queue:       queue,
			Exchange:    events.ExchangeAudit,
			BindingKeys: []string{"#"},
		}, func(ctx context.Context, env events.Envelope) error { return nil })
		if err != nil {
			b.Close()
			t.Fatalf("declare %d: a redeploy must not fail on an existing queue: %v", i, err)
		}
		b.Close()
	}
}

func marshal(v any) ([]byte, error) { return json.Marshal(v) }
