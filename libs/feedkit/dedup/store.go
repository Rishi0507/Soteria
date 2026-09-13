// Package dedup is the per-service SQLite store of notices already published
// and of each source's last successful poll.
package dedup

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS seen (
	source        TEXT NOT NULL,
	source_id     TEXT NOT NULL,
	event_id      TEXT NOT NULL,
	first_seen_at TEXT NOT NULL,
	PRIMARY KEY (source, source_id)
);
CREATE TABLE IF NOT EXISTS runs (
	source          TEXT PRIMARY KEY,
	last_success_at TEXT NOT NULL
);`

// Store wraps the SQLite database.
type Store struct{ db *sql.DB }

// Open opens (creating if needed) the database at path. ":memory:" is
// accepted for tests.
func Open(path string) (*Store, error) {
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, fmt.Errorf("dedup: mkdir: %w", err)
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("dedup: open: %w", err)
	}
	// A single connection keeps :memory: databases and WAL writers simple.
	db.SetMaxOpenConns(1)
	if path != ":memory:" {
		if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;`); err != nil {
			db.Close()
			return nil, fmt.Errorf("dedup: pragma: %w", err)
		}
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("dedup: migrate: %w", err)
	}
	return &Store{db: db}, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// Unseen returns the subset of ids not yet marked for source, preserving order.
func (s *Store) Unseen(ctx context.Context, source string, ids []string) ([]string, error) {
	stmt, err := s.db.PrepareContext(ctx, `SELECT 1 FROM seen WHERE source = ? AND source_id = ?`)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		var one int
		err := stmt.QueryRowContext(ctx, source, id).Scan(&one)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			out = append(out, id)
		case err != nil:
			return nil, err
		}
	}
	return out, nil
}

// Mark records that source_id was published as event_id. Idempotent.
func (s *Store) Mark(ctx context.Context, source, sourceID, eventID string, at time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO seen (source, source_id, event_id, first_seen_at) VALUES (?, ?, ?, ?)`,
		source, sourceID, eventID, at.UTC().Format(time.RFC3339Nano))
	return err
}

// Seen reports whether source_id was already marked.
func (s *Store) Seen(ctx context.Context, source, sourceID string) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM seen WHERE source = ? AND source_id = ?`, source, sourceID).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// Count returns the number of marked notices for source.
func (s *Store) Count(ctx context.Context, source string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM seen WHERE source = ?`, source).Scan(&n)
	return n, err
}

// LastSuccess returns the last successful poll time for source, ok=false if never.
func (s *Store) LastSuccess(ctx context.Context, source string) (time.Time, bool, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT last_success_at FROM runs WHERE source = ?`, source).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, false, err
	}
	return t, true, nil
}

// SetLastSuccess upserts the last successful poll time for source.
func (s *Store) SetLastSuccess(ctx context.Context, source string, at time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO runs (source, last_success_at) VALUES (?, ?)
		 ON CONFLICT(source) DO UPDATE SET last_success_at = excluded.last_success_at`,
		source, at.UTC().Format(time.RFC3339Nano))
	return err
}
