// Contract test: every event this service publishes must validate against the
// schema in /contracts, so schema drift fails in CI rather than at a regulator's
// desk. Reuses the harness the ingestion services already validate against.
package auditproof

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"soteria/libs/core/bus"
	"soteria/libs/core/events"
	"soteria/libs/feedkit/contracttest"
	"soteria/services/audit-proof-service/audit"
)

func TestDossierEventMatchesContract(t *testing.T) {
	schema := contracttest.Compile(t, contracttest.SchemaPath(t, "audit.dossier.generated.v1.json"))

	b := bus.NewInMem(nil)
	svc := audit.New(b, nil, nil) // no timestamper: the unanchored path must still be valid
	if err := svc.Register(b); err != nil {
		t.Fatalf("register: %v", err)
	}

	env, err := events.NewEnvelope(events.TypeContainmentTaken, "containment-service", "inc-contract-1",
		events.ContainmentActionTaken{
			IncidentID: "inc-contract-1", ActionID: events.NewID(), TakenAt: time.Now().UTC(),
			Decision: events.DecisionAutoHold, Actor: "system", Confidence: 0.99, Threshold: 0.85,
			Targets: []events.ContainmentTarget{{GTIN: "00041196910537", Scope: events.ScopeLot, LotCodes: []string{"8H-1132"}}},
			Results: []events.ContainmentResult{{GTIN: "00041196910537", Status: "HELD", UnitsHeld: 40, UnitsLeftSellable: 60}},
		})
	if err != nil {
		t.Fatalf("envelope: %v", err)
	}
	if err := b.Publish(context.Background(), env); err != nil {
		t.Fatalf("publish: %v", err)
	}

	published := b.PublishedOfType(events.TypeAuditDossier)
	if len(published) != 1 {
		t.Fatalf("expected one dossier event, got %d", len(published))
	}

	var payload json.RawMessage = published[0].Payload
	contracttest.Validate(t, schema, payload)

	// The envelope itself must also satisfy the bus contract.
	if err := published[0].Validate(); err != nil {
		t.Fatalf("envelope invalid: %v", err)
	}
}
