// Package resolver turns raw recall signals into lot-level resolutions.
//
// It consumes recall.raw.received.v1 and catalog.sku.vanished.v1 from the ingestion
// services and publishes lot.resolved.v1 (/contracts/events).
package resolver

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"soteria/libs/core/bus"
	"soteria/libs/core/events"
	"soteria/libs/core/matching"
	"soteria/libs/core/offacts"
	"soteria/services/resolution-service/catalog"
)

const (
	producer = "resolution-service"
	// MatchFloor is the confidence below which a candidate is not even reported.
	// It is deliberately low: the auto-hold decision belongs to containment-service,
	// this floor only keeps obvious noise out of the incident record.
	MatchFloor = 0.30
	// MaxMatches caps how many products one notice may resolve to.
	MaxMatches = 10
)

// Subscription is the queue this service owns (/contracts/rabbitmq-topology.md).
// The recall binding is a pattern so a future .v2 of the ingestion contract still
// lands here rather than silently vanishing.
var Subscription = bus.Subscription{
	Queue:    "resolution.recall-raw",
	Exchange: events.ExchangeIngestion,
	BindingKeys: []string{
		"ingestion.recall.raw.received.*",
		events.TypeCatalogSKUVanished,
	},
}

// Resolver is the service core.
type Resolver struct {
	catalog catalog.Catalog
	pub     bus.Publisher
	store   *Store
	logger  *slog.Logger

	mu   sync.Mutex
	seen map[string]bool // event_id -> handled; redelivery is expected
}

func New(cat catalog.Catalog, pub bus.Publisher, store *Store, logger *slog.Logger) *Resolver {
	if logger == nil {
		logger = slog.Default()
	}
	return &Resolver{catalog: cat, pub: pub, store: store, logger: logger, seen: map[string]bool{}}
}

// Register wires the resolver onto a consumer.
func (r *Resolver) Register(c bus.Consumer) error {
	return c.Subscribe(Subscription, r.Handle)
}

// Handle dispatches one delivery.
func (r *Resolver) Handle(ctx context.Context, env events.Envelope) error {
	if r.alreadyHandled(env.EventID) {
		r.logger.Debug("duplicate delivery ignored", "event_id", env.EventID)
		return nil
	}
	switch {
	case strings.HasPrefix(env.EventType, "ingestion.recall.raw.received."):
		return r.handleRecall(ctx, env)
	case env.EventType == events.TypeCatalogSKUVanished:
		return r.handleVanished(ctx, env)
	default:
		// Bound only to the two keys above; anything else is a topology bug.
		return fmt.Errorf("resolver: unexpected event type %q on %s", env.EventType, Subscription.Queue)
	}
}

func (r *Resolver) alreadyHandled(eventID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.seen[eventID] {
		return true
	}
	r.seen[eventID] = true
	return false
}

func (r *Resolver) handleRecall(ctx context.Context, env events.Envelope) error {
	var in events.RecallRawReceived
	if err := env.Into(&in); err != nil {
		return fmt.Errorf("resolver: decode recall.raw.received.v1: %w", err)
	}
	if in.SourceID == "" || in.Normalized.Title == "" {
		return fmt.Errorf("resolver: recall.raw.received.v1 missing source_id or normalized.title")
	}

	// The ingestion contract hands over verbatim feed text and leaves every piece
	// of parsing to this service: barcodes live in the product description, lot
	// codes in code_info, and severity in a free-form classification string.
	text := in.Text()
	codeInfo := deref(in.Normalized.CodeInfo)
	sig := matching.Signals{
		UPCs:     matching.ExtractGTINs(text),
		Brands:   compact(deref(in.Normalized.Firm)),
		Text:     text,
		LotCodes: append(matching.ExtractLotCodes(codeInfo), matching.ExtractLotCodes(text)...),
	}

	publishedAt := time.Time{}
	if in.PublishedAt != nil {
		publishedAt = *in.PublishedAt
	}
	incidentID := IncidentID(in.Source, in.SourceID)
	return r.resolve(ctx, env, incidentID, sig, events.Signal{
		Kind:        "AGENCY_NOTICE",
		Source:      in.Source,
		SourceRef:   in.SourceID,
		SourceURL:   deref(in.SourceURL),
		PublishedAt: publishedAt,
	}, hazardOf(in), Classification(deref(in.Normalized.Classification)))
}

// hazardOf prefers the feed's stated reason and falls back to the notice title.
func hazardOf(in events.RecallRawReceived) string {
	if reason := deref(in.Normalized.Reason); reason != "" {
		return reason
	}
	return in.Normalized.Title
}

// Classification maps a feed's free-form severity label ("Class I",
// "High - Class I", "alert") onto the canonical enum. Anything unrecognized stays
// UNCLASSIFIED, which containment treats as no evidence of severity rather than
// as low severity.
func Classification(raw string) string {
	l := strings.ToLower(raw)
	switch {
	case strings.Contains(l, "class iii"):
		return "CLASS_III"
	case strings.Contains(l, "class ii"):
		return "CLASS_II"
	case strings.Contains(l, "class i"):
		return "CLASS_I"
	case strings.Contains(l, "high"), strings.Contains(l, "serious"):
		return "CLASS_I"
	default:
		return "UNCLASSIFIED"
	}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (r *Resolver) handleVanished(ctx context.Context, env events.Envelope) error {
	var in events.CatalogSKUVanished
	if err := env.Into(&in); err != nil {
		return fmt.Errorf("resolver: decode catalog.sku.vanished.v1: %w", err)
	}
	if in.SKU == "" {
		return fmt.Errorf("resolver: catalog.sku.vanished.v1 missing sku")
	}
	sig := matching.Signals{
		UPCs:       compact(in.UPC),
		SKUs:       compact(in.SKU),
		Brands:     compact(in.BrandName),
		Text:       strings.TrimSpace(in.BrandName + " " + in.ProductDescription),
		LotCodes:   in.LotsLastSeen,
		SilentDiff: in.VanishConfidence,
	}
	incidentID := IncidentID("SILENT:"+in.CatalogSource, in.SKU)
	return r.resolve(ctx, env, incidentID, sig, events.Signal{
		Kind:        "SILENT_DIFF",
		Source:      in.CatalogSource,
		SourceRef:   in.SKU,
		PublishedAt: in.ObservedAt,
	}, "sku withdrawn from distributor catalog without a public notice", "UNCLASSIFIED")
}

// resolve scores the signal against the catalog and publishes lot.resolved.v1.
func (r *Resolver) resolve(ctx context.Context, env events.Envelope, incidentID string, sig matching.Signals, signal events.Signal, hazard, classification string) error {
	candidates, err := r.catalog.Candidates(ctx)
	if err != nil {
		return fmt.Errorf("resolver: load catalog: %w", err)
	}
	ranked := matching.Rank(sig, candidates, MatchFloor)
	if len(ranked) == 0 {
		// Not an error: most agency notices cover products this retailer does not
		// carry. Nacking here would dead-letter perfectly good traffic.
		r.logger.Info("signal matched no catalog product",
			"incident_id", incidentID, "source", signal.Source, "source_ref", signal.SourceRef)
		return nil
	}
	if len(ranked) > MaxMatches {
		ranked = ranked[:MaxMatches]
	}

	matches := make([]events.Match, 0, len(ranked))
	scope := events.ScopeLot
	best := 0.0
	for _, res := range ranked {
		if len(res.LotCodes) == 0 {
			// No lot codes recoverable for this product: containment would have to
			// take the whole SKU, which is exactly what Soteria exists to avoid -
			// so we mark it and let containment apply a stricter threshold.
			scope = events.ScopeSKU
		}
		if res.Confidence > best {
			best = res.Confidence
		}
		matches = append(matches, events.Match{
			GTIN:         res.Candidate.GTIN,
			UPC:          res.Candidate.UPC,
			SKU:          res.Candidate.SKU,
			ProductTitle: res.Candidate.ProductTitle,
			Brand:        res.Candidate.Brand,
			LotCodes:     res.LotCodes,
			Confidence:   res.Confidence,
			Evidence:     toEvidence(res.Evidence),
		})
	}

	out := events.LotResolved{
		IncidentID:     incidentID,
		ResolutionID:   events.NewID(),
		ResolvedAt:     time.Now().UTC(),
		Confidence:     best,
		Scope:          scope,
		Hazard:         hazard,
		Allergens:      offacts.HazardAllergens(hazard + " " + sig.Text),
		Classification: defaultString(classification, "UNCLASSIFIED"),
		Signal:         signal,
		Matches:        matches,
	}

	envelope, err := events.NewEnvelope(events.TypeLotResolved, producer, incidentID, out)
	if err != nil {
		return err
	}
	envelope.CausationID = env.EventID
	if err := r.pub.Publish(ctx, envelope); err != nil {
		return fmt.Errorf("resolver: publish lot.resolved.v1: %w", err)
	}
	r.store.Put(out)
	r.logger.Info("resolved recall signal",
		"incident_id", incidentID, "scope", scope, "confidence", best, "matches", len(matches))
	return nil
}

// IncidentID is deterministic so replays and follow-up notices from the same
// agency record collapse onto one incident.
func IncidentID(source, ref string) string {
	clean := func(s string) string {
		s = strings.ToLower(strings.TrimSpace(s))
		s = strings.ReplaceAll(s, " ", "-")
		return strings.ReplaceAll(s, ":", "-")
	}
	return "inc-" + clean(source) + "-" + clean(ref)
}

func toEvidence(in []matching.Evidence) []events.MatchEvidence {
	out := make([]events.MatchEvidence, 0, len(in))
	for _, e := range in {
		out = append(out, events.MatchEvidence{Signal: e.Signal, Weight: e.Weight, Detail: e.Detail})
	}
	return out
}

func compact(vals ...string) []string {
	var out []string
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			out = append(out, v)
		}
	}
	return out
}

func defaultString(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
