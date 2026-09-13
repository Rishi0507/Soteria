// Package containment decides whether a resolved recall is held automatically or
// queued for a human, and executes the inventory write.
package containment

import (
	"sort"
	"sync"
	"time"

	"soteria/libs/core/events"
)

// Action statuses (see /contracts/openapi/containment-api.v1.yaml).
const (
	StatusPendingReview = "PENDING_REVIEW"
	StatusAutoHeld      = "AUTO_HELD"
	StatusConfirmed     = "HUMAN_CONFIRMED"
	StatusRejected      = "HUMAN_REJECTED"
	StatusFailed        = "FAILED"
)

// Action is one containment decision and its outcome.
type Action struct {
	ActionID   string                     `json:"action_id"`
	IncidentID string                     `json:"incident_id"`
	Status     string                     `json:"status"`
	Confidence float64                    `json:"confidence"`
	Threshold  float64                    `json:"threshold"`
	Hazard     string                     `json:"hazard,omitempty"`
	Reason     string                     `json:"reason,omitempty"`
	Actor      string                     `json:"actor,omitempty"`
	Note       string                     `json:"note,omitempty"`
	CreatedAt  time.Time                  `json:"created_at"`
	DecidedAt  *time.Time                 `json:"decided_at,omitempty"`
	Targets    []events.ContainmentTarget `json:"targets"`
	Results    []events.ContainmentResult `json:"results,omitempty"`
}

// Config is the threshold surface the ops console tunes without a redeploy.
type Config struct {
	// AutoHoldThreshold: at or above this confidence a LOT-scope containment is
	// executed without a human.
	AutoHoldThreshold float64 `json:"auto_hold_threshold"`
	// SKUScopeThreshold: holding a whole SKU burns inventory that may be clean, so
	// it carries a higher bar than a lot-level hold.
	SKUScopeThreshold float64   `json:"sku_scope_threshold"`
	UpdatedBy         string    `json:"updated_by,omitempty"`
	UpdatedAt         time.Time `json:"updated_at"`
}

func DefaultConfig() Config {
	return Config{AutoHoldThreshold: 0.85, SKUScopeThreshold: 0.95, UpdatedBy: "default", UpdatedAt: time.Now().UTC()}
}

// Store keeps actions and the live config.
type Store struct {
	mu      sync.RWMutex
	actions map[string]*Action
	order   []string
	config  Config
}

func NewStore(cfg Config) *Store {
	return &Store{actions: map[string]*Action{}, config: cfg}
}

func (s *Store) Put(a Action) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.actions[a.ActionID]; !exists {
		s.order = append(s.order, a.ActionID)
	}
	copied := a
	s.actions[a.ActionID] = &copied
}

func (s *Store) Get(actionID string) (Action, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, ok := s.actions[actionID]
	if !ok {
		return Action{}, false
	}
	return *a, true
}

// ByIncident finds an existing action for an incident, making redelivery of the
// same lot.resolved.v1 idempotent.
func (s *Store) ByIncident(incidentID string) (Action, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, id := range s.order {
		if a := s.actions[id]; a.IncidentID == incidentID {
			return *a, true
		}
	}
	return Action{}, false
}

func (s *Store) List(status, incidentID string, limit int) []Action {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Action
	for _, id := range s.order {
		a := s.actions[id]
		if status != "" && a.Status != status {
			continue
		}
		if incidentID != "" && a.IncidentID != incidentID {
			continue
		}
		out = append(out, *a)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (s *Store) Config() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config
}

func (s *Store) SetConfig(cfg Config) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config = cfg
}
