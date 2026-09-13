// Package file is the RASFF Source that reads notification exports
// (*.json, *.csv) from a directory. Every poll re-reads every file; the
// dedup store makes that idempotent, so operators can keep dropping fresh
// exports into the folder without cleaning up old ones.
package file

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"soteria/libs/feedkit/source"
	"soteria/services/ingestion-rasff/internal/rasff"
)

// Source reads exports from Dir.
type Source struct {
	Dir string
}

// New returns a file source for dir.
func New(dir string) *Source { return &Source{Dir: dir} }

func (s *Source) Name() string { return source.EURASFF }

// Fetch parses every export in Dir and returns notifications dated at or
// after since (undated ones are always returned).
func (s *Source) Fetch(_ context.Context, since time.Time) ([]source.Item, error) {
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		return nil, fmt.Errorf("rasff/file: read dir %q: %w", s.Dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		switch strings.ToLower(filepath.Ext(e.Name())) {
		case ".json", ".csv":
			names = append(names, e.Name())
		}
	}
	sort.Strings(names) // deterministic order across polls
	var items []source.Item
	for _, name := range names {
		ns, err := parseFile(filepath.Join(s.Dir, name))
		if err != nil {
			return nil, err
		}
		for _, n := range ns {
			it, err := rasff.ToItem(n)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", name, err)
			}
			if it.PublishedAt != nil && it.PublishedAt.Before(since) {
				continue
			}
			items = append(items, it)
		}
	}
	return items, nil
}

func parseFile(path string) ([]rasff.Notification, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var ns []rasff.Notification
	if strings.EqualFold(filepath.Ext(path), ".csv") {
		ns, err = rasff.ParseCSV(f)
	} else {
		ns, err = rasff.ParseJSON(f)
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return ns, nil
}
