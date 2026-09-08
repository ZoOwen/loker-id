// Package scraper fetches job postings from external job boards.
package scraper

import (
	"context"
	"time"
)

// Scraper fetches job postings from a single source.
type Scraper interface {
	// Source returns the source slug, e.g. "kalibrr" — must match the
	// slug column of the sources table.
	Source() string

	// Scrape fetches whatever postings are currently listed, walking up
	// to maxPages of paginated results. maxPages <= 0 means "use the
	// implementation's default" (DefaultMaxPages); a value above
	// MaxPagesCap is clamped down to it — see ClampMaxPages, which every
	// implementation should apply as the first thing Scrape does.
	// Implementations should also stop before reaching maxPages once
	// it's clear there's nothing more to page through (an empty page, or
	// one whose jobs all already appeared on the previous page).
	//
	// It returns the jobs collected so far alongside an error when it
	// had to stop early, so a partial run is never silently discarded.
	Scrape(ctx context.Context, maxPages int) ([]RawJob, error)
}

// DefaultMaxPages and MaxPagesCap bound every Scraper implementation's
// maxPages: 3 pages is enough to catch new postings on a normal-sized
// board without hammering it, and 20 is a hard ceiling no caller should
// be able to exceed by accident (at ~2s/page that's already ~40s).
const (
	DefaultMaxPages = 3
	MaxPagesCap     = 20
)

// ClampMaxPages applies the standard default/cap described on Scraper —
// implementations call this once at the top of Scrape, and a caller that
// wants to know the effective value up front (e.g. to size a log message)
// can call it too.
func ClampMaxPages(maxPages int) int {
	if maxPages <= 0 {
		return DefaultMaxPages
	}
	if maxPages > MaxPagesCap {
		return MaxPagesCap
	}
	return maxPages
}

// RawJob is an unparsed job posting straight from a source. Normalizing
// fields such as salary, tech stack, or experience level happens in a
// separate layer — this stays as close to the source's own text as
// possible.
type RawJob struct {
	Title       string
	Company     string
	SalaryRaw   string
	Location    string
	Description string
	SourceURL   string
	SourceJobID string
	PostedAt    *time.Time
}
