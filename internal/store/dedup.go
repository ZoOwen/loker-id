package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/ZoOwen/loker-id/internal/db"
)

// resolveDuplicates looks for other canonical jobs that match newJob (same
// fingerprint, or same company with a fuzzy-matching title) and, if any
// are found, decides which job among {newJob, candidates...} becomes
// canonical, then points every other one at it — including flattening any
// existing duplicates of a job that gets displaced from canonical status.
//
// Returns the winning job's id and whether newJob itself ended up marked
// as a duplicate of some other job.
func (s *Store) resolveDuplicates(ctx context.Context, q *db.Queries, newJob db.Job) (winnerID uuid.UUID, newJobIsDuplicate bool, err error) {
	candidates, err := q.FindDuplicateCandidates(ctx, db.FindDuplicateCandidatesParams{
		ExcludeJobID:    newJob.ID,
		Fingerprint:     newJob.Fingerprint,
		CompanyID:       newJob.CompanyID,
		TitleNormalized: newJob.TitleNormalized,
	})
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("find duplicate candidates: %w", err)
	}
	if len(candidates) == 0 {
		return newJob.ID, false, nil
	}

	group := append([]db.Job{newJob}, candidates...)
	winner := canonicalOf(group)

	for _, loser := range group {
		if loser.ID == winner.ID {
			continue
		}

		if err := q.MarkAsDuplicate(ctx, db.MarkAsDuplicateParams{
			CanonicalJobID: winner.ID,
			JobID:          loser.ID,
		}); err != nil {
			return uuid.Nil, false, fmt.Errorf("mark %s as duplicate of %s: %w", loser.ID, winner.ID, err)
		}

		// loser may itself have been canonical for other jobs already —
		// point those straight at the new winner instead of leaving a
		// two-level chain.
		if err := q.ReparentDuplicates(ctx, db.ReparentDuplicatesParams{
			NewCanonicalID: winner.ID,
			OldCanonicalID: loser.ID,
		}); err != nil {
			return uuid.Nil, false, fmt.Errorf("reparent duplicates of %s to %s: %w", loser.ID, winner.ID, err)
		}
	}

	return winner.ID, winner.ID != newJob.ID, nil
}

// canonicalOf picks the canonical job from a group of jobs that all refer
// to the same posting: earliest posted_at wins, ties (including "both
// unknown") broken by earliest first_seen_at.
func canonicalOf(group []db.Job) db.Job {
	best := group[0]
	for _, j := range group[1:] {
		if postedBefore(j, best) {
			best = j
		}
	}
	return best
}

// postedBefore reports whether a should be preferred as canonical over b:
// a's posted_at is earlier, or unknown loses to known, or (both equal or
// both unknown) a's first_seen_at is earlier.
func postedBefore(a, b db.Job) bool {
	switch {
	case a.PostedAt.Valid && !b.PostedAt.Valid:
		return true
	case !a.PostedAt.Valid && b.PostedAt.Valid:
		return false
	case a.PostedAt.Valid && b.PostedAt.Valid && !a.PostedAt.Time.Equal(b.PostedAt.Time):
		return a.PostedAt.Time.Before(b.PostedAt.Time)
	default:
		return a.FirstSeenAt.Time.Before(b.FirstSeenAt.Time)
	}
}
