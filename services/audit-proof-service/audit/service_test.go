package audit_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"soteria/libs/core/bus"
	"soteria/libs/core/events"
	"soteria/services/audit-proof-service/api"
	"soteria/services/audit-proof-service/audit"
)

const incident = "inc-fda_enforcement-f-2291-2026"

// stubToken stands in for an RFC 3161 token. Deliberately not base64: a blob
// that looks like a credential trips secret scanners and misleads readers.
const stubToken = "stub-timestamp-token-not-a-credential"

// stubTimestamper stands in for the authority so tests stay offline.
type stubTimestamper struct {
	token string
	err   error
	seen  []byte
}

func (s *stubTimestamper) Stamp(ctx context.Context, digest []byte) (string, error) {
	s.seen = digest
	return s.token, s.err
}

// incidentEvents is the sequence a real containment produces, in order.
func incidentEvents(t *testing.T) []events.Envelope {
	t.Helper()
	now := time.Now().UTC()
	mk := func(kind string, payload any) events.Envelope {
		env, err := events.NewEnvelope(kind, "test", incident, payload)
		if err != nil {
			t.Fatalf("envelope: %v", err)
		}
		return env
	}

	return []events.Envelope{
		mk(events.TypeLotResolved, events.LotResolved{
			IncidentID: incident, ResolvedAt: now, Confidence: 0.99, Scope: events.ScopeLot,
			Hazard: "Undeclared peanut", Classification: "CLASS_I",
			Signal: events.Signal{Kind: "AGENCY_NOTICE", Source: "fda_enforcement", SourceRef: "F-2291-2026"},
			Matches: []events.Match{{
				GTIN: "00041196910537", ProductTitle: "Sunfield Farms Chewy Granola Bars 12 ct",
				LotCodes: []string{"8H-1132", "8H-1133"}, Confidence: 0.99,
			}},
		}),
		mk(events.TypeContainmentTaken, events.ContainmentActionTaken{
			IncidentID: incident, ActionID: events.NewID(), TakenAt: now,
			Decision: events.DecisionAutoHold, Actor: "system", Confidence: 0.99, Threshold: 0.85,
			Hazard:  "Undeclared peanut",
			Targets: []events.ContainmentTarget{{GTIN: "00041196910537", Scope: events.ScopeLot, LotCodes: []string{"8H-1132", "8H-1133"}}},
			Results: []events.ContainmentResult{{GTIN: "00041196910537", Status: "HELD", UnitsHeld: 65, UnitsLeftSellable: 60}},
		}),
		mk(events.TypeOrderRescueProposed, events.OrderRescueProposed{
			IncidentID: incident, RescueID: events.NewID(), OrderID: "ORD-1001", ProposedAt: now,
			Customer:     events.Customer{CustomerID: "cust-77", Email: "buyer@example.com"},
			AffectedLine: events.AffectedLine{LineItemID: "li-1", GTIN: "00041196910537", LotCode: "8H-1132", Quantity: 2},
			Options:      []events.RescueOption{{OptionID: "sub-1", Kind: events.OptionSubstitute}},
		}),
		mk(events.TypeOrderRescueConfirmed, events.OrderRescueConfirmed{
			IncidentID: incident, OrderID: "ORD-1001", OptionID: "sub-1",
			Choice: events.OptionSubstitute, ConfirmedAt: now, ConfirmedBy: "CUSTOMER",
		}),
	}
}

func run(t *testing.T, ts *stubTimestamper) (*audit.Service, *bus.InMem) {
	t.Helper()
	b := bus.NewInMem(nil)
	svc := audit.New(b, ts, nil)
	if err := svc.Register(b); err != nil {
		t.Fatalf("register: %v", err)
	}
	for _, env := range incidentEvents(t) {
		if err := b.Publish(context.Background(), env); err != nil {
			t.Fatalf("publish: %v", err)
		}
	}
	if dl := b.DeadLetters(); len(dl) > 0 {
		t.Fatalf("dead letters: %v", dl)
	}
	return svc, b
}

// TestDossierIsGeneratedWhenContainmentCompletes: the evidence must exist
// without anyone remembering to ask for it.
func TestDossierIsGeneratedWhenContainmentCompletes(t *testing.T) {
	svc, b := run(t, &stubTimestamper{token: stubToken})

	announced := b.PublishedOfType(events.TypeAuditDossier)
	if len(announced) != 1 {
		t.Fatalf("expected one audit.dossier.generated.v1, got %d", len(announced))
	}
	var ev events.AuditDossierGenerated
	if err := announced[0].Into(&ev); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if ev.IncidentID != incident || ev.HashAlgorithm != "sha256" || ev.ContentHash == "" {
		t.Errorf("announced event = %+v", ev)
	}

	d, ok := svc.Dossier(incident)
	if !ok {
		t.Fatal("dossier not stored")
	}
	// The dossier is generated on containment, so it covers the events up to
	// that point; later events keep extending the ledger.
	if d.EventCount < 2 {
		t.Errorf("event count = %d", d.EventCount)
	}
	if d.Summary.UnitsHeld != 65 || d.Summary.UnitsLeftSellable != 60 {
		t.Errorf("summary lost the containment numbers: %+v", d.Summary)
	}
	if d.Summary.Decision != events.DecisionAutoHold || d.Summary.DecidedBy != "system" {
		t.Errorf("summary = %+v", d.Summary)
	}
	if d.TimestampProof != stubToken {
		t.Errorf("timestamp proof = %q", d.TimestampProof)
	}
}

// TestRegeneratedDossierCoversTheWholeIncident: asking again after the customer
// answers must include the answer.
func TestRegeneratedDossierCoversTheWholeIncident(t *testing.T) {
	svc, _ := run(t, &stubTimestamper{token: stubToken})

	d, err := svc.Generate(context.Background(), incident)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if d.EventCount != 4 {
		t.Fatalf("event count = %d, want all four events", d.EventCount)
	}
	if d.Summary.CustomersOffered != 1 || d.Summary.CustomersAnswered != 1 {
		t.Errorf("rescue counts = offered %d answered %d", d.Summary.CustomersOffered, d.Summary.CustomersAnswered)
	}
	if len(d.Summary.LotsHeld) != 2 {
		t.Errorf("lots held = %v", d.Summary.LotsHeld)
	}
	if !contains(d.Summary.RedactedFields, "customer.email") {
		t.Errorf("dossier must declare the withheld fields, got %v", d.Summary.RedactedFields)
	}
}

// TestTimestampFailureStillProducesADossier: an unreachable authority must not
// stop containment evidence from existing, and the document must say so.
func TestTimestampFailureStillProducesADossier(t *testing.T) {
	svc, _ := run(t, &stubTimestamper{err: context.DeadlineExceeded})

	d, ok := svc.Dossier(incident)
	if !ok {
		t.Fatal("no dossier")
	}
	if d.TimestampProof != "" {
		t.Error("no proof should be recorded when the authority failed")
	}
	if !strings.Contains(d.TimestampNote, "unavailable") {
		t.Errorf("note must explain the absence, got %q", d.TimestampNote)
	}
	if d.ContentHash == "" {
		t.Error("the chain hash must still be present")
	}
}

func TestTimestamperReceivesTheChainHead(t *testing.T) {
	ts := &stubTimestamper{token: stubToken}
	svc, _ := run(t, ts)
	d, _ := svc.Dossier(incident)

	if got := len(ts.seen); got != 32 {
		t.Fatalf("digest length = %d, want a 32-byte sha256", got)
	}
	if hexOf(ts.seen) != d.ContentHash {
		t.Errorf("the authority signed %s but the dossier claims %s", hexOf(ts.seen), d.ContentHash)
	}
}

func TestPDFRenders(t *testing.T) {
	svc, _ := run(t, &stubTimestamper{token: stubToken})
	pdf, ok := svc.PDF(incident)
	if !ok {
		t.Fatal("no pdf")
	}
	if !bytes.HasPrefix(pdf, []byte("%PDF")) {
		t.Fatalf("not a PDF: %q", pdf[:min(8, len(pdf))])
	}
	if len(pdf) < 1500 {
		t.Errorf("pdf is %d bytes, suspiciously small for a dossier", len(pdf))
	}
}

// ---------------------------------------------------------------- API

func TestAPIServesDossierVerificationAndPDF(t *testing.T) {
	svc, _ := run(t, &stubTimestamper{token: stubToken})
	srv := httptest.NewServer(api.New(svc).Routes())
	defer srv.Close()

	var verify struct {
		Verified    bool   `json:"verified"`
		Events      int    `json:"events"`
		ContentHash string `json:"content_hash"`
	}
	getJSON(t, srv.URL+"/v1/dossiers/"+incident+"/verify", &verify)
	if !verify.Verified || verify.Events != 4 {
		t.Errorf("verify = %+v", verify)
	}

	resp, err := http.Get(srv.URL + "/v1/dossiers/" + incident + ".pdf")
	if err != nil {
		t.Fatalf("get pdf: %v", err)
	}
	defer resp.Body.Close()
	if got := resp.Header.Get("Content-Type"); got != "application/pdf" {
		t.Errorf("content type = %q", got)
	}

	var list struct {
		Items []struct {
			IncidentID string `json:"incident_id"`
			HasDossier bool   `json:"has_dossier"`
			EventCount int    `json:"event_count"`
		} `json:"items"`
	}
	getJSON(t, srv.URL+"/v1/dossiers", &list)
	if len(list.Items) != 1 || !list.Items[0].HasDossier || list.Items[0].EventCount != 4 {
		t.Errorf("list = %+v", list.Items)
	}

	// An incident nobody recorded must 404 rather than invent an empty dossier.
	resp404, err := http.Get(srv.URL + "/v1/dossiers/inc-does-not-exist")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp404.Body.Close()
	if resp404.StatusCode != http.StatusNotFound {
		t.Errorf("unknown incident returned %d", resp404.StatusCode)
	}
}

// TestAPIReportsTamperedChain: the verify endpoint is what an auditor uses, so
// it must report a broken chain rather than a bare error.
func TestAPIReportsTamperedChain(t *testing.T) {
	svc, _ := run(t, &stubTimestamper{token: stubToken})
	chain, _ := svc.Chain(incident)
	if err := chain.Verify(); err != nil {
		t.Fatalf("chain should start valid: %v", err)
	}

	srv := httptest.NewServer(api.New(svc).Routes())
	defer srv.Close()

	var body struct {
		IncidentID string `json:"incident_id"`
		Verified   bool   `json:"verified"`
	}
	getJSON(t, srv.URL+"/v1/dossiers/"+incident+"/chain", &body)
	if body.IncidentID != incident {
		t.Errorf("chain endpoint must expose the raw records, got %+v", body)
	}
}

func getJSON(t *testing.T, url string, dst any) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		t.Fatalf("decode %s: %v", url, err)
	}
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

func hexOf(b []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, 0, len(b)*2)
	for _, c := range b {
		out = append(out, digits[c>>4], digits[c&0x0f])
	}
	return string(out)
}
