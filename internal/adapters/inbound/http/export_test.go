package http

import (
	"net/http"
	"slices"
)

// ReplayEntry is one problem type of the service as the replay-class tests
// need it: the typed error that makes the production writer answer it.
type ReplayEntry struct {
	Err    error
	Slug   string
	Status int
	Class  string
}

// ReplayCatalogue returns every problem type a handler behind the
// Idempotency-Key middleware answers through writeError, read from the
// production catalogue and the adapter's own typed errors, one entry per slug.
func ReplayCatalogue() []ReplayEntry {
	var out []ReplayEntry
	add := func(err error, class replayClass) {
		p := problemFor(err)
		if !slices.ContainsFunc(out, func(e ReplayEntry) bool { return e.Slug == p.slug }) {
			out = append(out, ReplayEntry{Err: err, Slug: p.slug, Status: p.status, Class: class.String()})
		}
	}
	for _, e := range problemCatalogue {
		add(e.err, e.replay)
	}
	for _, err := range adapterErrors {
		add(err, replayOf(err))
	}
	return out
}

// WriteError answers err through the production error writer, so the replay
// class of its problem marks the capturing writer of the middleware.
func WriteError(w http.ResponseWriter, r *http.Request, err error) { writeError(w, r, err) }
