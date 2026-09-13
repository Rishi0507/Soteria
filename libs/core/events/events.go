// Package events is the Go binding of /contracts/events/*.json.
//
// Every struct here mirrors a JSON Schema in /contracts. If you change a field,
// change the schema in the same PR and bump the version if the change is breaking
// (see /contracts/rabbitmq-topology.md, "Versioning").
package events

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// Routing keys, which are also the envelope event types on this bus.
// Pattern: <context>.<subject>.<action>.<version>, where <context> is the
// publishing exchange without its ".x" suffix. See /contracts/rabbitmq-topology.md.
const (
	// TypeRecallRawReceived is produced by the ingestion services and is the one
	// inbound contract this layer does not own; its payload is flat rather than
	// enveloped (see RecallRawReceived and InboundEnvelope).
	TypeRecallRawReceived  = "ingestion.recall.raw.received.v1"
	TypeCatalogSKUVanished = "ingestion.catalog.sku.vanished.v1"

	TypeLotResolved          = "resolution.lot.resolved.v1"
	TypeContainmentProposed  = "containment.action.proposed.v1"
	TypeContainmentTaken     = "containment.action.taken.v1"
	TypeOrderRescueProposed  = "rescue.order.proposed.v1"
	TypeOrderRescueConfirmed = "rescue.order.confirmed.v1"
	TypeEvasionFlagged       = "evasion.flagged.v1"
	TypeAuditDossier         = "audit.dossier.generated.v1"
)

// Exchanges, one per bounded context.
const (
	ExchangeIngestion    = "ingestion.x"
	ExchangeResolution   = "resolution.x"
	ExchangeContainment  = "containment.x"
	ExchangeRescue       = "rescue.x"
	ExchangeEvasion      = "evasion.x"
	ExchangeAudit        = "audit.x"
	ExchangeNotification = "notification.x"
)

// ExchangeFor returns the exchange an event type is published to.
func ExchangeFor(eventType string) (string, error) {
	switch eventType {
	case TypeRecallRawReceived, TypeCatalogSKUVanished:
		return ExchangeIngestion, nil
	case TypeLotResolved:
		return ExchangeResolution, nil
	case TypeContainmentProposed, TypeContainmentTaken:
		return ExchangeContainment, nil
	case TypeOrderRescueProposed, TypeOrderRescueConfirmed:
		return ExchangeRescue, nil
	case TypeEvasionFlagged:
		return ExchangeEvasion, nil
	case TypeAuditDossier:
		return ExchangeAudit, nil
	default:
		return "", fmt.Errorf("events: unknown event type %q, not in /contracts", eventType)
	}
}

// Envelope is _envelope.v1.json. Consumers route on EventType, never on queue name.
type Envelope struct {
	EventID       string          `json:"event_id"`
	EventType     string          `json:"event_type"`
	EventVersion  int             `json:"event_version"`
	OccurredAt    time.Time       `json:"occurred_at"`
	Producer      string          `json:"producer"`
	CorrelationID string          `json:"correlation_id,omitempty"`
	CausationID   string          `json:"causation_id,omitempty"`
	Payload       json.RawMessage `json:"payload"`
}

// NewEnvelope builds a valid envelope around any payload struct in this package.
func NewEnvelope(eventType, producer, correlationID string, payload any) (Envelope, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return Envelope{}, fmt.Errorf("events: marshal payload: %w", err)
	}
	return Envelope{
		EventID:       NewID(),
		EventType:     eventType,
		EventVersion:  1,
		OccurredAt:    time.Now().UTC(),
		Producer:      producer,
		CorrelationID: correlationID,
		Payload:       body,
	}, nil
}

// Validate enforces the envelope invariants the schema declares as required.
func (e Envelope) Validate() error {
	switch {
	case e.EventID == "":
		return fmt.Errorf("events: event_id is required")
	case e.EventType == "":
		return fmt.Errorf("events: event_type is required")
	case e.EventVersion < 1:
		return fmt.Errorf("events: event_version must be >= 1")
	case e.OccurredAt.IsZero():
		return fmt.Errorf("events: occurred_at is required")
	case e.Producer == "":
		return fmt.Errorf("events: producer is required")
	case len(e.Payload) == 0:
		return fmt.Errorf("events: payload is required")
	}
	if _, err := ExchangeFor(e.EventType); err != nil {
		return err
	}
	return nil
}

// Into unmarshals the payload into dst.
func (e Envelope) Into(dst any) error {
	return json.Unmarshal(e.Payload, dst)
}

// NewID returns a RFC-4122-shaped v4 UUID without pulling in a dependency.
func NewID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("events: entropy unavailable: %v", err))
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

// ---------------------------------------------------------------- ingestion signals (inbound)

// RecallNormalized is the light projection the ingestion services apply. Every
// optional field is a verbatim string from the feed or null: lot parsing, barcode
// resolution and severity mapping are this layer's job, not theirs.
type RecallNormalized struct {
	Title              string  `json:"title"`
	Firm               *string `json:"firm"`
	ProductDescription *string `json:"product_description"`
	Reason             *string `json:"reason"`
	CodeInfo           *string `json:"code_info"`
	Classification     *string `json:"classification"`
	Distribution       *string `json:"distribution"`
	Country            string  `json:"country"`
}

// RecallRawReceived mirrors contracts/events/recall.raw.received.v1.json, which is
// owned by the ingestion services. Unlike the events this layer publishes, it is a
// flat message: its metadata sits alongside its data rather than in an envelope.
type RecallRawReceived struct {
	EventID     string           `json:"event_id"`
	EventType   string           `json:"event_type"` // always "recall.raw.received"
	Version     int              `json:"version"`
	OccurredAt  time.Time        `json:"occurred_at"`
	Producer    string           `json:"producer"`
	Source      string           `json:"source"` // fda_enforcement | fda_press | usda_fsis | eu_rasff
	SourceID    string           `json:"source_id"`
	SourceURL   *string          `json:"source_url"`
	PublishedAt *time.Time       `json:"published_at"`
	Normalized  RecallNormalized `json:"normalized"`
	Raw         json.RawMessage  `json:"raw"`
}

// Text returns everything worth parsing for barcodes and lot codes, in one string.
func (r RecallRawReceived) Text() string {
	parts := []string{r.Normalized.Title, deref(r.Normalized.ProductDescription),
		deref(r.Normalized.CodeInfo), deref(r.Normalized.Reason)}
	out := ""
	for _, p := range parts {
		if p != "" {
			out += p + "\n"
		}
	}
	return out
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// InboundEnvelope adapts a delivery to the envelope consumers handle. Messages
// this layer publishes already carry an envelope; messages from the ingestion
// services are flat, so the whole body becomes the payload and the routing key
// becomes the event type.
func InboundEnvelope(routingKey string, body []byte) (Envelope, error) {
	var probe struct {
		Payload    json.RawMessage `json:"payload"`
		EventID    string          `json:"event_id"`
		OccurredAt time.Time       `json:"occurred_at"`
		Producer   string          `json:"producer"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		return Envelope{}, fmt.Errorf("events: undecodable message on %s: %w", routingKey, err)
	}
	if len(probe.Payload) > 0 {
		var env Envelope
		if err := json.Unmarshal(body, &env); err != nil {
			return Envelope{}, fmt.Errorf("events: undecodable envelope on %s: %w", routingKey, err)
		}
		return env, nil
	}
	env := Envelope{
		EventID:      probe.EventID,
		EventType:    routingKey,
		EventVersion: 1,
		OccurredAt:   probe.OccurredAt,
		Producer:     probe.Producer,
		Payload:      body,
	}
	if env.EventID == "" {
		env.EventID = NewID()
	}
	if env.OccurredAt.IsZero() {
		env.OccurredAt = time.Now().UTC()
	}
	if env.Producer == "" {
		env.Producer = "unknown"
	}
	return env, nil
}

type CatalogSKUVanished struct {
	CatalogSource      string    `json:"catalog_source"`
	SKU                string    `json:"sku"`
	UPC                string    `json:"upc,omitempty"`
	ProductDescription string    `json:"product_description,omitempty"`
	BrandName          string    `json:"brand_name,omitempty"`
	LotsLastSeen       []string  `json:"lots_last_seen,omitempty"`
	LastSeenAt         time.Time `json:"last_seen_at,omitempty"`
	ObservedAt         time.Time `json:"observed_at"`
	VanishConfidence   float64   `json:"vanish_confidence"`
}

// ---------------------------------------------------------------- resolution

const (
	ScopeLot = "LOT"
	ScopeSKU = "SKU"
)

type Signal struct {
	Kind        string    `json:"kind"` // AGENCY_NOTICE | SILENT_DIFF
	Source      string    `json:"source"`
	SourceRef   string    `json:"source_ref"`
	SourceURL   string    `json:"source_url,omitempty"`
	PublishedAt time.Time `json:"published_at,omitempty"`
}

type MatchEvidence struct {
	Signal string  `json:"signal"`
	Weight float64 `json:"weight"`
	Detail string  `json:"detail"`
}

type Match struct {
	GTIN         string          `json:"gtin"`
	UPC          string          `json:"upc,omitempty"`
	SKU          string          `json:"sku,omitempty"`
	ProductTitle string          `json:"product_title,omitempty"`
	Brand        string          `json:"brand,omitempty"`
	LotCodes     []string        `json:"lot_codes,omitempty"`
	Confidence   float64         `json:"confidence"`
	Evidence     []MatchEvidence `json:"evidence"`
}

type LotResolved struct {
	IncidentID     string    `json:"incident_id"`
	ResolutionID   string    `json:"resolution_id"`
	ResolvedAt     time.Time `json:"resolved_at"`
	Confidence     float64   `json:"confidence"`
	Scope          string    `json:"scope"`
	Hazard         string    `json:"hazard,omitempty"`
	Allergens      []string  `json:"allergens,omitempty"`
	Classification string    `json:"classification,omitempty"`
	Signal         Signal    `json:"signal"`
	Matches        []Match   `json:"matches"`
}

// ---------------------------------------------------------------- containment

type ContainmentTarget struct {
	GTIN         string   `json:"gtin"`
	SKU          string   `json:"sku,omitempty"`
	ProductTitle string   `json:"product_title,omitempty"`
	Scope        string   `json:"scope"`
	LotCodes     []string `json:"lot_codes,omitempty"`
	Confidence   float64  `json:"confidence,omitempty"`
}

type ContainmentResult struct {
	GTIN              string `json:"gtin"`
	Status            string `json:"status"` // HELD | SKIPPED | FAILED | RELEASED
	Platform          string `json:"platform,omitempty"`
	PlatformRef       string `json:"platform_ref,omitempty"`
	UnitsHeld         int    `json:"units_held,omitempty"`
	UnitsLeftSellable int    `json:"units_left_sellable,omitempty"`
	Error             string `json:"error,omitempty"`
}

type ContainmentActionProposed struct {
	IncidentID string              `json:"incident_id"`
	ActionID   string              `json:"action_id"`
	ProposedAt time.Time           `json:"proposed_at"`
	Confidence float64             `json:"confidence"`
	Threshold  float64             `json:"threshold"`
	Reason     string              `json:"reason"`
	Hazard     string              `json:"hazard,omitempty"`
	Targets    []ContainmentTarget `json:"targets"`
}

const (
	DecisionAutoHold       = "AUTO_HOLD"
	DecisionHumanConfirmed = "HUMAN_CONFIRMED"
	DecisionHumanRejected  = "HUMAN_REJECTED"
	DecisionReleased       = "RELEASED"
)

type ContainmentActionTaken struct {
	IncidentID string              `json:"incident_id"`
	ActionID   string              `json:"action_id"`
	TakenAt    time.Time           `json:"taken_at"`
	Decision   string              `json:"decision"`
	Actor      string              `json:"actor"`
	Confidence float64             `json:"confidence"`
	Threshold  float64             `json:"threshold"`
	Hazard     string              `json:"hazard,omitempty"`
	Targets    []ContainmentTarget `json:"targets"`
	Results    []ContainmentResult `json:"results"`
}

// ---------------------------------------------------------------- order rescue

type Money struct {
	AmountMinor int    `json:"amount_minor"`
	Currency    string `json:"currency"`
}

type Customer struct {
	CustomerID string `json:"customer_id"`
	Email      string `json:"email,omitempty"`
	Phone      string `json:"phone,omitempty"`
	Locale     string `json:"locale,omitempty"`
}

type AffectedLine struct {
	LineItemID   string `json:"line_item_id"`
	GTIN         string `json:"gtin"`
	SKU          string `json:"sku,omitempty"`
	ProductTitle string `json:"product_title,omitempty"`
	LotCode      string `json:"lot_code"`
	Quantity     int    `json:"quantity"`
	UnitPrice    Money  `json:"unit_price"`
}

const (
	OptionSubstitute = "SUBSTITUTE"
	OptionRefund     = "REFUND"
	OptionCancel     = "CANCEL"
)

type RescueOption struct {
	OptionID     string   `json:"option_id"`
	Kind         string   `json:"kind"`
	GTIN         string   `json:"gtin,omitempty"`
	SKU          string   `json:"sku,omitempty"`
	ProductTitle string   `json:"product_title,omitempty"`
	UnitPrice    *Money   `json:"unit_price,omitempty"`
	AllergenSafe bool     `json:"allergen_safe,omitempty"`
	Allergens    []string `json:"allergens,omitempty"`
	Rationale    string   `json:"rationale,omitempty"`
}

type OrderRescueProposed struct {
	IncidentID   string         `json:"incident_id"`
	RescueID     string         `json:"rescue_id"`
	OrderID      string         `json:"order_id"`
	ProposedAt   time.Time      `json:"proposed_at"`
	ExpiresAt    time.Time      `json:"expires_at"`
	Hazard       string         `json:"hazard,omitempty"`
	Customer     Customer       `json:"customer"`
	AffectedLine AffectedLine   `json:"affected_line"`
	Options      []RescueOption `json:"options"`
}

type OrderRescueConfirmed struct {
	IncidentID   string    `json:"incident_id"`
	RescueID     string    `json:"rescue_id"`
	OrderID      string    `json:"order_id"`
	OptionID     string    `json:"option_id"`
	Choice       string    `json:"choice"`
	ConfirmedAt  time.Time `json:"confirmed_at"`
	ConfirmedBy  string    `json:"confirmed_by"` // always CUSTOMER
	ConsentToken string    `json:"consent_token,omitempty"`
}

// ---------------------------------------------------------------- external producers

type EvasionFlagged struct {
	IncidentID   string    `json:"incident_id"`
	FlagID       string    `json:"flag_id"`
	Marketplace  string    `json:"marketplace"`
	ListingURL   string    `json:"listing_url"`
	ListingTitle string    `json:"listing_title,omitempty"`
	Seller       string    `json:"seller,omitempty"`
	GTIN         string    `json:"gtin,omitempty"`
	LotCode      string    `json:"lot_code,omitempty"`
	ObservedAt   time.Time `json:"observed_at"`
	Confidence   float64   `json:"confidence"`
	Evidence     []string  `json:"evidence,omitempty"`
}

type AuditDossierGenerated struct {
	IncidentID     string    `json:"incident_id"`
	DossierID      string    `json:"dossier_id"`
	GeneratedAt    time.Time `json:"generated_at"`
	ContentHash    string    `json:"content_hash"`
	HashAlgorithm  string    `json:"hash_algorithm"`
	TimestampProof string    `json:"timestamp_proof,omitempty"`
	PDFURL         string    `json:"pdf_url,omitempty"`
	EventCount     int       `json:"event_count"`
}
