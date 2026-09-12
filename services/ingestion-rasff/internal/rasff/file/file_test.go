package file

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFetchReadsJSONAndCSVInOrderAndFiltersBySince(t *testing.T) {
	dir := t.TempDir()
	js, _ := os.ReadFile("../../../fixtures/rasff_2026-09_sample.json")
	csv, _ := os.ReadFile("../testdata/export_semicolon.csv")
	os.WriteFile(filepath.Join(dir, "b_export.csv"), csv, 0o644)
	os.WriteFile(filepath.Join(dir, "a_sample.json"), js, 0o644)
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("ignored"), 0o644)

	items, err := New(dir).Fetch(context.Background(), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 6 {
		t.Fatalf("want 4 json + 2 csv = 6, got %d", len(items))
	}
	if items[0].SourceID != "2026.5412" || items[5].SourceID != "2026.5502" {
		t.Errorf("files not read in sorted order: first=%s last=%s", items[0].SourceID, items[5].SourceID)
	}

	recent, err := New(dir).Fetch(context.Background(), time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 3 { // 2026.5471 (11th) + two CSV rows (12th)
		t.Fatalf("since filter: want 3 got %d", len(recent))
	}
}

func TestMissingDirIsAnError(t *testing.T) {
	if _, err := New(filepath.Join(t.TempDir(), "nope")).Fetch(context.Background(), time.Time{}); err == nil {
		t.Fatal("a missing export directory must be an outage, not an empty poll")
	}
}
