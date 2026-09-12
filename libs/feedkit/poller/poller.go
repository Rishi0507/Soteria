// Package poller runs the fetch → dedupe → publish → mark loop for one source.
//
// Ordering matters for the delivery guarantee: a notice is marked seen only
// after the broker confirms the publish, so the pipeline is at-least-once
// and never at-most-once. A crash between publish and mark re-emits the
// notice with a fresh event_id; consumers dedupe on (source, source_id).
package poller

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"time"

	"soteria/libs/feedkit/dedup"
	"soteria/libs/feedkit/event"
	"soteria/libs/feedkit/health"
	"soteria/libs/feedkit/publish"
	"soteria/libs/feedkit/source"
)

// Config tunes one poller.
type Config struct {
	Producer string
	Interval time.Duration // poll cadence
	Backfill time.Duration // how far back to look on first run (default 30d)
	Overlap  time.Duration // re-request window before last success (default 48h)
	Once     bool          // run a single tick and return
	Now      func() time.Time
}

// Poller owns the loop for one source.
type Poller struct {
	cfg     Config
	src     source.Source
	store   *dedup.Store
	pub     publish.Publisher
	tracker *health.Tracker
	log     *slog.Logger
}

// New wires a poller; tracker and log may be nil.
func New(cfg Config, src source.Source, store *dedup.Store, pub publish.Publisher, tracker *health.Tracker, log *slog.Logger) *Poller {
	if cfg.Backfill <= 0 {
		cfg.Backfill = 30 * 24 * time.Hour
	}
	if cfg.Overlap <= 0 {
		cfg.Overlap = 48 * time.Hour
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if log == nil {
		log = slog.Default()
	}
	if tracker == nil {
		tracker = health.New(cfg.Producer)
	}
	tracker.Register(src.Name(), cfg.Interval)
	return &Poller{cfg: cfg, src: src, store: store, pub: pub, tracker: tracker, log: log.With("source", src.Name())}
}

// Run polls until ctx is cancelled (or once, if configured). In once mode the
// tick's error is returned; in loop mode errors are logged and retried next tick.
func (p *Poller) Run(ctx context.Context) error {
	if p.cfg.Once {
		_, err := p.Tick(ctx)
		return err
	}
	for {
		if _, err := p.Tick(ctx); err != nil && !errors.Is(err, context.Canceled) {
			p.log.Error("poll failed", "err", err)
		}
		if err := sleep(ctx, jitter(p.cfg.Interval)); err != nil {
			return err
		}
	}
}

// Result summarizes one tick.
type Result struct {
	Fetched   int
	New       int
	Published int
}

// Tick runs a single fetch/publish pass.
func (p *Poller) Tick(ctx context.Context) (Result, error) {
	start := p.cfg.Now()
	name := p.src.Name()

	since, err := p.since(ctx, start)
	if err != nil {
		return Result{}, p.fail(start, err, false)
	}

	items, err := p.src.Fetch(ctx, since)
	if err != nil {
		return Result{}, p.fail(start, fmt.Errorf("fetch: %w", err), false)
	}
	res := Result{Fetched: len(items)}

	ids := make([]string, 0, len(items))
	byID := make(map[string]source.Item, len(items))
	for _, it := range items {
		if it.SourceID == "" {
			p.log.Warn("item without source_id skipped", "title", it.Normalized.Title)
			continue
		}
		if _, dup := byID[it.SourceID]; dup {
			continue // feed returned the same notice twice in one page set
		}
		ids = append(ids, it.SourceID)
		byID[it.SourceID] = it
	}
	unseen, err := p.store.Unseen(ctx, name, ids)
	if err != nil {
		return res, p.fail(start, fmt.Errorf("dedup: %w", err), false)
	}
	res.New = len(unseen)

	for _, id := range unseen {
		it := byID[id]
		ev := event.New(p.cfg.Producer, name, it, p.cfg.Now())
		body, err := ev.Marshal()
		if err != nil {
			// A contract violation is a connector bug, not a feed outage: log
			// loudly, skip this notice, keep the tick alive.
			p.log.Error("event failed validation; skipped", "source_id", id, "err", err)
			continue
		}
		if err := p.pub.Publish(ctx, publish.Message{
			RoutingKey: event.RoutingKey,
			MessageID:  ev.EventID,
			Type:       ev.EventType,
			AppID:      ev.Producer,
			Timestamp:  ev.OccurredAt,
			Body:       body,
		}); err != nil {
			p.tracker.AddPublished(name, res.Published)
			return res, p.fail(start, fmt.Errorf("publish %s: %w", id, err), true)
		}
		if err := p.store.Mark(ctx, name, id, ev.EventID, ev.OccurredAt); err != nil {
			return res, p.fail(start, fmt.Errorf("mark %s: %w", id, err), false)
		}
		res.Published++
		p.log.Info("published", "source_id", id, "event_id", ev.EventID, "title", it.Normalized.Title)
	}

	if err := p.store.SetLastSuccess(ctx, name, start); err != nil {
		return res, p.fail(start, fmt.Errorf("record success: %w", err), false)
	}
	p.tracker.RecordSuccess(name, res.Fetched, res.Published, p.cfg.Now().Sub(start))
	p.log.Info("poll ok", "fetched", res.Fetched, "new", res.New, "published", res.Published, "since", since.Format(time.RFC3339))
	return res, nil
}

func (p *Poller) since(ctx context.Context, now time.Time) (time.Time, error) {
	last, ok, err := p.store.LastSuccess(ctx, p.src.Name())
	if err != nil {
		return time.Time{}, fmt.Errorf("last success: %w", err)
	}
	if !ok {
		return now.Add(-p.cfg.Backfill), nil
	}
	return last.Add(-p.cfg.Overlap), nil
}

func (p *Poller) fail(start time.Time, err error, publishStage bool) error {
	p.tracker.RecordFailure(p.src.Name(), err, p.cfg.Now().Sub(start), publishStage)
	return err
}

func jitter(d time.Duration) time.Duration {
	if d <= 0 {
		return d
	}
	spread := int64(d) / 10 // ±10%
	return d + time.Duration(rand.Int64N(2*spread+1)-spread)
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
