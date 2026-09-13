// Package dossier turns an incident's ledger chain into the artifact a retailer
// hands to an insurer or a regulator: what was recalled, what was held, when,
// on whose authority, who was told, and proof that none of it was edited after
// the fact.
package dossier

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"soteria/libs/core/events"
	"soteria/services/audit-proof-service/ledger"
)

// Dossier is the generated record for one incident.
type Dossier struct {
	DossierID   string    `json:"dossier_id"`
	IncidentID  string    `json:"incident_id"`
	GeneratedAt time.Time `json:"generated_at"`

	// ContentHash is the chain head: the hash covering every record in order.
	ContentHash    string `json:"content_hash"`
	HashAlgorithm  string `json:"hash_algorithm"`
	TimestampProof string `json:"timestamp_proof,omitempty"`
	TimestampNote  string `json:"timestamp_note,omitempty"`

	EventCount int       `json:"event_count"`
	OpenedAt   time.Time `json:"opened_at"`
	ClosedAt   time.Time `json:"closed_at"`

	Summary  Summary         `json:"summary"`
	Timeline []Entry         `json:"timeline"`
	Chain    []ledger.Record `json:"chain"`
}

// Summary is the answer to "what happened", assembled from the events rather
// than written by anyone.
type Summary struct {
	Hazard            string   `json:"hazard,omitempty"`
	Classification    string   `json:"classification,omitempty"`
	Sources           []string `json:"sources,omitempty"`
	Products          []string `json:"products,omitempty"`
	LotsHeld          []string `json:"lots_held,omitempty"`
	Scope             string   `json:"scope,omitempty"`
	Decision          string   `json:"decision,omitempty"`
	DecidedBy         string   `json:"decided_by,omitempty"`
	Confidence        float64  `json:"confidence,omitempty"`
	Threshold         float64  `json:"threshold,omitempty"`
	UnitsHeld         int      `json:"units_held"`
	UnitsLeftSellable int      `json:"units_left_sellable"`
	CustomersOffered  int      `json:"customers_offered"`
	CustomersAnswered int      `json:"customers_answered"`
	NotificationsSent int      `json:"notifications_sent"`
	EvasionFlags      int      `json:"evasion_flags"`
	RedactedFields    []string `json:"redacted_fields,omitempty"`
}

// Entry is one line of the human-readable timeline.
type Entry struct {
	Seq         int       `json:"seq"`
	At          time.Time `json:"at"`
	Actor       string    `json:"actor"`
	Event       string    `json:"event"`
	Detail      string    `json:"detail"`
	EventID     string    `json:"event_id,omitempty"`
	CausationID string    `json:"causation_id,omitempty"`
}

// Timestamper anchors a hash in time. Implementations must not be trusted
// blindly: see Build, which records that no proof was obtained rather than
// failing or pretending.
type Timestamper interface {
	Stamp(ctx context.Context, digest []byte) (token string, err error)
}

// Build assembles a dossier from a chain.
//
// A chain that does not verify still produces a dossier, marked as failing
// verification. Refusing to generate one would leave the operator with nothing
// to investigate, and hiding the failure would be worse than either.
func Build(ctx context.Context, chain ledger.Chain, ts Timestamper, now func() time.Time) (Dossier, error) {
	if len(chain.Records) == 0 {
		return Dossier{}, fmt.Errorf("dossier: incident %s has no recorded events", chain.IncidentID)
	}

	d := Dossier{
		DossierID:     events.NewID(),
		IncidentID:    chain.IncidentID,
		GeneratedAt:   now(),
		ContentHash:   chain.Head,
		HashAlgorithm: "sha256",
		EventCount:    len(chain.Records),
		OpenedAt:      chain.OpenedAt,
		ClosedAt:      chain.UpdatedAt,
		Chain:         chain.Records,
	}

	d.Summary, d.Timeline = summarize(chain.Records)

	if err := chain.Verify(); err != nil {
		d.TimestampNote = "CHAIN VERIFICATION FAILED: " + err.Error()
		return d, nil
	}

	if ts == nil {
		d.TimestampNote = "no timestamp authority configured; the content hash is unanchored"
		return d, nil
	}
	digest, err := hex.DecodeString(chain.Head)
	if err != nil {
		d.TimestampNote = "content hash is not hex; not submitted for timestamping"
		return d, nil
	}
	token, err := ts.Stamp(ctx, digest)
	if err != nil {
		// An unreachable authority must not block containment evidence from
		// existing. Say so on the document instead.
		d.TimestampNote = "timestamp authority unavailable: " + err.Error()
		return d, nil
	}
	d.TimestampProof = token
	return d, nil
}

// Event returns the payload for audit.dossier.generated.v1.
func (d Dossier) Event(pdfURL string) events.AuditDossierGenerated {
	return events.AuditDossierGenerated{
		IncidentID:     d.IncidentID,
		DossierID:      d.DossierID,
		GeneratedAt:    d.GeneratedAt,
		ContentHash:    d.ContentHash,
		HashAlgorithm:  d.HashAlgorithm,
		TimestampProof: d.TimestampProof,
		PDFURL:         pdfURL,
		EventCount:     d.EventCount,
	}
}

// Fingerprint is the hash of the dossier as rendered, so the PDF and the JSON
// can be shown to describe the same thing.
func (d Dossier) Fingerprint() string {
	body, err := json.Marshal(struct {
		IncidentID  string          `json:"incident_id"`
		ContentHash string          `json:"content_hash"`
		EventCount  int             `json:"event_count"`
		Chain       []ledger.Record `json:"chain"`
	}{d.IncidentID, d.ContentHash, d.EventCount, d.Chain})
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// ---------------------------------------------------------------- summarizing

func summarize(records []ledger.Record) (Summary, []Entry) {
	var s Summary
	var timeline []Entry
	redacted := map[string]bool{}
	products := map[string]bool{}
	lots := map[string]bool{}
	sources := map[string]bool{}

	for _, rec := range records {
		for _, f := range rec.Redacted {
			redacted[f] = true
		}
		entry := Entry{
			Seq: rec.Seq, At: rec.OccurredAt, Actor: rec.Producer, Event: rec.EventType,
			EventID: rec.EventID, CausationID: rec.CausationID,
		}

		switch rec.EventType {
		case events.TypeLotResolved:
			var p events.LotResolved
			if json.Unmarshal(rec.Payload, &p) == nil {
				s.Hazard = firstNonEmpty(s.Hazard, p.Hazard)
				s.Classification = firstNonEmpty(s.Classification, p.Classification)
				s.Scope = p.Scope
				s.Confidence = p.Confidence
				if p.Signal.Source != "" {
					sources[p.Signal.Source+" "+p.Signal.SourceRef] = true
				}
				for _, m := range p.Matches {
					products[label(m.ProductTitle, m.GTIN)] = true
					for _, l := range m.LotCodes {
						lots[l] = true
					}
				}
				entry.Detail = fmt.Sprintf("resolved to %d product(s), scope %s, confidence %.2f",
					len(p.Matches), p.Scope, p.Confidence)
			}

		case events.TypeContainmentProposed:
			var p events.ContainmentActionProposed
			if json.Unmarshal(rec.Payload, &p) == nil {
				entry.Detail = fmt.Sprintf("queued for human review: %s", p.Reason)
			}

		case events.TypeContainmentTaken:
			var p events.ContainmentActionTaken
			if json.Unmarshal(rec.Payload, &p) == nil {
				s.Decision = p.Decision
				s.DecidedBy = p.Actor
				s.Threshold = p.Threshold
				if p.Confidence > 0 {
					s.Confidence = p.Confidence
				}
				held, sellable := 0, 0
				for _, r := range p.Results {
					held += r.UnitsHeld
					sellable += r.UnitsLeftSellable
				}
				s.UnitsHeld += held
				s.UnitsLeftSellable = sellable
				for _, t := range p.Targets {
					products[label(t.ProductTitle, t.GTIN)] = true
					for _, l := range t.LotCodes {
						lots[l] = true
					}
				}
				entry.Detail = fmt.Sprintf("%s by %s: %d units held, %d left sellable",
					p.Decision, orSystem(p.Actor), held, sellable)
			}

		case events.TypeOrderRescueProposed:
			s.CustomersOffered++
			var p events.OrderRescueProposed
			if json.Unmarshal(rec.Payload, &p) == nil {
				entry.Detail = fmt.Sprintf("order %s: %d option(s) offered for lot %s",
					p.OrderID, len(p.Options), p.AffectedLine.LotCode)
			}

		case events.TypeOrderRescueConfirmed:
			s.CustomersAnswered++
			var p events.OrderRescueConfirmed
			if json.Unmarshal(rec.Payload, &p) == nil {
				entry.Detail = fmt.Sprintf("order %s: customer chose %s", p.OrderID, p.Choice)
			}

		case events.TypeEvasionFlagged:
			s.EvasionFlags++
			var p events.EvasionFlagged
			if json.Unmarshal(rec.Payload, &p) == nil {
				entry.Detail = fmt.Sprintf("recalled lot %s resurfaced on %s", p.LotCode, p.Marketplace)
			}

		default:
			// notification.delivered.v1 and anything else added later: the proof
			// that a customer was told matters, so unknown types are still listed
			// rather than silently dropped.
			if strings.HasPrefix(rec.EventType, "notification.") {
				s.NotificationsSent++
			}
			entry.Detail = summarizeUnknown(rec.Payload)
		}

		timeline = append(timeline, entry)
	}

	timeline = order(timeline)

	s.Products = keys(products)
	s.LotsHeld = keys(lots)
	s.Sources = keys(sources)
	s.RedactedFields = keys(redacted)
	return s, timeline
}

// order puts the timeline in the sequence a reader expects.
//
// Arrival order is not it: a broker delivers to queues concurrently, and an
// in-process bus runs nested handlers before the original event reaches later
// subscribers. Wall-clock order alone is not it either: events minted in the
// same millisecond tie, and a clock with coarse resolution can show a rescue
// before the containment that caused it, which reads as nonsense in a document
// meant to establish what happened.
//
// So: earliest first, but never before the event that caused it. Each step takes
// the earliest entry whose cause has already been emitted.
func order(entries []Entry) []Entry {
	emitted := make(map[string]bool, len(entries))
	remaining := append([]Entry(nil), entries...)
	sort.SliceStable(remaining, func(i, j int) bool {
		if remaining[i].At.Equal(remaining[j].At) {
			return remaining[i].Seq < remaining[j].Seq
		}
		return remaining[i].At.Before(remaining[j].At)
	})

	out := make([]Entry, 0, len(remaining))
	for len(remaining) > 0 {
		pick := -1
		for i, e := range remaining {
			// Ready when it has no recorded cause, or its cause is already out.
			if e.CausationID == "" || emitted[e.CausationID] || !known(entries, e.CausationID) {
				pick = i
				break
			}
		}
		if pick == -1 {
			// A cycle, or a cause that arrives later than its effect. Emit the
			// earliest remaining rather than dropping anything: an incomplete
			// timeline would be worse than an imperfectly ordered one.
			pick = 0
		}
		e := remaining[pick]
		out = append(out, e)
		emitted[e.EventID] = true
		remaining = append(remaining[:pick], remaining[pick+1:]...)
	}
	return out
}

// known reports whether a causing event is itself in this dossier. A cause
// recorded against another incident must not block its effect from appearing.
func known(entries []Entry, eventID string) bool {
	for _, e := range entries {
		if e.EventID == eventID {
			return true
		}
	}
	return false
}

// summarizeUnknown gives a one-line description of a payload this service does
// not have a typed view of, so the timeline is never blank.
func summarizeUnknown(payload json.RawMessage) string {
	var doc map[string]any
	if json.Unmarshal(payload, &doc) != nil {
		return ""
	}
	for _, k := range []string{"channel", "status", "to", "order_id", "marketplace"} {
		if v, ok := doc[k]; ok {
			return fmt.Sprintf("%s=%v", k, v)
		}
	}
	return ""
}

func label(title, gtin string) string {
	if title == "" {
		return gtin
	}
	return fmt.Sprintf("%s (%s)", title, gtin)
}

func orSystem(actor string) string {
	if actor == "" {
		return "system"
	}
	return actor
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		if k != "" {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}
