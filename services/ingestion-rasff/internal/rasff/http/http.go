// Package http is the placeholder for a RASFF Window portal-backend adapter.
//
// Findings from probing the portal (2026-09-12): the SPA's API base is
// /rasff-window/backend and the search calls are "/consumer/search" and
// "/notification/search/consolidated", but the exact path prefix, request
// body and pagination live in a lazy-loaded Angular chunk and the obvious
// candidates (backend/public/consumer/search, backend/consumer/search)
// return 404. Until that contract is captured from a browser session and
// verified stable, this mode refuses to start rather than polling a guess.
package http

import (
	"errors"

	"soteria/libs/feedkit/source"
)

// ErrNotImplemented explains how to proceed.
var ErrNotImplemented = errors.New("rasff/http: portal backend adapter not implemented; " +
	"use RASFF_MODE=file with exports in RASFF_FILE_DIR, or capture the /rasff-window/backend search " +
	"request from the browser devtools and implement it here")

// New always fails until the adapter exists.
func New(_ string) (source.Source, error) { return nil, ErrNotImplemented }
