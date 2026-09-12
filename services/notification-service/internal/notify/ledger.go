package notify

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

// Delivery statuses.
const (
	StatusPending = "PENDING"
	StatusSent    = "SENT"
	StatusFailed  = "FAILED"   // permanent; will not retry
	StatusRetry   = "RETRYING" // transient; the bus will redeliver
)

// Delivery is one (event, channel, recipient) attempt record.
type Delivery struct {
	ID          int64
	EventID     string
	EventType   string
	IncidentID  string
	Channel     string
	Recipient   string
	Subject     string
	Status      string
	Attempts    int
	ProviderRef string
	LastError   string
	CreatedAt   time.Time
	SentAt      *time.Time
}

const schema = `
CREATE TABLE IF NOT EXISTS deliveries (
	id           INTEGER PRIMARY KEY AUTOINCREMENT,
	event_id     TEXT NOT NULL,
	event_type   TEXT NOT NULL,
	incident_id  TEXT NOT NULL DEFAULT '',
	channel      TEXT NOT NULL,
	recipient    TEXT NOT NULL,
	subject      TEXT NOT NULL DEFAULT '',
	status       TEXT NOT NULL,
	attempts     INTEGER NOT NULL DEFAULT 0,
	provider_ref TEXT NOT NULL DEFAULT '',
	last_error   TEXT NOT NULL DEFAULT '',
	created_at   TEXT NOT NULL,
	sent_at      TEXT,
	UNIQUE (event_id, channel, recipient)
);
CREATE INDEX IF NOT EXISTS deliveries_incident ON deliveries (incident_id);`

// Ledger is the SQLite delivery record store.
type Ledger struct{ db *sql.DB }

// OpenLedger opens (creating) the database; ":memory:" for tests.
func OpenLedger(path string) (*Ledger, error) {
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if path != ":memory:" {
		if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;`); err != nil {
			db.Close()
			return nil, err
		}
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("ledger: migrate: %w", err)
	}
	return &Ledger{db: db}, nil
}

// Close closes the store.
func (l *Ledger) Close() error { return l.db.Close() }

// Begin returns the existing record for (event, channel, recipient) or
// creates a PENDING one. The bool is true when the message was already SENT
// (redelivery) so the caller must skip it.
func (l *Ledger) Begin(ctx context.Context, d Delivery) (Delivery, bool, error) {
	_, err := l.db.ExecContext(ctx, `INSERT OR IGNORE INTO deliveries
		(event_id, event_type, incident_id, channel, recipient, subject, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		d.EventID, d.EventType, d.IncidentID, d.Channel, d.Recipient, d.Subject, StatusPending, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return Delivery{}, false, err
	}
	cur, err := l.get(ctx, d.EventID, d.Channel, d.Recipient)
	if err != nil {
		return Delivery{}, false, err
	}
	return cur, cur.Status == StatusSent, nil
}

// Attempt increments the attempt counter.
func (l *Ledger) Attempt(ctx context.Context, id int64) error {
	_, err := l.db.ExecContext(ctx, `UPDATE deliveries SET attempts = attempts + 1 WHERE id = ?`, id)
	return err
}

// MarkSent records success.
func (l *Ledger) MarkSent(ctx context.Context, id int64, providerRef string, at time.Time) error {
	_, err := l.db.ExecContext(ctx, `UPDATE deliveries SET status = ?, provider_ref = ?, last_error = '', sent_at = ? WHERE id = ?`,
		StatusSent, providerRef, at.UTC().Format(time.RFC3339Nano), id)
	return err
}

// MarkFailed records a terminal (permanent) failure.
func (l *Ledger) MarkFailed(ctx context.Context, id int64, cause error) error {
	_, err := l.db.ExecContext(ctx, `UPDATE deliveries SET status = ?, last_error = ? WHERE id = ?`, StatusFailed, cause.Error(), id)
	return err
}

// MarkRetrying records a transient failure the bus will redeliver.
func (l *Ledger) MarkRetrying(ctx context.Context, id int64, cause error) error {
	_, err := l.db.ExecContext(ctx, `UPDATE deliveries SET status = ?, last_error = ? WHERE id = ?`, StatusRetry, cause.Error(), id)
	return err
}

// ByIncident lists deliveries for an incident, oldest first.
func (l *Ledger) ByIncident(ctx context.Context, incidentID string) ([]Delivery, error) {
	rows, err := l.db.QueryContext(ctx, `SELECT id, event_id, event_type, incident_id, channel, recipient, subject, status, attempts, provider_ref, last_error, created_at, sent_at
		FROM deliveries WHERE incident_id = ? ORDER BY id`, incidentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Delivery
	for rows.Next() {
		d, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// Counts returns totals per status.
func (l *Ledger) Counts(ctx context.Context) (map[string]int, error) {
	rows, err := l.db.QueryContext(ctx, `SELECT status, COUNT(*) FROM deliveries GROUP BY status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var s string
		var n int
		if err := rows.Scan(&s, &n); err != nil {
			return nil, err
		}
		out[s] = n
	}
	return out, rows.Err()
}

func (l *Ledger) get(ctx context.Context, eventID, channel, recipient string) (Delivery, error) {
	row := l.db.QueryRowContext(ctx, `SELECT id, event_id, event_type, incident_id, channel, recipient, subject, status, attempts, provider_ref, last_error, created_at, sent_at
		FROM deliveries WHERE event_id = ? AND channel = ? AND recipient = ?`, eventID, channel, recipient)
	d, err := scan(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Delivery{}, fmt.Errorf("ledger: record vanished for %s/%s", eventID, channel)
	}
	return d, err
}

type scanner interface{ Scan(dest ...any) error }

func scan(s scanner) (Delivery, error) {
	var d Delivery
	var created string
	var sent sql.NullString
	if err := s.Scan(&d.ID, &d.EventID, &d.EventType, &d.IncidentID, &d.Channel, &d.Recipient, &d.Subject, &d.Status, &d.Attempts, &d.ProviderRef, &d.LastError, &created, &sent); err != nil {
		return Delivery{}, err
	}
	d.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	if sent.Valid {
		t, _ := time.Parse(time.RFC3339Nano, sent.String)
		d.SentAt = &t
	}
	return d, nil
}
