// Package event builds and validates recall.raw.received.v1 events.
package event

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"soteria/libs/feedkit/source"
)

const (
	Type       = "recall.raw.received"
	Version    = 1
	Exchange   = "ingestion.x"
	RoutingKey = "ingestion.recall.raw.received.v1"
)

// Normalized mirrors source.Normalized with the contract's JSON shape
// (empty optional strings become null).
type Normalized struct {
	Title              string  `json:"title"`
	Firm               *string `json:"firm"`
	ProductDescription *string `json:"product_description"`
	Reason             *string `json:"reason"`
	CodeInfo           *string `json:"code_info"`
	Classification     *string `json:"classification"`
	Distribution       *string `json:"distribution"`
	Country            string  `json:"country"`
}

// Event is one recall.raw.received.v1 message.
type Event struct {
	EventID     string          `json:"event_id"`
	EventType   string          `json:"event_type"`
	Version     int             `json:"version"`
	OccurredAt  time.Time       `json:"occurred_at"`
	Producer    string          `json:"producer"`
	Source      string          `json:"source"`
	SourceID    string          `json:"source_id"`
	SourceURL   *string         `json:"source_url"`
	PublishedAt *time.Time      `json:"published_at"`
	Normalized  Normalized      `json:"normalized"`
	Raw         json.RawMessage `json:"raw"`
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// New builds an event for an item fetched by the named source.
func New(producer, sourceName string, it source.Item, now time.Time) Event {
	n := it.Normalized
	return Event{
		EventID:     uuid.NewString(),
		EventType:   Type,
		Version:     Version,
		OccurredAt:  now.UTC(),
		Producer:    producer,
		Source:      sourceName,
		SourceID:    it.SourceID,
		SourceURL:   nullable(it.SourceURL),
		PublishedAt: utc(it.PublishedAt),
		Normalized: Normalized{
			Title:              n.Title,
			Firm:               nullable(n.Firm),
			ProductDescription: nullable(n.ProductDescription),
			Reason:             nullable(n.Reason),
			CodeInfo:           nullable(n.CodeInfo),
			Classification:     nullable(n.Classification),
			Distribution:       nullable(n.Distribution),
			Country:            n.Country,
		},
		Raw: it.Raw,
	}
}

func utc(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

// Validate enforces the structural rules of the contract without needing the
// JSON Schema file at runtime. The contract test in each service additionally
// validates against contracts/events/recall.raw.received.v1.json.
func (e Event) Validate() error {
	var errs []error
	if _, err := uuid.Parse(e.EventID); err != nil {
		errs = append(errs, fmt.Errorf("event_id: %w", err))
	}
	if e.EventType != Type {
		errs = append(errs, fmt.Errorf("event_type: got %q want %q", e.EventType, Type))
	}
	if e.Version != Version {
		errs = append(errs, fmt.Errorf("version: got %d want %d", e.Version, Version))
	}
	if e.OccurredAt.IsZero() {
		errs = append(errs, errors.New("occurred_at: zero"))
	}
	if e.Producer == "" {
		errs = append(errs, errors.New("producer: empty"))
	}
	if !source.IsKnown(e.Source) {
		errs = append(errs, fmt.Errorf("source: unknown %q", e.Source))
	}
	if e.SourceID == "" {
		errs = append(errs, errors.New("source_id: empty"))
	}
	if e.Normalized.Title == "" {
		errs = append(errs, errors.New("normalized.title: empty"))
	}
	if c := e.Normalized.Country; c != "US" && c != "EU" {
		errs = append(errs, fmt.Errorf("normalized.country: %q not in [US EU]", c))
	}
	if len(e.Raw) == 0 || e.Raw[0] != '{' || !json.Valid(e.Raw) {
		errs = append(errs, errors.New("raw: must be a JSON object"))
	}
	return errors.Join(errs...)
}

// Marshal validates and encodes the event.
func (e Event) Marshal() ([]byte, error) {
	if err := e.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(e)
}
