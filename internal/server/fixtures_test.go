package server

import (
	"context"
	"math/rand"
	"testing"

	"github.com/google/uuid"

	"github.com/ZoOwen/loker-id/internal/normalizer"
	"github.com/ZoOwen/loker-id/internal/parser"
	"github.com/ZoOwen/loker-id/internal/store"
)

// seedJob creates a company and a job for it in one call. TitleNormalized
// and Fingerprint both default to values derived from sourceURL, so two
// jobs from different URLs never accidentally collide via the real
// fuzzy-dedup rule (same company + trigram similarity > 0.7) — verified
// the hard way in internal/store's own tests.
func seedJob(t *testing.T, st *store.Store, sourceID int32, company, title, sourceURL string) store.UpsertJobResult {
	t.Helper()
	return seedJobOpts(t, st, seedOpts{
		sourceID: sourceID, company: company, title: title, sourceURL: sourceURL,
	})
}

func seedJobWithFingerprint(t *testing.T, st *store.Store, sourceID int32, company, title, sourceURL, fingerprint string) store.UpsertJobResult {
	t.Helper()
	return seedJobOpts(t, st, seedOpts{
		sourceID: sourceID, company: company, title: title, sourceURL: sourceURL, fingerprint: fingerprint,
	})
}

func seedJobWithStack(t *testing.T, st *store.Store, sourceID int32, company, title, sourceURL string, stack []string) store.UpsertJobResult {
	t.Helper()
	return seedJobOpts(t, st, seedOpts{
		sourceID: sourceID, company: company, title: title, sourceURL: sourceURL, stack: stack,
	})
}

type seedOpts struct {
	sourceID    int32
	company     string
	title       string
	sourceURL   string
	fingerprint string   // defaults to "fp-"+sourceURL
	stack       []string // defaults to []string{}
}

func seedJobOpts(t *testing.T, st *store.Store, o seedOpts) store.UpsertJobResult {
	t.Helper()
	ctx := context.Background()

	companyID, err := st.UpsertCompany(ctx, o.company, normalizer.NormalizeCompany(o.company))
	if err != nil {
		t.Fatalf("seedJob: UpsertCompany(%q): %v", o.company, err)
	}

	fingerprint := o.fingerprint
	if fingerprint == "" {
		fingerprint = "fp-" + o.sourceURL
	}
	stack := o.stack
	if stack == nil {
		stack = []string{}
	}

	result, err := st.UpsertJob(ctx, store.UpsertJobParams{
		Title:           o.title,
		TitleNormalized: randomAlnum(24),
		CompanyID:       companyID,
		Description:     "A job description.",
		Salary:          parser.Salary{Conf: parser.ConfUnknown},
		Stack:           stack,
		Location:        "Jakarta, Indonesia",
		LocationCity:    "Jakarta",
		Mode:            normalizer.ModeUnknown,
		Level:           normalizer.LevelUnknown,
		SourceID:        o.sourceID,
		SourceURL:       o.sourceURL,
		SourceJobID:     o.sourceURL,
		Fingerprint:     fingerprint,
	})
	if err != nil {
		t.Fatalf("seedJob: UpsertJob(%q): %v", o.sourceURL, err)
	}
	return result
}

func mustSourceID(t *testing.T, st *store.Store, slug string) int32 {
	t.Helper()
	src, err := st.GetSourceBySlug(context.Background(), slug)
	if err != nil {
		t.Fatalf("GetSourceBySlug(%q): %v", slug, err)
	}
	return src.ID
}

func mustRandomUUID(t *testing.T) uuid.UUID {
	t.Helper()
	id, err := uuid.NewRandom()
	if err != nil {
		t.Fatalf("uuid.NewRandom(): %v", err)
	}
	return id
}

const alnumAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"

func randomAlnum(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = alnumAlphabet[rand.Intn(len(alnumAlphabet))]
	}
	return string(b)
}
