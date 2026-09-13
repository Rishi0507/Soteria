package dedup

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestUnseenMarkRoundTrip(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()

	unseen, err := s.Unseen(ctx, "fda_enforcement", []string{"a", "b", "c"})
	if err != nil {
		t.Fatal(err)
	}
	if len(unseen) != 3 {
		t.Fatalf("want 3 unseen, got %v", unseen)
	}

	if err := s.Mark(ctx, "fda_enforcement", "b", "evt-1", time.Now()); err != nil {
		t.Fatal(err)
	}
	// Marking twice must be a no-op, not an error.
	if err := s.Mark(ctx, "fda_enforcement", "b", "evt-2", time.Now()); err != nil {
		t.Fatal(err)
	}

	unseen, err = s.Unseen(ctx, "fda_enforcement", []string{"a", "b", "c"})
	if err != nil {
		t.Fatal(err)
	}
	if len(unseen) != 2 || unseen[0] != "a" || unseen[1] != "c" {
		t.Fatalf("want [a c], got %v", unseen)
	}

	// Same id under a different source is a different notice.
	seen, err := s.Seen(ctx, "usda_fsis", "b")
	if err != nil || seen {
		t.Fatalf("cross-source leak: seen=%v err=%v", seen, err)
	}
	n, _ := s.Count(ctx, "fda_enforcement")
	if n != 1 {
		t.Fatalf("count: want 1 got %d", n)
	}
}

func TestLastSuccessPersistsAcrossOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.db")
	ctx := context.Background()
	at := time.Date(2026, 9, 12, 7, 0, 0, 0, time.UTC)

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.LastSuccess(ctx, "fda_press"); ok {
		t.Fatal("fresh store should have no last success")
	}
	if err := s.SetLastSuccess(ctx, "fda_press", at); err != nil {
		t.Fatal(err)
	}
	if err := s.SetLastSuccess(ctx, "fda_press", at.Add(time.Hour)); err != nil {
		t.Fatal(err) // upsert
	}
	s.Close()

	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, ok, err := s.LastSuccess(ctx, "fda_press")
	if err != nil || !ok || !got.Equal(at.Add(time.Hour)) {
		t.Fatalf("got %v ok=%v err=%v", got, ok, err)
	}
}
