package poller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"soteria/libs/feedkit/dedup"
	"soteria/libs/feedkit/health"
	"soteria/libs/feedkit/publish"
	"soteria/libs/feedkit/source"
)

// ---- fakes -----------------------------------------------------------------

type fakeSource struct {
	items []source.Item
	err   error
	since []time.Time
}

func (f *fakeSource) Name() string { return source.FDAEnforcement }
func (f *fakeSource) Fetch(_ context.Context, since time.Time) ([]source.Item, error) {
	f.since = append(f.since, since)
	return f.items, f.err
}

type fakePub struct {
	sent      []publish.Message
	failAfter int // fail the (failAfter+1)th publish; -1 never fails
}

func (p *fakePub) Publish(_ context.Context, m publish.Message) error {
	if p.failAfter >= 0 && len(p.sent) == p.failAfter {
		return errors.New("broker down")
	}
	p.sent = append(p.sent, m)
	return nil
}
func (p *fakePub) Close() error { return nil }

func item(id string) source.Item {
	return source.Item{
		SourceID:   id,
		Normalized: source.Normalized{Title: "notice " + id, Country: "US"},
		Raw:        json.RawMessage(fmt.Sprintf(`{"id":%q}`, id)),
	}
}

func newPoller(t *testing.T, src *fakeSource, pub *fakePub, now time.Time) (*Poller, *dedup.Store, *health.Tracker) {
	t.Helper()
	store, err := dedup.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	tr := health.New("ingestion-test")
	p := New(Config{Producer: "ingestion-test", Interval: time.Minute, Backfill: 24 * time.Hour, Overlap: time.Hour, Now: func() time.Time { return now }},
		src, store, pub, tr, nil)
	return p, store, tr
}

// ---- tests -----------------------------------------------------------------

func TestTickPublishesOnlyUnseenAndAdvancesSince(t *testing.T) {
	now := time.Date(2026, 9, 12, 7, 0, 0, 0, time.UTC)
	src := &fakeSource{items: []source.Item{item("a"), item("b"), item("b") /* dup within page */}}
	pub := &fakePub{failAfter: -1}
	p, store, _ := newPoller(t, src, pub, now)
	ctx := context.Background()

	res, err := p.Tick(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.Fetched != 3 || res.New != 2 || res.Published != 2 || len(pub.sent) != 2 {
		t.Fatalf("first tick: %+v sent=%d", res, len(pub.sent))
	}
	if !src.since[0].Equal(now.Add(-24 * time.Hour)) {
		t.Errorf("first run should use backfill window, got since=%v", src.since[0])
	}
	if pub.sent[0].RoutingKey != "ingestion.recall.raw.received.v1" || pub.sent[0].MessageID == "" {
		t.Errorf("bad message envelope: %+v", pub.sent[0])
	}

	// Second tick: same feed → nothing new, since = last success − overlap.
	src.items = append(src.items, item("c"))
	res, err = p.Tick(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.New != 1 || res.Published != 1 || len(pub.sent) != 3 {
		t.Fatalf("second tick: %+v sent=%d", res, len(pub.sent))
	}
	if !src.since[1].Equal(now.Add(-time.Hour)) {
		t.Errorf("second run since should be lastSuccess-overlap, got %v", src.since[1])
	}
	if n, _ := store.Count(ctx, source.FDAEnforcement); n != 3 {
		t.Errorf("store count want 3 got %d", n)
	}
}

func TestPublishFailureDoesNotMarkSeen(t *testing.T) {
	now := time.Now()
	src := &fakeSource{items: []source.Item{item("a"), item("b"), item("c")}}
	pub := &fakePub{failAfter: 1} // "a" succeeds, "b" fails
	p, store, tr := newPoller(t, src, pub, now)
	ctx := context.Background()

	_, err := p.Tick(ctx)
	if err == nil {
		t.Fatal("expected publish error")
	}
	if seenA, _ := store.Seen(ctx, source.FDAEnforcement, "a"); !seenA {
		t.Error("a was confirmed and must be marked")
	}
	if seenB, _ := store.Seen(ctx, source.FDAEnforcement, "b"); seenB {
		t.Error("b failed to publish and must NOT be marked")
	}
	if _, ok, _ := store.LastSuccess(ctx, source.FDAEnforcement); ok {
		t.Error("failed tick must not record a last success")
	}
	// One failure is recorded but is below the unhealthy threshold.
	snap, healthy := tr.Snapshot()
	st := snap[source.FDAEnforcement]
	if !healthy || st.ConsecutiveFailures != 1 || st.PublishFailures != 1 || st.ItemsPublishedTotal != 1 {
		t.Errorf("tracker: healthy=%v state=%+v", healthy, st)
	}

	// Broker recovers: b and c go out, nothing duplicated.
	pub.failAfter = -1
	res, err := p.Tick(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.Published != 2 || len(pub.sent) != 3 {
		t.Fatalf("recovery tick: %+v sent=%d", res, len(pub.sent))
	}
	if _, healthy := tr.Snapshot(); !healthy {
		t.Error("tracker should be healthy after a successful tick")
	}
}

func TestFetchFailureIsRecorded(t *testing.T) {
	src := &fakeSource{err: errors.New("503 from feed")}
	p, _, tr := newPoller(t, src, &fakePub{failAfter: -1}, time.Now())
	for i := 0; i < health.MaxConsecutiveFailures; i++ {
		if _, err := p.Tick(context.Background()); err == nil {
			t.Fatal("expected error")
		}
	}
	snap, healthy := tr.Snapshot()
	if healthy || snap[source.FDAEnforcement].ConsecutiveFailures != health.MaxConsecutiveFailures {
		t.Fatalf("expected unhealthy after %d failures: %+v", health.MaxConsecutiveFailures, snap[source.FDAEnforcement])
	}
}

func TestInvalidItemIsSkippedNotFatal(t *testing.T) {
	bad := item("bad")
	bad.Normalized.Country = "MARS"
	src := &fakeSource{items: []source.Item{bad, item("ok")}}
	pub := &fakePub{failAfter: -1}
	p, store, _ := newPoller(t, src, pub, time.Now())
	res, err := p.Tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Published != 1 || len(pub.sent) != 1 {
		t.Fatalf("want exactly the valid item published: %+v", res)
	}
	if seen, _ := store.Seen(context.Background(), source.FDAEnforcement, "bad"); seen {
		t.Error("invalid item must not be marked seen (fix the connector, then it will flow)")
	}
}

func TestOnceModeReturnsTickError(t *testing.T) {
	src := &fakeSource{err: errors.New("down")}
	store, _ := dedup.Open(":memory:")
	defer store.Close()
	p := New(Config{Producer: "x", Interval: time.Minute, Once: true}, src, store, &fakePub{failAfter: -1}, nil, nil)
	if err := p.Run(context.Background()); err == nil {
		t.Fatal("once mode must surface the error")
	}
}
