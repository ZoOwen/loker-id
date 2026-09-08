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

	// Scrape fetches whatever postings are currently listed. It returns
	// the jobs collected so far alongside an error when it had to stop
	// early, so a partial run is never silently discarded.
	Scrape(ctx context.Context) ([]RawJob, error)
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
