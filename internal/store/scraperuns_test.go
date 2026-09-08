package store

import (
	"context"
	"testing"

	"github.com/ZoOwen/loker-id/internal/db"
)

func TestCreateAndFinishScrapeRun(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	sourceID := mustSourceID(t, s, "kalibrr")

	id, err := s.CreateScrapeRun(ctx, sourceID)
	if err != nil {
		t.Fatalf("CreateScrapeRun() error = %v", err)
	}

	var status string
	var finishedAt any
	if err := s.pool.QueryRow(ctx, "SELECT status, finished_at FROM scrape_runs WHERE id = $1", id).Scan(&status, &finishedAt); err != nil {
		t.Fatalf("read back scrape run: %v", err)
	}
	if status != string(db.RunStatusRunning) {
		t.Errorf("status after create = %q, want %q", status, db.RunStatusRunning)
	}
	if finishedAt != nil {
		t.Errorf("finished_at after create = %v, want NULL", finishedAt)
	}

	err = s.FinishScrapeRun(ctx, FinishScrapeRunParams{
		ID:            id,
		Status:        db.RunStatusSuccess,
		JobsFound:     42,
		JobsNew:       10,
		JobsDuplicate: 3,
	})
	if err != nil {
		t.Fatalf("FinishScrapeRun() error = %v", err)
	}

	var jobsFound, jobsNew, jobsDup int32
	var errMsg *string
	if err := s.pool.QueryRow(ctx,
		"SELECT status, jobs_found, jobs_new, jobs_duplicate, error_message, finished_at FROM scrape_runs WHERE id = $1", id,
	).Scan(&status, &jobsFound, &jobsNew, &jobsDup, &errMsg, &finishedAt); err != nil {
		t.Fatalf("read back finished scrape run: %v", err)
	}

	if status != string(db.RunStatusSuccess) {
		t.Errorf("status = %q, want %q", status, db.RunStatusSuccess)
	}
	if jobsFound != 42 || jobsNew != 10 || jobsDup != 3 {
		t.Errorf("counts = found=%d new=%d dup=%d, want 42/10/3", jobsFound, jobsNew, jobsDup)
	}
	if errMsg != nil {
		t.Errorf("error_message = %v, want NULL for a successful run", *errMsg)
	}
	if finishedAt == nil {
		t.Error("finished_at is NULL, want it set")
	}
}

func TestFinishScrapeRun_RecordsErrorMessageOnFailure(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	sourceID := mustSourceID(t, s, "kalibrr")

	id, err := s.CreateScrapeRun(ctx, sourceID)
	if err != nil {
		t.Fatalf("CreateScrapeRun() error = %v", err)
	}

	err = s.FinishScrapeRun(ctx, FinishScrapeRunParams{
		ID:           id,
		Status:       db.RunStatusFailed,
		ErrorMessage: "robots.txt disallowed",
	})
	if err != nil {
		t.Fatalf("FinishScrapeRun() error = %v", err)
	}

	var status string
	var errMsg *string
	if err := s.pool.QueryRow(ctx, "SELECT status, error_message FROM scrape_runs WHERE id = $1", id).Scan(&status, &errMsg); err != nil {
		t.Fatalf("read back scrape run: %v", err)
	}
	if status != string(db.RunStatusFailed) {
		t.Errorf("status = %q, want %q", status, db.RunStatusFailed)
	}
	if errMsg == nil || *errMsg != "robots.txt disallowed" {
		t.Errorf("error_message = %v, want %q", errMsg, "robots.txt disallowed")
	}
}
