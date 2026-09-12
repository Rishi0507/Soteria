// Package health tracks per-source poll outcomes and serves /healthz,
// /readyz and a Prometheus-text /metrics endpoint. A feed outage surfaces as
// a 503 from /healthz, which is what the n8n health-check workflow pings.
package health

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"
)

// StaleFactor multiplies a source's poll interval to decide staleness:
// no success within StaleFactor×interval → unhealthy.
const StaleFactor = 3

// MaxConsecutiveFailures marks a source unhealthy once reached.
const MaxConsecutiveFailures = 3

// SourceState is the observable state of one feed.
type SourceState struct {
	Interval            time.Duration `json:"-"`
	IntervalSeconds     float64       `json:"interval_seconds"`
	LastSuccessAt       *time.Time    `json:"last_success_at"`
	LastAttemptAt       *time.Time    `json:"last_attempt_at"`
	LastError           string        `json:"last_error,omitempty"`
	ConsecutiveFailures int           `json:"consecutive_failures"`
	FetchTotal          int64         `json:"fetch_total"`
	FetchFailuresTotal  int64         `json:"fetch_failures_total"`
	ItemsFetchedTotal   int64         `json:"items_fetched_total"`
	ItemsPublishedTotal int64         `json:"items_published_total"`
	PublishFailures     int64         `json:"publish_failures_total"`
	LastFetchSeconds    float64       `json:"last_fetch_seconds"`
	Healthy             bool          `json:"healthy"`
	Reason              string        `json:"reason,omitempty"`
}

// Tracker aggregates state for all sources of one service.
type Tracker struct {
	producer string
	started  time.Time
	now      func() time.Time

	mu      sync.RWMutex
	sources map[string]*SourceState
	ready   bool
}

// New returns a tracker for the named producer.
func New(producer string) *Tracker {
	return &Tracker{producer: producer, started: time.Now(), now: time.Now, sources: map[string]*SourceState{}}
}

// Register declares a source and its poll interval. Must precede Record* calls.
func (t *Tracker) Register(source string, interval time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sources[source] = &SourceState{Interval: interval, IntervalSeconds: interval.Seconds()}
}

// SetReady flips /readyz (true once the store and publisher are up).
func (t *Tracker) SetReady(ready bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.ready = ready
}

func (t *Tracker) state(source string) *SourceState {
	s, ok := t.sources[source]
	if !ok {
		s = &SourceState{}
		t.sources[source] = s
	}
	return s
}

// RecordSuccess records a completed poll: fetched items and how many were published.
func (t *Tracker) RecordSuccess(source string, fetched, published int, dur time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	s := t.state(source)
	s.LastAttemptAt = &now
	s.LastSuccessAt = &now
	s.LastError = ""
	s.ConsecutiveFailures = 0
	s.FetchTotal++
	s.ItemsFetchedTotal += int64(fetched)
	s.ItemsPublishedTotal += int64(published)
	s.LastFetchSeconds = dur.Seconds()
}

// RecordFailure records a failed poll (fetch or publish stage).
func (t *Tracker) RecordFailure(source string, err error, dur time.Duration, publishStage bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	s := t.state(source)
	s.LastAttemptAt = &now
	s.LastError = err.Error()
	s.ConsecutiveFailures++
	s.FetchTotal++
	s.FetchFailuresTotal++
	if publishStage {
		s.PublishFailures++
	}
	s.LastFetchSeconds = dur.Seconds()
}

// AddPublished bumps the published counter mid-poll (partial progress).
func (t *Tracker) AddPublished(source string, n int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.state(source).ItemsPublishedTotal += int64(n)
}

// Snapshot evaluates health for every source.
func (t *Tracker) Snapshot() (map[string]SourceState, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	now := t.now()
	out := make(map[string]SourceState, len(t.sources))
	all := true
	for name, s := range t.sources {
		c := *s
		c.Healthy, c.Reason = evaluate(&c, now, t.started)
		all = all && c.Healthy
		out[name] = c
	}
	return out, all
}

func evaluate(s *SourceState, now, started time.Time) (bool, string) {
	if s.ConsecutiveFailures >= MaxConsecutiveFailures {
		return false, fmt.Sprintf("%d consecutive failures: %s", s.ConsecutiveFailures, s.LastError)
	}
	if s.Interval <= 0 {
		return true, ""
	}
	limit := StaleFactor * s.Interval
	ref := started
	if s.LastSuccessAt != nil {
		ref = *s.LastSuccessAt
	}
	if now.Sub(ref) > limit {
		if s.LastSuccessAt == nil {
			return false, fmt.Sprintf("no successful poll since start (%s ago)", now.Sub(ref).Round(time.Second))
		}
		return false, fmt.Sprintf("last success %s ago exceeds %s", now.Sub(ref).Round(time.Second), limit)
	}
	return true, ""
}

// Handler serves /healthz, /readyz and /metrics.
func (t *Tracker) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", t.healthz)
	mux.HandleFunc("GET /readyz", t.readyz)
	mux.HandleFunc("GET /metrics", t.metrics)
	return mux
}

func (t *Tracker) healthz(w http.ResponseWriter, _ *http.Request) {
	sources, ok := t.Snapshot()
	w.Header().Set("Content-Type", "application/json")
	if !ok {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"producer":       t.producer,
		"healthy":        ok,
		"uptime_seconds": t.now().Sub(t.started).Seconds(),
		"sources":        sources,
	})
}

func (t *Tracker) readyz(w http.ResponseWriter, _ *http.Request) {
	t.mu.RLock()
	ready := t.ready
	t.mu.RUnlock()
	if !ready {
		http.Error(w, "not ready", http.StatusServiceUnavailable)
		return
	}
	_, _ = w.Write([]byte("ok\n"))
}

// metrics writes Prometheus text exposition format (no client library needed).
func (t *Tracker) metrics(w http.ResponseWriter, _ *http.Request) {
	sources, ok := t.Snapshot()
	names := make([]string, 0, len(sources))
	for n := range sources {
		names = append(names, n)
	}
	sort.Strings(names)

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	fmt.Fprintf(w, "# HELP soteria_ingestion_up 1 if every source is healthy.\n# TYPE soteria_ingestion_up gauge\nsoteria_ingestion_up{producer=%q} %d\n", t.producer, b2i(ok))
	gauge := func(name, help string, f func(SourceState) float64) {
		fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s gauge\n", name, help, name)
		for _, n := range names {
			fmt.Fprintf(w, "%s{producer=%q,source=%q} %g\n", name, t.producer, n, f(sources[n]))
		}
	}
	counter := func(name, help string, f func(SourceState) int64) {
		fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s counter\n", name, help, name)
		for _, n := range names {
			fmt.Fprintf(w, "%s{producer=%q,source=%q} %d\n", name, t.producer, n, f(sources[n]))
		}
	}
	gauge("soteria_ingestion_source_healthy", "1 if the source is healthy.", func(s SourceState) float64 { return float64(b2i(s.Healthy)) })
	gauge("soteria_ingestion_consecutive_failures", "Failed polls in a row.", func(s SourceState) float64 { return float64(s.ConsecutiveFailures) })
	gauge("soteria_ingestion_last_success_timestamp_seconds", "Unix time of the last successful poll (0 if never).", func(s SourceState) float64 {
		if s.LastSuccessAt == nil {
			return 0
		}
		return float64(s.LastSuccessAt.Unix())
	})
	gauge("soteria_ingestion_last_fetch_duration_seconds", "Duration of the last poll.", func(s SourceState) float64 { return s.LastFetchSeconds })
	counter("soteria_ingestion_fetch_total", "Polls attempted.", func(s SourceState) int64 { return s.FetchTotal })
	counter("soteria_ingestion_fetch_failures_total", "Polls that failed.", func(s SourceState) int64 { return s.FetchFailuresTotal })
	counter("soteria_ingestion_items_fetched_total", "Notices returned by the feed.", func(s SourceState) int64 { return s.ItemsFetchedTotal })
	counter("soteria_ingestion_items_published_total", "Notices published to the bus.", func(s SourceState) int64 { return s.ItemsPublishedTotal })
	counter("soteria_ingestion_publish_failures_total", "Polls aborted by a publish failure.", func(s SourceState) int64 { return s.PublishFailures })
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}
