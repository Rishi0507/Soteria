package dossier

import (
	"testing"
	"time"
)

// TestOrderPutsCauseBeforeEffect: three events minted inside the same
// millisecond used to render as rescue-before-containment, which reads as
// nonsense in a document meant to establish what happened.
func TestOrderPutsCauseBeforeEffect(t *testing.T) {
	same := time.Date(2026, 9, 13, 10, 0, 0, 0, time.UTC)

	// Recorded in arrival order: the effect landed first.
	entries := []Entry{
		{Seq: 1, At: same, Event: "rescue.order.proposed.v1", EventID: "c", CausationID: "b"},
		{Seq: 2, At: same, Event: "containment.action.taken.v1", EventID: "b", CausationID: "a"},
		{Seq: 3, At: same.Add(-2 * time.Millisecond), Event: "resolution.lot.resolved.v1", EventID: "a"},
	}

	got := order(entries)
	want := []string{"a", "b", "c"}
	for i, id := range want {
		if got[i].EventID != id {
			t.Fatalf("position %d is %s (%s), want %s; full order %v",
				i, got[i].EventID, got[i].Event, id, ids(got))
		}
	}
}

func TestOrderIsChronologicalWhenNothingIsCausallyLinked(t *testing.T) {
	base := time.Date(2026, 9, 13, 10, 0, 0, 0, time.UTC)
	entries := []Entry{
		{Seq: 1, At: base.Add(2 * time.Second), EventID: "late"},
		{Seq: 2, At: base, EventID: "early"},
		{Seq: 3, At: base.Add(time.Second), EventID: "middle"},
	}
	if got := ids(order(entries)); got != "early,middle,late" {
		t.Fatalf("order = %s", got)
	}
}

// TestOrderKeepsEveryEntry: an incomplete timeline is worse than an imperfectly
// ordered one, so a cycle or a dangling cause must not drop anything.
func TestOrderKeepsEveryEntry(t *testing.T) {
	at := time.Date(2026, 9, 13, 10, 0, 0, 0, time.UTC)
	cyclic := []Entry{
		{Seq: 1, At: at, EventID: "x", CausationID: "y"},
		{Seq: 2, At: at, EventID: "y", CausationID: "x"},
		{Seq: 3, At: at, EventID: "z", CausationID: "gone-elsewhere"},
	}
	got := order(cyclic)
	if len(got) != 3 {
		t.Fatalf("order dropped entries: %v", ids(got))
	}
}

func ids(entries []Entry) string {
	out := ""
	for i, e := range entries {
		if i > 0 {
			out += ","
		}
		out += e.EventID
	}
	return out
}
