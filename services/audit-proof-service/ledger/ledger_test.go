package ledger

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"soteria/libs/core/events"
)

func envelope(t *testing.T, incident, eventType string, payload any) events.Envelope {
	t.Helper()
	env, err := events.NewEnvelope(eventType, "test-producer", incident, payload)
	if err != nil {
		t.Fatalf("envelope: %v", err)
	}
	return env
}

func seeded(t *testing.T) *Ledger {
	t.Helper()
	l := New()
	l.Append(envelope(t, "inc-1", events.TypeLotResolved, events.LotResolved{
		IncidentID: "inc-1", Scope: events.ScopeLot, Confidence: 0.99,
	}))
	l.Append(envelope(t, "inc-1", events.TypeContainmentTaken, events.ContainmentActionTaken{
		IncidentID: "inc-1", Decision: events.DecisionAutoHold, Actor: "system",
	}))
	l.Append(envelope(t, "inc-1", events.TypeOrderRescueProposed, events.OrderRescueProposed{
		IncidentID: "inc-1", OrderID: "ORD-1001",
		Customer: events.Customer{CustomerID: "cust-77", Email: "buyer@example.com", Phone: "+15550001111"},
	}))
	return l
}

func TestChainVerifies(t *testing.T) {
	chain, ok := seeded(t).Chain("inc-1")
	if !ok {
		t.Fatal("chain missing")
	}
	if len(chain.Records) != 3 {
		t.Fatalf("records = %d, want 3", len(chain.Records))
	}
	if chain.Records[0].PrevHash != GenesisHash {
		t.Errorf("first record must follow the genesis hash, got %s", chain.Records[0].PrevHash)
	}
	for i := 1; i < len(chain.Records); i++ {
		if chain.Records[i].PrevHash != chain.Records[i-1].RecordHash {
			t.Errorf("record %d is not linked to its predecessor", i+1)
		}
	}
	if err := chain.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
}

// TestTamperingIsDetected is the whole point of the chain: each way of editing
// history must be caught, and the error must name the first bad record.
func TestTamperingIsDetected(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(c *Chain)
		expect string
	}{
		{
			name:   "payload altered",
			mutate: func(c *Chain) { c.Records[1].PayloadHash = strings.Repeat("a", 64) },
			expect: "content altered",
		},
		{
			name:   "record removed",
			mutate: func(c *Chain) { c.Records = append(c.Records[:1], c.Records[2:]...) },
			expect: "sequence",
		},
		{
			name:   "records reordered",
			mutate: func(c *Chain) { c.Records[0], c.Records[1] = c.Records[1], c.Records[0] },
			expect: "sequence",
		},
		{
			name: "record inserted",
			mutate: func(c *Chain) {
				forged := c.Records[2]
				forged.Seq = 3
				c.Records = append(c.Records[:2], append([]Record{forged}, c.Records[2:]...)...)
			},
			expect: "sequence",
		},
		{
			name:   "actor rewritten",
			mutate: func(c *Chain) { c.Records[1].Producer = "somebody-else" },
			expect: "content altered",
		},
		{
			name:   "head replaced",
			mutate: func(c *Chain) { c.Head = strings.Repeat("f", 64) },
			expect: "head",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			chain, _ := seeded(t).Chain("inc-1")
			c.mutate(&chain)
			err := chain.Verify()
			if err == nil {
				t.Fatal("tampering went undetected")
			}
			if !strings.Contains(err.Error(), c.expect) {
				t.Errorf("error %q should mention %q", err, c.expect)
			}
		})
	}
}

// TestPIIIsRedacted: a dossier goes to an insurer or a regulator, and a
// customer's contact details are not theirs to receive. The identity inside the
// retailer's own system stays, because "which order" must remain answerable.
func TestPIIIsRedacted(t *testing.T) {
	chain, _ := seeded(t).Chain("inc-1")
	rescue := chain.Records[2]

	var payload map[string]any
	if err := json.Unmarshal(rescue.Payload, &payload); err != nil {
		t.Fatalf("payload: %v", err)
	}
	customer, _ := payload["customer"].(map[string]any)
	if _, leaked := customer["email"]; leaked {
		t.Error("customer email survived redaction")
	}
	if _, leaked := customer["phone"]; leaked {
		t.Error("customer phone survived redaction")
	}
	if customer["customer_id"] != "cust-77" {
		t.Errorf("customer_id must be kept, got %v", customer["customer_id"])
	}
	if len(rescue.Redacted) != 2 {
		t.Errorf("redaction must be declared, got %v", rescue.Redacted)
	}

	// The hash covers the original payload, so the untouched event still proves
	// itself against this record if it is ever replayed from the bus.
	if err := chain.Verify(); err != nil {
		t.Fatalf("redaction must not break the chain: %v", err)
	}
}

// TestRedeliveryIsNotRecordedTwice: the bus is at-least-once, and a duplicated
// record would make the dossier describe events that did not happen.
func TestRedeliveryIsNotRecordedTwice(t *testing.T) {
	l := New()
	env := envelope(t, "inc-1", events.TypeContainmentTaken, events.ContainmentActionTaken{IncidentID: "inc-1"})

	if _, appended := l.Append(env); !appended {
		t.Fatal("first delivery should be recorded")
	}
	if _, appended := l.Append(env); appended {
		t.Fatal("redelivery must not be recorded again")
	}
	chain, _ := l.Chain("inc-1")
	if len(chain.Records) != 1 {
		t.Fatalf("records = %d, want 1", len(chain.Records))
	}
}

func TestIncidentsAreSeparateChains(t *testing.T) {
	l := New()
	l.Append(envelope(t, "inc-1", events.TypeLotResolved, events.LotResolved{IncidentID: "inc-1"}))
	l.Append(envelope(t, "inc-2", events.TypeLotResolved, events.LotResolved{IncidentID: "inc-2"}))

	for _, id := range []string{"inc-1", "inc-2"} {
		chain, ok := l.Chain(id)
		if !ok || len(chain.Records) != 1 {
			t.Fatalf("%s: chain = %+v", id, chain)
		}
		if chain.Records[0].PrevHash != GenesisHash {
			t.Errorf("%s: each incident starts its own chain", id)
		}
	}
}

// TestEventWithNoIncidentIsStillRecorded: an audit trail that discards what it
// cannot classify is not an audit trail.
func TestEventWithNoIncidentIsStillRecorded(t *testing.T) {
	l := New()
	env, _ := events.NewEnvelope(events.TypeEvasionFlagged, "anti-evasion", "", events.EvasionFlagged{
		FlagID: "f-1", Marketplace: "example", ObservedAt: time.Now().UTC(),
	})
	if _, appended := l.Append(env); !appended {
		t.Fatal("event should be recorded")
	}
	if chain, ok := l.Chain("unattributed"); !ok || len(chain.Records) != 1 {
		t.Fatalf("unattributed chain = %+v", chain)
	}
}
