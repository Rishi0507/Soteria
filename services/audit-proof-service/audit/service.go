// Package audit is the service core: it records every event that touches an
// incident and generates the dossier on demand.
package audit

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"soteria/libs/core/bus"
	"soteria/libs/core/events"
	"soteria/services/audit-proof-service/dossier"
	"soteria/services/audit-proof-service/ledger"
)

const producer = "audit-proof-service"

// Subscriptions are the queues this service owns. It binds with "#" on every
// exchange that carries incident activity: a dossier that only contains the
// events someone remembered to route here would be evidence of nothing.
var Subscriptions = []bus.Subscription{
	{Queue: "audit.ledger", Exchange: events.ExchangeResolution, BindingKeys: []string{"#"}},
	{Queue: "audit.ledger", Exchange: events.ExchangeContainment, BindingKeys: []string{"#"}},
	{Queue: "audit.ledger", Exchange: events.ExchangeRescue, BindingKeys: []string{"#"}},
	{Queue: "audit.ledger", Exchange: events.ExchangeEvasion, BindingKeys: []string{"#"}},
	{Queue: "audit.ledger", Exchange: events.ExchangeNotification, BindingKeys: []string{"#"}},
}

// Service records events and produces dossiers.
type Service struct {
	ledger *ledger.Ledger
	pub    bus.Publisher
	ts     dossier.Timestamper
	logger *slog.Logger
	now    func() time.Time

	mu       sync.RWMutex
	dossiers map[string]dossier.Dossier // by incident id, latest generation
	pdfs     map[string][]byte
	// autoGenerate produces a dossier as soon as containment completes, so the
	// evidence exists without anyone remembering to ask for it.
	autoGenerate bool
}

func New(pub bus.Publisher, ts dossier.Timestamper, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		ledger: ledger.New(), pub: pub, ts: ts, logger: logger,
		now:          func() time.Time { return time.Now().UTC() },
		dossiers:     map[string]dossier.Dossier{},
		pdfs:         map[string][]byte{},
		autoGenerate: true,
	}
}

// Register binds every audit subscription.
func (s *Service) Register(c bus.Consumer) error {
	for _, sub := range Subscriptions {
		if err := c.Subscribe(sub, s.Handle); err != nil {
			return fmt.Errorf("audit: subscribe %s to %s: %w", sub.Queue, sub.Exchange, err)
		}
	}
	return nil
}

// Handle records one event.
//
// It never rejects an event for being unrecognized: this service exists to hold
// everything that happened, including events added after it was written.
func (s *Service) Handle(ctx context.Context, env events.Envelope) error {
	rec, appended := s.ledger.Append(env)
	if !appended {
		return nil // redelivery; already recorded
	}
	s.logger.Info("recorded",
		"incident_id", env.CorrelationID, "event_type", env.EventType, "seq", rec.Seq)

	if s.autoGenerate && env.EventType == events.TypeContainmentTaken {
		if _, err := s.Generate(ctx, env.CorrelationID); err != nil {
			// The dossier failing must not nack the event: the ledger already
			// holds it, and losing the event would be the greater harm.
			s.logger.Error("dossier generation failed", "incident_id", env.CorrelationID, "err", err)
		}
	}
	return nil
}

// Generate builds, stores and announces a dossier for one incident.
func (s *Service) Generate(ctx context.Context, incidentID string) (dossier.Dossier, error) {
	chain, ok := s.ledger.Chain(incidentID)
	if !ok {
		return dossier.Dossier{}, fmt.Errorf("audit: no events recorded for incident %q", incidentID)
	}

	d, err := dossier.Build(ctx, chain, s.ts, s.now)
	if err != nil {
		return dossier.Dossier{}, err
	}
	pdf, err := d.PDF()
	if err != nil {
		return dossier.Dossier{}, err
	}

	s.mu.Lock()
	s.dossiers[incidentID] = d
	s.pdfs[incidentID] = pdf
	s.mu.Unlock()

	env, err := events.NewEnvelope(events.TypeAuditDossier, producer, incidentID, d.Event(s.pdfURL(incidentID)))
	if err != nil {
		return dossier.Dossier{}, err
	}
	if err := s.pub.Publish(ctx, env); err != nil {
		return dossier.Dossier{}, fmt.Errorf("audit: publish audit.dossier.generated.v1: %w", err)
	}

	s.logger.Info("dossier generated",
		"incident_id", incidentID, "dossier_id", d.DossierID,
		"events", d.EventCount, "timestamped", d.TimestampProof != "", "pdf_bytes", len(pdf))
	return d, nil
}

func (s *Service) pdfURL(incidentID string) string {
	return fmt.Sprintf("/v1/dossiers/%s.pdf", incidentID)
}

// Dossier returns the dossier for an incident, regenerating it first when the
// ledger has moved on since it was built.
//
// A dossier is generated as soon as containment completes, but events keep
// arriving after that: a customer answers, a notification is delivered, a
// marketplace listing is flagged. Serving the stale copy would mean handing an
// insurer a document that is missing the end of its own story.
func (s *Service) Dossier(incidentID string) (dossier.Dossier, bool) {
	s.mu.RLock()
	d, ok := s.dossiers[incidentID]
	s.mu.RUnlock()
	if !ok {
		return dossier.Dossier{}, false
	}

	chain, found := s.ledger.Chain(incidentID)
	if !found || chain.Head == d.ContentHash {
		return d, true
	}

	fresh, err := s.Generate(context.Background(), incidentID)
	if err != nil {
		// Serving the stale copy beats serving nothing, but say so in the logs.
		s.logger.Error("could not refresh a stale dossier", "incident_id", incidentID, "err", err)
		return d, true
	}
	return fresh, true
}

// PDF returns the rendered document, refreshed on the same terms as Dossier.
func (s *Service) PDF(incidentID string) ([]byte, bool) {
	if _, ok := s.Dossier(incidentID); !ok {
		return nil, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, ok := s.pdfs[incidentID]
	return b, ok
}

// Incidents lists recorded incidents, most recent first.
func (s *Service) Incidents() []string { return s.ledger.Incidents() }

// Chain exposes the raw chain, so a recipient can verify independently.
func (s *Service) Chain(incidentID string) (ledger.Chain, bool) { return s.ledger.Chain(incidentID) }

// SetAutoGenerate controls whether containment completion triggers a dossier.
func (s *Service) SetAutoGenerate(on bool) { s.autoGenerate = on }
