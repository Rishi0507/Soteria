package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"soteria/libs/core/bus"
	"soteria/libs/core/events"
)

// TypeDelivered is the event this service produces (contracts/events/
// notification.delivered.v1.json) on exchange notification.x. It is the
// audit-grade proof that a party was told.
const (
	TypeDelivered = "notification.delivered.v1"
	Producer      = "notification-service"
)

// Delivered is the payload of notification.delivered.v1.
type Delivered struct {
	IncidentID    string    `json:"incident_id"`
	SourceEventID string    `json:"source_event_id"`
	SourceType    string    `json:"source_event_type"`
	Channel       string    `json:"channel"`
	Recipient     string    `json:"recipient"`
	Status        string    `json:"status"` // SENT | FAILED
	Provider      string    `json:"provider,omitempty"`
	ProviderRef   string    `json:"provider_ref,omitempty"`
	Attempts      int       `json:"attempts"`
	SentAt        time.Time `json:"sent_at"`
	Error         string    `json:"error,omitempty"`
}

// DeliveredPublisher emits Delivered records; nil disables emission.
type DeliveredPublisher interface {
	PublishDelivered(ctx context.Context, env events.Envelope) error
}

// Service is the bus handler.
type Service struct {
	Router   *Router
	Ledger   *Ledger
	Channels map[string]Channel // by channel name
	Out      DeliveredPublisher
	Log      *slog.Logger

	// SendRetries is how many in-process attempts per message before handing
	// the transient failure back to the bus (default 3).
	SendRetries int
	// Backoff between in-process attempts (default 500ms, doubling).
	Backoff time.Duration
	Sleep   func(context.Context, time.Duration) error

	stats Stats
}

// Stats are cheap counters for /healthz and /metrics.
type Stats struct {
	mu sync.Mutex
	StatsSnapshot
}

// StatsSnapshot is a lock-free copy of the counters.
type StatsSnapshot struct {
	Received  int64
	Sent      int64
	Failed    int64
	Retrying  int64
	LastError string
	LastAt    time.Time
}

// Snapshot returns a copy of the counters.
func (s *Stats) Snapshot() StatsSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.StatsSnapshot
}

// New wires a service with defaults.
func New(router *Router, ledger *Ledger, channels map[string]Channel, out DeliveredPublisher, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{Router: router, Ledger: ledger, Channels: channels, Out: out, Log: log, SendRetries: 3, Backoff: 500 * time.Millisecond, Sleep: sleep}
}

// Stats exposes the counters.
func (s *Service) Stats() *Stats { return &s.stats }

// Subscriptions returns the queue bindings from /contracts/rabbitmq-topology.md.
// One queue, three exchanges: the bus API takes one exchange per call, so
// Subscribe is invoked three times with the same queue name.
func Subscriptions() []bus.Subscription {
	return []bus.Subscription{
		{Queue: "notification.outbound", Exchange: events.ExchangeContainment, BindingKeys: []string{events.TypeContainmentTaken}},
		{Queue: "notification.outbound", Exchange: events.ExchangeRescue, BindingKeys: []string{events.TypeOrderRescueProposed}},
		{Queue: "notification.outbound", Exchange: events.ExchangeEvasion, BindingKeys: []string{events.TypeEvasionFlagged}},
	}
}

// Handle is the bus.Handler. Returning an error asks the bus to redeliver
// (and eventually dead-letter); it is returned only for transient delivery
// failures. Permanent failures are recorded and acked.
func (s *Service) Handle(ctx context.Context, env events.Envelope) error {
	s.stats.mu.Lock()
	s.stats.Received++
	s.stats.mu.Unlock()

	msgs, err := s.Router.Route(env)
	if err != nil {
		// Undecodable payload: nothing we can do; record and ack.
		s.Log.Error("event rejected", "event_type", env.EventType, "event_id", env.EventID, "err", err)
		s.note(err)
		return nil
	}
	incident := IncidentID(env)
	var transient []error
	for _, m := range msgs {
		if err := s.deliver(ctx, env, incident, m); err != nil {
			transient = append(transient, err)
		}
	}
	if len(transient) > 0 {
		return fmt.Errorf("notification: %d message(s) deferred to bus retry: %w", len(transient), errors.Join(transient...))
	}
	return nil
}

func (s *Service) deliver(ctx context.Context, env events.Envelope, incident string, m Message) error {
	rec, alreadySent, err := s.Ledger.Begin(ctx, Delivery{
		EventID: env.EventID, EventType: env.EventType, IncidentID: incident,
		Channel: m.Channel, Recipient: m.Recipient, Subject: m.Subject,
	})
	if err != nil {
		return fmt.Errorf("ledger: %w", err)
	}
	if alreadySent {
		s.Log.Info("redelivery of an already-sent notification skipped", "event_id", env.EventID, "channel", m.Channel)
		return nil
	}
	ch, ok := s.Channels[m.Channel]
	if !ok {
		err := Permanent(fmt.Errorf("no provider configured for channel %q", m.Channel))
		_ = s.Ledger.MarkFailed(ctx, rec.ID, err)
		s.fail(env, incident, m, rec.Attempts, err)
		return nil
	}

	var last error
	for attempt := 1; attempt <= max(1, s.SendRetries); attempt++ {
		if attempt > 1 {
			if err := s.Sleep(ctx, s.Backoff<<(attempt-2)); err != nil {
				return err
			}
		}
		_ = s.Ledger.Attempt(ctx, rec.ID)
		ref, err := ch.Send(ctx, m)
		if err == nil {
			now := time.Now().UTC()
			_ = s.Ledger.MarkSent(ctx, rec.ID, ref, now)
			s.stats.mu.Lock()
			s.stats.Sent++
			s.stats.LastAt = now
			s.stats.mu.Unlock()
			s.Log.Info("notification sent", "channel", m.Channel, "recipient", Mask(m.Recipient), "incident_id", incident, "provider_ref", ref, "attempt", attempt)
			s.emit(ctx, env, Delivered{IncidentID: incident, SourceEventID: env.EventID, SourceType: env.EventType, Channel: m.Channel,
				Recipient: m.Recipient, Status: StatusSent, Provider: ch.Name(), ProviderRef: ref, Attempts: rec.Attempts + attempt, SentAt: now})
			return nil
		}
		last = err
		if IsPermanent(err) {
			_ = s.Ledger.MarkFailed(ctx, rec.ID, err)
			s.fail(env, incident, m, rec.Attempts+attempt, err)
			return nil
		}
		s.Log.Warn("notification attempt failed", "channel", m.Channel, "attempt", attempt, "err", err)
	}
	_ = s.Ledger.MarkRetrying(ctx, rec.ID, last)
	s.stats.mu.Lock()
	s.stats.Retrying++
	s.stats.mu.Unlock()
	s.note(last)
	return fmt.Errorf("%s to %s: %w", m.Channel, Mask(m.Recipient), last)
}

func (s *Service) fail(env events.Envelope, incident string, m Message, attempts int, err error) {
	s.stats.mu.Lock()
	s.stats.Failed++
	s.stats.mu.Unlock()
	s.note(err)
	s.Log.Error("notification permanently failed", "channel", m.Channel, "recipient", Mask(m.Recipient), "incident_id", incident, "err", err)
	s.emit(context.Background(), env, Delivered{IncidentID: incident, SourceEventID: env.EventID, SourceType: env.EventType, Channel: m.Channel,
		Recipient: m.Recipient, Status: StatusFailed, Attempts: attempts, SentAt: time.Now().UTC(), Error: err.Error()})
}

func (s *Service) emit(ctx context.Context, cause events.Envelope, d Delivered) {
	if s.Out == nil {
		return
	}
	body, err := json.Marshal(d)
	if err != nil {
		return
	}
	env := events.Envelope{
		EventID: events.NewID(), EventType: TypeDelivered, EventVersion: 1, OccurredAt: time.Now().UTC(),
		Producer: Producer, CorrelationID: d.IncidentID, CausationID: cause.EventID, Payload: body,
	}
	if err := s.Out.PublishDelivered(ctx, env); err != nil {
		// The ledger row is the source of truth; losing the event is logged, not fatal.
		s.Log.Warn("could not publish notification.delivered.v1", "err", err)
	}
}

func (s *Service) note(err error) {
	s.stats.mu.Lock()
	s.stats.LastError = err.Error()
	s.stats.LastAt = time.Now().UTC()
	s.stats.mu.Unlock()
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
