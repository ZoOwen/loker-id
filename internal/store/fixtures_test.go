package store

import (
	"context"
	"math/rand"
	"testing"

	"github.com/google/uuid"

	"github.com/ZoOwen/loker-id/internal/normalizer"
	"github.com/ZoOwen/loker-id/internal/parser"
)

// testCompany creates a company for tests that don't care about company
// dedup themselves and just need a valid company_id to attach jobs to.
func testCompany(t *testing.T, s *Store, name string) uuid.UUID {
	t.Helper()
	id, err := s.UpsertCompany(context.Background(), name, normalizer.NormalizeCompany(name))
	if err != nil {
		t.Fatalf("testCompany(%q): %v", name, err)
	}
	return id
}

// testJobParams returns a valid, minimal UpsertJobParams for sourceURL,
// overridable field-by-field by the caller. TitleNormalized defaults to a
// random string and Fingerprint to a value derived from sourceURL, so two
// jobs built from different URLs never accidentally collide via the dedup
// rules (same company + fuzzy-matching title) unless a test explicitly
// sets matching values on purpose.
//
// TitleNormalized specifically must be random, not merely different — a
// shared prefix like "backendengineer<url>" still scores ~0.78 trigram
// similarity between two otherwise-different suffixes (verified against
// the real similarity() function), comfortably above the 0.7 dedup
// threshold. Trigram similarity is a real comparison here, not a mock, so
// near-identical default titles at the same company genuinely would merge
// into one canonical job and quietly break any test with more than one
// job per company.
func testJobParams(companyID uuid.UUID, sourceID int32, sourceURL string) UpsertJobParams {
	return UpsertJobParams{
		Title:           "Backend Engineer",
		TitleNormalized: randomAlnum(24),
		CompanyID:       companyID,
		Description:     "Job description",
		Salary:          parser.Salary{Conf: parser.ConfUnknown},
		Stack:           []string{"Go", "PostgreSQL"},
		Location:        "Jakarta, Indonesia",
		LocationCity:    "Jakarta",
		Mode:            normalizer.ModeOnsite,
		Level:           normalizer.LevelUnknown,
		SourceID:        sourceID,
		SourceURL:       sourceURL,
		SourceJobID:     "1",
		Fingerprint:     "fp-" + sourceURL,
	}
}

const alnumAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"

func randomAlnum(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = alnumAlphabet[rand.Intn(len(alnumAlphabet))]
	}
	return string(b)
}
