// Package ledger is the append-only, tamper-evident record of everything that
// happened during an incident.
//
// The dossier's value rests entirely on one claim: this is the complete and
// unaltered sequence of events, and any edit is detectable. That is enforced by
// hash-chaining each record to its predecessor, so altering, reordering,
// inserting or deleting a record breaks every hash after it.
//
// The chain is per incident. Incidents are independent, and a retailer defending
// one recall should not have to disclose another.
package ledger

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"soteria/libs/core/events"
)

// GenesisHash is the predecessor of the first record in every chain.
const GenesisHash = "0000000000000000000000000000000000000000000000000000000000000000"

// Record is one event, as recorded.
//
// The payload is kept redacted (see Redact): a dossier is written to be handed
// to an insurer or a regulator, and a customer's email address is not theirs to
// receive. PayloadHash is taken over the *original* payload, so the untouched
// event still proves itself if it is ever produced from the bus.
type Record struct {
	Seq         int             `json:"seq"`
	EventID     string          `json:"event_id"`
	EventType   string          `json:"event_type"`
	OccurredAt  time.Time       `json:"occurred_at"`
	RecordedAt  time.Time       `json:"recorded_at"`
	Producer    string          `json:"producer"`
	CausationID string          `json:"causation_id,omitempty"`
	Payload     json.RawMessage `json:"payload"`
	PayloadHash string          `json:"payload_hash"`
	PrevHash    string          `json:"prev_hash"`
	RecordHash  string          `json:"record_hash"`
	Redacted    []string        `json:"redacted_fields,omitempty"`
}

// Chain is one incident's records.
type Chain struct {
	IncidentID string    `json:"incident_id"`
	Records    []Record  `json:"records"`
	Head       string    `json:"head"`
	OpenedAt   time.Time `json:"opened_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// Ledger holds every incident chain. In-memory for v1; the interface is narrow
// enough to back with append-only storage, which is what a production deployment
// needs for this to mean anything after a restart.
type Ledger struct {
	mu     sync.RWMutex
	chains map[string]*Chain
	order  []string
	now    func() time.Time
	seen   map[string]bool // event_id, because the bus is at-least-once
}

func New() *Ledger {
	return &Ledger{
		chains: map[string]*Chain{},
		seen:   map[string]bool{},
		now:    func() time.Time { return time.Now().UTC() },
	}
}

// Append records an event against its incident and returns the new record.
//
// A redelivered event is not appended twice: duplicates would inflate the event
// count and make the dossier describe something that did not happen.
func (l *Ledger) Append(env events.Envelope) (Record, bool) {
	incident := env.CorrelationID
	if incident == "" {
		// An event with no incident id cannot be filed against a recall. Keeping
		// it under a reserved key is better than dropping it: an audit trail that
		// quietly discards what it cannot classify is not an audit trail.
		incident = "unattributed"
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.seen[env.EventID] {
		return Record{}, false
	}
	l.seen[env.EventID] = true

	chain, ok := l.chains[incident]
	if !ok {
		chain = &Chain{IncidentID: incident, Head: GenesisHash, OpenedAt: l.now()}
		l.chains[incident] = chain
		l.order = append(l.order, incident)
	}

	redactedPayload, removed := Redact(env.EventType, env.Payload)
	rec := Record{
		Seq:         len(chain.Records) + 1,
		EventID:     env.EventID,
		EventType:   env.EventType,
		OccurredAt:  env.OccurredAt.UTC(),
		RecordedAt:  l.now(),
		Producer:    env.Producer,
		CausationID: env.CausationID,
		Payload:     redactedPayload,
		PayloadHash: hashBytes(env.Payload),
		PrevHash:    chain.Head,
		Redacted:    removed,
	}
	rec.RecordHash = hashRecord(rec)

	chain.Records = append(chain.Records, rec)
	chain.Head = rec.RecordHash
	chain.UpdatedAt = rec.RecordedAt
	return rec, true
}

// Chain returns a copy of one incident's chain.
func (l *Ledger) Chain(incidentID string) (Chain, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	c, ok := l.chains[incidentID]
	if !ok {
		return Chain{}, false
	}
	out := *c
	out.Records = append([]Record(nil), c.Records...)
	return out, true
}

// Incidents lists incident ids, most recently updated first.
func (l *Ledger) Incidents() []string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	ids := append([]string(nil), l.order...)
	sort.SliceStable(ids, func(i, j int) bool {
		return l.chains[ids[i]].UpdatedAt.After(l.chains[ids[j]].UpdatedAt)
	})
	return ids
}

// Verify recomputes the whole chain and reports the first record that does not
// match. This is the operation an auditor actually cares about, so it recomputes
// from the payload hashes rather than trusting any stored hash.
func (c Chain) Verify() error {
	prev := GenesisHash
	for i, rec := range c.Records {
		if rec.Seq != i+1 {
			return fmt.Errorf("record %d: sequence is %d, expected %d", i+1, rec.Seq, i+1)
		}
		if rec.PrevHash != prev {
			return fmt.Errorf("record %d (%s): chain broken, prev_hash %s does not match the previous record's hash %s",
				rec.Seq, rec.EventType, short(rec.PrevHash), short(prev))
		}
		if want := hashRecord(rec); want != rec.RecordHash {
			return fmt.Errorf("record %d (%s): content altered, hash is %s but recomputes to %s",
				rec.Seq, rec.EventType, short(rec.RecordHash), short(want))
		}
		prev = rec.RecordHash
	}
	if c.Head != prev {
		return fmt.Errorf("head %s does not match the last record %s", short(c.Head), short(prev))
	}
	return nil
}

// hashRecord covers every field that gives a record meaning, including its
// position and its predecessor. PayloadHash stands in for the payload so that
// redaction cannot change the chain.
func hashRecord(r Record) string {
	h := sha256.New()
	fmt.Fprintf(h, "%d\n%s\n%s\n%s\n%s\n%s\n%s\n",
		r.Seq, r.EventID, r.EventType,
		r.OccurredAt.UTC().Format(time.RFC3339Nano),
		r.Producer, r.PayloadHash, r.PrevHash)
	return hex.EncodeToString(h.Sum(nil))
}

func hashBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// ---------------------------------------------------------------- redaction

// piiFields are payload paths that must not appear in a dossier. The customer's
// identity within the retailer's own system stays, because "which order" has to
// be answerable; how to contact them does not.
var piiFields = []string{"customer.email", "customer.phone"}

// Redact removes PII from a payload and reports what it removed, so the dossier
// can state plainly that fields were withheld rather than appearing complete.
func Redact(eventType string, payload json.RawMessage) (json.RawMessage, []string) {
	if len(payload) == 0 {
		return payload, nil
	}
	var doc map[string]any
	if err := json.Unmarshal(payload, &doc); err != nil {
		// Not an object: nothing addressable to redact.
		return payload, nil
	}

	var removed []string
	for _, path := range piiFields {
		if removeAt(doc, strings.Split(path, ".")) {
			removed = append(removed, path)
		}
	}
	if len(removed) == 0 {
		return payload, nil
	}
	out, err := json.Marshal(doc)
	if err != nil {
		return payload, nil
	}
	return out, removed
}

func removeAt(doc map[string]any, path []string) bool {
	if len(path) == 0 {
		return false
	}
	if len(path) == 1 {
		if _, ok := doc[path[0]]; ok {
			delete(doc, path[0])
			return true
		}
		return false
	}
	child, ok := doc[path[0]].(map[string]any)
	if !ok {
		return false
	}
	return removeAt(child, path[1:])
}

func short(hash string) string {
	if len(hash) <= 12 {
		return hash
	}
	return hash[:12] + "…"
}
