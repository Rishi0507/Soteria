// Package rescue detects in-flight orders holding a contained lot and offers the
// customer a same-price, allergen-safe substitute - never applying one until the
// customer has explicitly said yes.
package rescue

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"soteria/libs/core/bus"
	"soteria/libs/core/events"
	"soteria/libs/core/offacts"
	"soteria/services/order-rescue-service/orders"
)

const producer = "order-rescue-service"

// DefaultTTL is how long a customer has to answer before the proposal expires and
// the order falls back to the retailer's normal cancellation path.
const DefaultTTL = 48 * time.Hour

// Rescue statuses.
const (
	StatusProposed  = "PROPOSED"
	StatusConfirmed = "CONFIRMED"
	StatusExpired   = "EXPIRED"
)

// Subscriptions are the queues this service owns (/contracts/rabbitmq-topology.md).
var (
	SubContainment = bus.Subscription{
		Queue:       "rescue.containment-taken",
		Exchange:    events.ExchangeContainment,
		BindingKeys: []string{events.TypeContainmentTaken},
	}
	SubConfirmations = bus.Subscription{
		Queue:       "rescue.confirmations",
		Exchange:    events.ExchangeRescue,
		BindingKeys: []string{events.TypeOrderRescueConfirmed},
	}
)

var (
	ErrNotFound      = errors.New("rescue not found")
	ErrConflict      = errors.New("rescue is no longer open")
	ErrBadConsent    = errors.New("invalid consent token")
	ErrUnknownOption = errors.New("unknown option")
)

// Record is a proposal plus its lifecycle.
type Record struct {
	RescueID       string                `json:"rescue_id"`
	IncidentID     string                `json:"incident_id"`
	OrderID        string                `json:"order_id"`
	Status         string                `json:"status"`
	Hazard         string                `json:"hazard,omitempty"`
	ProposedAt     time.Time             `json:"proposed_at"`
	ExpiresAt      time.Time             `json:"expires_at"`
	ConfirmedAt    *time.Time            `json:"confirmed_at,omitempty"`
	ChosenOptionID string                `json:"chosen_option_id,omitempty"`
	AffectedLine   events.AffectedLine   `json:"affected_line"`
	Options        []events.RescueOption `json:"options"`

	// customer stays out of the API response: PII belongs to the notification
	// path, not to anything the storefront or the dossier renders.
	customer events.Customer
}

// Service is the order-rescue core.
type Service struct {
	orders      orders.Repository
	substitutes SubstituteSource
	allergens   offacts.Provider
	pub         bus.Publisher
	logger      *slog.Logger
	secret      []byte
	ttl         time.Duration

	mu    sync.RWMutex
	byID  map[string]*Record
	order []string
	seen  map[string]bool
	nowFn func() time.Time
}

func New(repo orders.Repository, subs SubstituteSource, allergens offacts.Provider, pub bus.Publisher, secret string, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		orders: repo, substitutes: subs, allergens: allergens, pub: pub,
		logger: logger, secret: []byte(secret), ttl: DefaultTTL,
		byID: map[string]*Record{}, seen: map[string]bool{},
		nowFn: func() time.Time { return time.Now().UTC() },
	}
}

func (s *Service) Register(c bus.Consumer) error {
	if err := c.Subscribe(SubContainment, s.HandleContainment); err != nil {
		return err
	}
	return c.Subscribe(SubConfirmations, s.HandleConfirmation)
}

// HandleContainment reacts to inventory actually being held.
func (s *Service) HandleContainment(ctx context.Context, env events.Envelope) error {
	if env.EventType != events.TypeContainmentTaken {
		return fmt.Errorf("rescue: unexpected event type %q on %s", env.EventType, SubContainment.Queue)
	}
	if s.dedupe(env.EventID) {
		return nil
	}
	var in events.ContainmentActionTaken
	if err := env.Into(&in); err != nil {
		return fmt.Errorf("rescue: decode containment.action.taken.v1: %w", err)
	}
	// A rejection or a failed hold means nothing was contained, so there is
	// nothing to rescue customers from.
	if in.Decision != events.DecisionAutoHold && in.Decision != events.DecisionHumanConfirmed {
		return nil
	}
	held := map[string]bool{}
	for _, r := range in.Results {
		if r.Status == "HELD" {
			held[r.GTIN] = true
		}
	}

	hazardAllergens := offacts.HazardAllergens(in.Hazard)
	for _, t := range in.Targets {
		if !held[t.GTIN] {
			continue
		}
		affected, err := s.orders.AffectedLines(ctx, t.GTIN, t.LotCodes)
		if err != nil {
			return fmt.Errorf("rescue: scan orders for %s: %w", t.GTIN, err)
		}
		for _, a := range affected {
			if err := s.propose(ctx, env, in, a, hazardAllergens); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) propose(ctx context.Context, env events.Envelope, in events.ContainmentActionTaken, a orders.Affected, hazardAllergens []string) error {
	if s.existsFor(in.IncidentID, a.Order.OrderID, a.Line.LineItemID) {
		return nil
	}
	now := s.nowFn()
	rec := &Record{
		RescueID:   events.NewID(),
		IncidentID: in.IncidentID,
		OrderID:    a.Order.OrderID,
		Status:     StatusProposed,
		Hazard:     in.Hazard,
		ProposedAt: now,
		ExpiresAt:  now.Add(s.ttl),
		AffectedLine: events.AffectedLine{
			LineItemID:   a.Line.LineItemID,
			GTIN:         a.Line.GTIN,
			SKU:          a.Line.SKU,
			ProductTitle: a.Line.ProductTitle,
			LotCode:      a.Line.LotCode,
			Quantity:     a.Line.Quantity,
			UnitPrice:    a.Line.UnitPrice,
		},
		customer: a.Order.Customer,
	}
	options, err := s.buildOptions(ctx, a.Line, hazardAllergens)
	if err != nil {
		return err
	}
	rec.Options = options
	s.put(rec)

	payload := events.OrderRescueProposed{
		IncidentID:   rec.IncidentID,
		RescueID:     rec.RescueID,
		OrderID:      rec.OrderID,
		ProposedAt:   rec.ProposedAt,
		ExpiresAt:    rec.ExpiresAt,
		Hazard:       rec.Hazard,
		Customer:     rec.customer,
		AffectedLine: rec.AffectedLine,
		Options:      rec.Options,
	}
	outEnv, err := events.NewEnvelope(events.TypeOrderRescueProposed, producer, rec.IncidentID, payload)
	if err != nil {
		return err
	}
	outEnv.CausationID = env.EventID
	if err := s.pub.Publish(ctx, outEnv); err != nil {
		return fmt.Errorf("rescue: publish order.rescue.proposed.v1: %w", err)
	}
	s.logger.Info("order rescue proposed",
		"incident_id", rec.IncidentID, "order_id", rec.OrderID, "rescue_id", rec.RescueID,
		"substitutes", countSubstitutes(rec.Options))
	return nil
}

// buildOptions offers only substitutes that are the same price and allergen-safe,
// always alongside a refund and a cancel option so the customer keeps control.
func (s *Service) buildOptions(ctx context.Context, line orders.LineItem, hazardAllergens []string) ([]events.RescueOption, error) {
	original, err := s.allergens.Lookup(ctx, line.GTIN)
	if err != nil {
		// Enrichment being down must not block containment follow-up; we simply
		// cannot vouch for any substitute, so we offer refund/cancel only.
		s.logger.Warn("allergen lookup failed for the recalled product", "gtin", line.GTIN, "err", err)
		original = offacts.Product{GTIN: line.GTIN, Coverage: offacts.CoverageAbsent}
	}
	candidates, err := s.substitutes.Alternatives(ctx, line.GTIN)
	if err != nil {
		return nil, fmt.Errorf("rescue: substitute lookup for %s: %w", line.GTIN, err)
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].InStock > candidates[j].InStock })

	var options []events.RescueOption
	for _, c := range candidates {
		if c.UnitPrice != line.UnitPrice {
			continue // same-price only: a rescue must never quietly cost more
		}
		sub, err := s.allergens.Lookup(ctx, c.GTIN)
		if err != nil {
			s.logger.Warn("allergen lookup failed for substitute", "gtin", c.GTIN, "err", err)
			continue
		}
		safe, why := allergenSafe(original, sub, hazardAllergens)
		if !safe {
			s.logger.Info("substitute rejected on allergen grounds", "gtin", c.GTIN, "reason", why)
			continue
		}
		price := c.UnitPrice
		options = append(options, events.RescueOption{
			OptionID:     "sub-" + c.GTIN,
			Kind:         events.OptionSubstitute,
			GTIN:         c.GTIN,
			SKU:          c.SKU,
			ProductTitle: c.ProductTitle,
			UnitPrice:    &price,
			AllergenSafe: true,
			Allergens:    sortedKeys(sub.AllergenSet()),
			Rationale:    why,
		})
		if len(options) == 3 {
			break
		}
	}
	options = append(options,
		events.RescueOption{OptionID: "refund", Kind: events.OptionRefund, Rationale: "Refund this item and keep the rest of the order."},
		events.RescueOption{OptionID: "cancel", Kind: events.OptionCancel, Rationale: "Cancel the whole order."},
	)
	return options, nil
}

// allergenSafe is deliberately conservative: anything short of a complete
// allergen record is treated as unsafe, and a substitute may not introduce any
// allergen the original did not have.
//
// The coverage check is what stops the dangerous case: Open Food Facts omits
// allergen tags entirely for products nobody has annotated, so an untagged
// product and a genuinely allergen-free product look identical in the data. Only
// COMPLETE coverage with an empty allergen list means "allergen-free"; PARTIAL
// and ABSENT mean "we do not know", and we never offer a substitute on a guess.
func allergenSafe(original, sub offacts.Product, hazardAllergens []string) (bool, string) {
	if !sub.Complete() {
		return false, fmt.Sprintf("allergen data for the substitute is %s, not COMPLETE", coverageOf(sub))
	}
	subSet := sub.AllergenSet()
	for _, h := range hazardAllergens {
		if subSet[h] {
			return false, fmt.Sprintf("substitute contains the recall hazard allergen %q", h)
		}
	}
	if original.Complete() {
		origSet := original.AllergenSet()
		for a := range subSet {
			if !origSet[a] {
				return false, fmt.Sprintf("substitute introduces a new allergen %q", a)
			}
		}
	} else if len(subSet) > 0 {
		return false, fmt.Sprintf("allergen data for the recalled product is %s, so any allergen in the substitute is unverifiable", coverageOf(original))
	}
	return true, "same price, complete allergen data, no recall hazard allergen and no allergen the original did not already carry"
}

func coverageOf(p offacts.Product) string {
	if p.Coverage == "" {
		return offacts.CoverageAbsent
	}
	return p.Coverage
}

// HandleConfirmation applies the customer's explicit choice.
func (s *Service) HandleConfirmation(ctx context.Context, env events.Envelope) error {
	if env.EventType != events.TypeOrderRescueConfirmed {
		return fmt.Errorf("rescue: unexpected event type %q on %s", env.EventType, SubConfirmations.Queue)
	}
	if s.dedupe(env.EventID) {
		return nil
	}
	var in events.OrderRescueConfirmed
	if err := env.Into(&in); err != nil {
		return fmt.Errorf("rescue: decode order.rescue.confirmed.v1: %w", err)
	}
	rec, ok := s.get(in.RescueID)
	if !ok {
		return fmt.Errorf("rescue: confirmation for unknown rescue %s", in.RescueID)
	}
	if rec.Status != StatusProposed {
		s.logger.Info("confirmation for a closed rescue ignored", "rescue_id", rec.RescueID, "status", rec.Status)
		return nil
	}
	opt, ok := optionByID(rec.Options, in.OptionID)
	if !ok {
		return fmt.Errorf("rescue: %w %q on rescue %s", ErrUnknownOption, in.OptionID, rec.RescueID)
	}

	switch opt.Kind {
	case events.OptionSubstitute:
		if err := s.orders.ApplySwap(ctx, rec.OrderID, rec.AffectedLine.LineItemID, opt.GTIN); err != nil {
			return fmt.Errorf("rescue: apply swap: %w", err)
		}
	case events.OptionCancel:
		if err := s.orders.Cancel(ctx, rec.OrderID); err != nil {
			if !errors.Is(err, orders.ErrUnsupported) {
				return fmt.Errorf("rescue: cancel order: %w", err)
			}
			// The platform client cannot cancel yet. Retrying forever would only
			// dead-letter the customer's decision, so we record it and say plainly
			// that a human has to finish it.
			s.logger.Warn("cancellation recorded but not executed on the platform",
				"rescue_id", rec.RescueID, "order_id", rec.OrderID, "err", err)
		}
	case events.OptionRefund:
		// Refund execution is the retailer's payment flow (out of scope for v1);
		// the confirmed event is the instruction and the audit record.
	}

	now := s.nowFn()
	s.mu.Lock()
	rec.Status = StatusConfirmed
	rec.ConfirmedAt = &now
	rec.ChosenOptionID = opt.OptionID
	s.mu.Unlock()
	s.logger.Info("order rescue confirmed",
		"rescue_id", rec.RescueID, "order_id", rec.OrderID, "choice", opt.Kind)
	return nil
}

// Confirm is the storefront-facing path. It validates consent and publishes
// order.rescue.confirmed.v1; the swap itself happens in HandleConfirmation, so
// there is exactly one writer whether the confirmation arrives by API or by event.
func (s *Service) Confirm(ctx context.Context, rescueID, optionID, consentToken string) (Record, error) {
	rec, ok := s.get(rescueID)
	if !ok {
		return Record{}, ErrNotFound
	}
	if !hmac.Equal([]byte(consentToken), []byte(s.ConsentToken(rescueID))) {
		return Record{}, ErrBadConsent
	}
	if rec.Status != StatusProposed {
		return Record{}, ErrConflict
	}
	if s.nowFn().After(rec.ExpiresAt) {
		s.mu.Lock()
		rec.Status = StatusExpired
		s.mu.Unlock()
		return Record{}, ErrConflict
	}
	opt, ok := optionByID(rec.Options, optionID)
	if !ok {
		return Record{}, ErrUnknownOption
	}

	payload := events.OrderRescueConfirmed{
		IncidentID:   rec.IncidentID,
		RescueID:     rec.RescueID,
		OrderID:      rec.OrderID,
		OptionID:     opt.OptionID,
		Choice:       opt.Kind,
		ConfirmedAt:  s.nowFn(),
		ConfirmedBy:  "CUSTOMER",
		ConsentToken: consentToken,
	}
	env, err := events.NewEnvelope(events.TypeOrderRescueConfirmed, producer, rec.IncidentID, payload)
	if err != nil {
		return Record{}, err
	}
	if err := s.pub.Publish(ctx, env); err != nil {
		return Record{}, fmt.Errorf("rescue: publish order.rescue.confirmed.v1: %w", err)
	}
	out, _ := s.get(rescueID)
	return *out, nil
}

// ConsentToken is the proof-of-click issued with a proposal and recorded in the
// audit dossier. It is derived, not stored, so a leaked store reveals nothing.
func (s *Service) ConsentToken(rescueID string) string {
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(rescueID))
	return hex.EncodeToString(mac.Sum(nil))
}

// --------------------------------------------------------------------- store

func (s *Service) put(r *Record) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byID[r.RescueID] = r
	s.order = append(s.order, r.RescueID)
}

func (s *Service) get(id string) (*Record, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.byID[id]
	return r, ok
}

// Get returns a copy for the API layer.
func (s *Service) Get(id string) (Record, bool) {
	r, ok := s.get(id)
	if !ok {
		return Record{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return *r, true
}

// List filters rescues, newest first.
func (s *Service) List(orderID, incidentID, status string) []Record {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Record
	for _, id := range s.order {
		r := s.byID[id]
		if orderID != "" && r.OrderID != orderID {
			continue
		}
		if incidentID != "" && r.IncidentID != incidentID {
			continue
		}
		if status != "" && r.Status != status {
			continue
		}
		out = append(out, *r)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ProposedAt.After(out[j].ProposedAt) })
	return out
}

func (s *Service) existsFor(incidentID, orderID, lineItemID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, id := range s.order {
		r := s.byID[id]
		if r.IncidentID == incidentID && r.OrderID == orderID && r.AffectedLine.LineItemID == lineItemID {
			return true
		}
	}
	return false
}

func (s *Service) dedupe(eventID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.seen[eventID] {
		return true
	}
	s.seen[eventID] = true
	return false
}

// --------------------------------------------------------------------- helpers

func optionByID(options []events.RescueOption, id string) (events.RescueOption, bool) {
	for _, o := range options {
		if o.OptionID == id {
			return o, true
		}
	}
	return events.RescueOption{}, false
}

func countSubstitutes(options []events.RescueOption) int {
	n := 0
	for _, o := range options {
		if o.Kind == events.OptionSubstitute {
			n++
		}
	}
	return n
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Customer exposes the PII-bearing contact block to the notification path only.
func (r Record) Customer() events.Customer { return r.customer }

// Redact strips anything that must not reach an audit dossier in cleartext.
func (r Record) Redact() Record {
	r.customer = events.Customer{CustomerID: r.customer.CustomerID, Locale: r.customer.Locale}
	return r
}
