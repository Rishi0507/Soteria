// Package source defines the contract every feed connector implements.
//
// A Source knows how to fetch recall notices from one upstream feed and
// project them into Items. It performs no parsing intelligence — lot
// extraction, product resolution and severity decisions belong to the
// resolution-service downstream.
package source

import (
	"context"
	"encoding/json"
	"time"
)

// Normalized is the light projection of a notice shared by every feed.
// Fields are verbatim from the feed; empty strings are serialized as null
// by the event package.
type Normalized struct {
	Title              string
	Firm               string
	ProductDescription string
	Reason             string
	CodeInfo           string
	Classification     string
	Distribution       string
	Country            string // "US" or "EU"
}

// Item is one notice as fetched from a feed.
type Item struct {
	// SourceID is the feed's own identifier and the dedup key within a source.
	SourceID string
	// SourceURL is a human-readable page for the notice, if any.
	SourceURL string
	// PublishedAt is the feed's own timestamp, nil if the feed has none.
	PublishedAt *time.Time
	Normalized  Normalized
	// Raw is the untouched upstream record (must be a JSON object).
	Raw json.RawMessage
}

// Source is one upstream recall feed.
type Source interface {
	// Name is the canonical source identifier used in events
	// (fda_enforcement, fda_press, usda_fsis, eu_rasff).
	Name() string
	// Fetch returns notices published at or after since. Feeds that cannot
	// filter server-side return everything; the poller dedupes.
	Fetch(ctx context.Context, since time.Time) ([]Item, error)
}

// Known source names, mirrored in contracts/events/recall.raw.received.v1.json.
const (
	FDAEnforcement = "fda_enforcement"
	FDAPress       = "fda_press"
	USDAFSIS       = "usda_fsis"
	EURASFF        = "eu_rasff"
)

var knownSources = map[string]bool{FDAEnforcement: true, FDAPress: true, USDAFSIS: true, EURASFF: true}

// IsKnown reports whether name is one of the contract's source enum values.
func IsKnown(name string) bool { return knownSources[name] }
