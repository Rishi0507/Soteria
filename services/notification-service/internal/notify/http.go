package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"soteria/libs/core/events"
	"soteria/libs/feedkit/publish"
)

// Handler serves /healthz, /readyz, /metrics and a read-only ledger lookup
// (/v1/incidents/{id}/deliveries) for the ops console and audit service.
func (s *Service) Handler(broker func() bool) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		st := s.stats.Snapshot()
		counts, _ := s.Ledger.Counts(r.Context())
		ok := broker == nil || broker()
		w.Header().Set("Content-Type", "application/json")
		if !ok {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"healthy":          ok,
			"broker_connected": ok,
			"received":         st.Received,
			"sent":             st.Sent,
			"failed":           st.Failed,
			"retrying":         st.Retrying,
			"last_error":       st.LastError,
			"ledger":           counts,
			"channels":         channelNames(s.Channels),
		})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok\n")) })
	mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, r *http.Request) {
		st := s.stats.Snapshot()
		counts, _ := s.Ledger.Counts(r.Context())
		up := 1
		if broker != nil && !broker() {
			up = 0
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		fmt.Fprintf(w, "# TYPE soteria_notification_up gauge\nsoteria_notification_up %d\n", up)
		fmt.Fprintf(w, "# TYPE soteria_notification_events_received_total counter\nsoteria_notification_events_received_total %d\n", st.Received)
		fmt.Fprintf(w, "# TYPE soteria_notification_sent_total counter\nsoteria_notification_sent_total %d\n", st.Sent)
		fmt.Fprintf(w, "# TYPE soteria_notification_failed_total counter\nsoteria_notification_failed_total %d\n", st.Failed)
		fmt.Fprintf(w, "# TYPE soteria_notification_retrying_total counter\nsoteria_notification_retrying_total %d\n", st.Retrying)
		fmt.Fprintf(w, "# TYPE soteria_notification_ledger_deliveries gauge\n")
		for status, n := range counts {
			fmt.Fprintf(w, "soteria_notification_ledger_deliveries{status=%q} %d\n", status, n)
		}
	})
	mux.HandleFunc("GET /v1/incidents/{id}/deliveries", func(w http.ResponseWriter, r *http.Request) {
		ds, err := s.Ledger.ByIncident(r.Context(), r.PathValue("id"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		type out struct {
			EventID     string     `json:"source_event_id"`
			EventType   string     `json:"source_event_type"`
			Channel     string     `json:"channel"`
			Recipient   string     `json:"recipient"`
			Subject     string     `json:"subject"`
			Status      string     `json:"status"`
			Attempts    int        `json:"attempts"`
			ProviderRef string     `json:"provider_ref,omitempty"`
			LastError   string     `json:"last_error,omitempty"`
			CreatedAt   time.Time  `json:"created_at"`
			SentAt      *time.Time `json:"sent_at"`
		}
		resp := make([]out, 0, len(ds))
		for _, d := range ds {
			resp = append(resp, out{d.EventID, d.EventType, d.Channel, Mask(d.Recipient), d.Subject, d.Status, d.Attempts, d.ProviderRef, d.LastError, d.CreatedAt, d.SentAt})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"incident_id": r.PathValue("id"), "deliveries": resp})
	})
	return mux
}

func channelNames(m map[string]Channel) map[string]string {
	out := map[string]string{}
	for k, c := range m {
		out[k] = c.Name()
	}
	return out
}

// ---- notification.delivered.v1 publishers ------------------------------------

// AMQPOut publishes delivered events straight to notification.x through the
// feedkit publisher (confirms + reconnect). It bypasses core/bus.Publish
// because that routes by a fixed event-type table that does not yet include
// this type — see the PR note for Person 3.
type AMQPOut struct{ Pub *publish.AMQP }

// NewAMQPOut connects to the broker and declares notification.x.
func NewAMQPOut(url string, log *slog.Logger) (*AMQPOut, error) {
	p, err := publish.NewAMQP(url, events.ExchangeNotification, log)
	if err != nil {
		return nil, err
	}
	return &AMQPOut{Pub: p}, nil
}

func (o *AMQPOut) PublishDelivered(ctx context.Context, env events.Envelope) error {
	body, err := json.Marshal(env)
	if err != nil {
		return err
	}
	return o.Pub.Publish(ctx, publish.Message{RoutingKey: env.EventType, MessageID: env.EventID, Type: env.EventType, AppID: env.Producer, Timestamp: env.OccurredAt, Body: body})
}

// Connected reports broker state for /healthz.
func (o *AMQPOut) Connected() bool { return o.Pub.Connected() }

// Close closes the publisher.
func (o *AMQPOut) Close() error { return o.Pub.Close() }

// MemOut collects delivered events in memory (in-process bus / tests).
type MemOut struct{ Events []events.Envelope }

func (m *MemOut) PublishDelivered(_ context.Context, env events.Envelope) error {
	m.Events = append(m.Events, env)
	return nil
}
