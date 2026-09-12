package containment

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"soteria/libs/core/bus"
	"soteria/libs/core/events"
	"soteria/libs/core/shopify"
)

const producer = "containment-service"

// Subscription is the queue this service owns (/contracts/rabbitmq-topology.md).
var Subscription = bus.Subscription{
	Queue:       "containment.lot-resolved",
	Exchange:    events.ExchangeResolution,
	BindingKeys: []string{events.TypeLotResolved},
}

// ErrConflict is returned when an action cannot make the requested transition.
var ErrConflict = fmt.Errorf("action is not awaiting review")

// ErrNotFound is returned for an unknown action id.
var ErrNotFound = fmt.Errorf("action not found")

// Service applies the threshold policy and performs the commerce-platform write.
type Service struct {
	store     *Store
	inventory shopify.InventoryClient
	pub       bus.Publisher
	logger    *slog.Logger

	mu   sync.Mutex
	seen map[string]bool
}

func New(store *Store, inv shopify.InventoryClient, pub bus.Publisher, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{store: store, inventory: inv, pub: pub, logger: logger, seen: map[string]bool{}}
}

func (s *Service) Register(c bus.Consumer) error {
	return c.Subscribe(Subscription, s.Handle)
}

// Handle consumes lot.resolved.v1.
func (s *Service) Handle(ctx context.Context, env events.Envelope) error {
	if env.EventType != events.TypeLotResolved {
		return fmt.Errorf("containment: unexpected event type %q on %s", env.EventType, Subscription.Queue)
	}
	s.mu.Lock()
	if s.seen[env.EventID] {
		s.mu.Unlock()
		return nil
	}
	s.seen[env.EventID] = true
	s.mu.Unlock()

	var in events.LotResolved
	if err := env.Into(&in); err != nil {
		return fmt.Errorf("containment: decode lot.resolved.v1: %w", err)
	}
	if len(in.Matches) == 0 {
		return fmt.Errorf("containment: lot.resolved.v1 for %s has no matches", in.IncidentID)
	}
	if _, exists := s.store.ByIncident(in.IncidentID); exists {
		s.logger.Info("incident already has a containment action", "incident_id", in.IncidentID)
		return nil
	}

	cfg := s.store.Config()
	targets := targetsFrom(in)
	threshold := cfg.AutoHoldThreshold
	if in.Scope == events.ScopeSKU {
		threshold = cfg.SKUScopeThreshold
	}

	action := Action{
		ActionID:   events.NewID(),
		IncidentID: in.IncidentID,
		Confidence: in.Confidence,
		Threshold:  threshold,
		Hazard:     in.Hazard,
		CreatedAt:  time.Now().UTC(),
		Targets:    targets,
	}

	if in.Confidence < threshold {
		action.Status = StatusPendingReview
		action.Reason = fmt.Sprintf("confidence %.2f below %s threshold %.2f", in.Confidence, strings.ToLower(in.Scope), threshold)
		s.store.Put(action)
		proposed := events.ContainmentActionProposed{
			IncidentID: action.IncidentID,
			ActionID:   action.ActionID,
			ProposedAt: action.CreatedAt,
			Confidence: action.Confidence,
			Threshold:  action.Threshold,
			Reason:     action.Reason,
			Hazard:     action.Hazard,
			Targets:    action.Targets,
		}
		if err := s.publish(ctx, events.TypeContainmentProposed, action.IncidentID, env.EventID, proposed); err != nil {
			return err
		}
		s.logger.Info("containment queued for human review",
			"incident_id", action.IncidentID, "action_id", action.ActionID, "confidence", action.Confidence)
		return nil
	}

	s.store.Put(action)
	return s.execute(ctx, action.ActionID, events.DecisionAutoHold, "system", env.EventID)
}

// Confirm is the ops-console approval path for a below-threshold action.
// A reviewer may narrow the held lots but never widen them.
func (s *Service) Confirm(ctx context.Context, actionID, actor, note string, lotOverride []string) (Action, error) {
	action, ok := s.store.Get(actionID)
	if !ok {
		return Action{}, ErrNotFound
	}
	if action.Status != StatusPendingReview {
		return Action{}, ErrConflict
	}
	if len(lotOverride) > 0 {
		action.Targets = narrowLots(action.Targets, lotOverride)
	}
	action.Actor, action.Note = actor, note
	s.store.Put(action)
	if err := s.execute(ctx, actionID, events.DecisionHumanConfirmed, actor, ""); err != nil {
		return Action{}, err
	}
	out, _ := s.store.Get(actionID)
	return out, nil
}

// Reject records a human decision not to contain. It is still an audited event:
// "we looked and chose not to act" is exactly what an insurer will ask about.
func (s *Service) Reject(ctx context.Context, actionID, actor, reason string) (Action, error) {
	action, ok := s.store.Get(actionID)
	if !ok {
		return Action{}, ErrNotFound
	}
	if action.Status != StatusPendingReview {
		return Action{}, ErrConflict
	}
	now := time.Now().UTC()
	action.Status = StatusRejected
	action.Actor, action.Note, action.DecidedAt = actor, reason, &now
	s.store.Put(action)

	taken := events.ContainmentActionTaken{
		IncidentID: action.IncidentID,
		ActionID:   action.ActionID,
		TakenAt:    now,
		Decision:   events.DecisionHumanRejected,
		Actor:      actor,
		Confidence: action.Confidence,
		Threshold:  action.Threshold,
		Hazard:     action.Hazard,
		Targets:    action.Targets,
		Results:    []events.ContainmentResult{},
	}
	if err := s.publish(ctx, events.TypeContainmentTaken, action.IncidentID, "", taken); err != nil {
		return Action{}, err
	}
	return action, nil
}

// execute performs the inventory write for every target and publishes
// containment.action.taken.v1 with per-target results.
func (s *Service) execute(ctx context.Context, actionID, decision, actor, causationID string) error {
	action, ok := s.store.Get(actionID)
	if !ok {
		return ErrNotFound
	}
	results := make([]events.ContainmentResult, 0, len(action.Targets))
	anyFailed, anyHeld := false, false
	for _, t := range action.Targets {
		resp, err := s.inventory.HoldLots(ctx, shopify.HoldRequest{
			GTIN:     t.GTIN,
			SKU:      t.SKU,
			LotCodes: t.LotCodes,
			Reason:   fmt.Sprintf("Soteria incident %s: %s", action.IncidentID, action.Hazard),
		})
		if err != nil {
			anyFailed = true
			results = append(results, events.ContainmentResult{GTIN: t.GTIN, Status: "FAILED", Platform: "shopify", Error: err.Error()})
			s.logger.Error("inventory hold failed", "incident_id", action.IncidentID, "gtin", t.GTIN, "err", err)
			continue
		}
		anyHeld = true
		results = append(results, events.ContainmentResult{
			GTIN:              t.GTIN,
			Status:            "HELD",
			Platform:          "shopify",
			PlatformRef:       resp.PlatformRef,
			UnitsHeld:         resp.UnitsHeld,
			UnitsLeftSellable: resp.UnitsLeftSellable,
		})
	}

	now := time.Now().UTC()
	action.Results = results
	action.DecidedAt = &now
	action.Actor = actor
	switch {
	case anyFailed && !anyHeld:
		action.Status = StatusFailed
	case decision == events.DecisionAutoHold:
		action.Status = StatusAutoHeld
	default:
		action.Status = StatusConfirmed
	}
	s.store.Put(action)

	taken := events.ContainmentActionTaken{
		IncidentID: action.IncidentID,
		ActionID:   action.ActionID,
		TakenAt:    now,
		Decision:   decision,
		Actor:      actor,
		Confidence: action.Confidence,
		Threshold:  action.Threshold,
		Hazard:     action.Hazard,
		Targets:    action.Targets,
		Results:    results,
	}
	if err := s.publish(ctx, events.TypeContainmentTaken, action.IncidentID, causationID, taken); err != nil {
		return err
	}
	s.logger.Info("containment executed",
		"incident_id", action.IncidentID, "action_id", action.ActionID,
		"decision", decision, "status", action.Status, "targets", len(action.Targets))
	if anyFailed && anyHeld {
		// Partial failure is surfaced in the results and to the operator, but the
		// successful holds stand: nacking would re-hold what is already held.
		s.logger.Warn("containment partially failed", "incident_id", action.IncidentID)
	}
	return nil
}

// UpdateConfig changes the thresholds live.
func (s *Service) UpdateConfig(cfg Config) Config {
	cfg.UpdatedAt = time.Now().UTC()
	if cfg.SKUScopeThreshold == 0 {
		cfg.SKUScopeThreshold = s.store.Config().SKUScopeThreshold
	}
	s.store.SetConfig(cfg)
	s.logger.Info("containment thresholds updated",
		"auto_hold_threshold", cfg.AutoHoldThreshold, "sku_scope_threshold", cfg.SKUScopeThreshold, "actor", cfg.UpdatedBy)
	return cfg
}

func (s *Service) Store() *Store { return s.store }

func (s *Service) publish(ctx context.Context, eventType, incidentID, causationID string, payload any) error {
	env, err := events.NewEnvelope(eventType, producer, incidentID, payload)
	if err != nil {
		return err
	}
	env.CausationID = causationID
	if err := s.pub.Publish(ctx, env); err != nil {
		return fmt.Errorf("containment: publish %s: %w", eventType, err)
	}
	return nil
}

func targetsFrom(in events.LotResolved) []events.ContainmentTarget {
	out := make([]events.ContainmentTarget, 0, len(in.Matches))
	for _, m := range in.Matches {
		scope := events.ScopeLot
		if len(m.LotCodes) == 0 {
			scope = events.ScopeSKU
		}
		out = append(out, events.ContainmentTarget{
			GTIN:         m.GTIN,
			SKU:          m.SKU,
			ProductTitle: m.ProductTitle,
			Scope:        scope,
			LotCodes:     m.LotCodes,
			Confidence:   m.Confidence,
		})
	}
	return out
}

// narrowLots intersects each target's lots with the reviewer's list. Lots the
// reviewer names that were not already in scope are ignored.
func narrowLots(targets []events.ContainmentTarget, allowed []string) []events.ContainmentTarget {
	allow := map[string]bool{}
	for _, l := range allowed {
		allow[strings.ToUpper(strings.TrimSpace(l))] = true
	}
	out := make([]events.ContainmentTarget, 0, len(targets))
	for _, t := range targets {
		var kept []string
		for _, l := range t.LotCodes {
			if allow[strings.ToUpper(strings.TrimSpace(l))] {
				kept = append(kept, l)
			}
		}
		if len(kept) > 0 {
			t.LotCodes = kept
			t.Scope = events.ScopeLot
		}
		out = append(out, t)
	}
	return out
}
