package store

import (
	"context"
	"testing"
)

func TestUpsertCompany_InsertsNew(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id, err := s.UpsertCompany(ctx, "PT Gojek Indonesia", "gojek indonesia")
	if err != nil {
		t.Fatalf("UpsertCompany() error = %v", err)
	}
	if id.String() == "" {
		t.Fatal("UpsertCompany() returned a zero id")
	}

	var name string
	if err := s.pool.QueryRow(ctx, "SELECT name FROM companies WHERE id = $1", id).Scan(&name); err != nil {
		t.Fatalf("read back company: %v", err)
	}
	if name != "PT Gojek Indonesia" {
		t.Errorf("stored name = %q, want %q", name, "PT Gojek Indonesia")
	}
}

func TestUpsertCompany_ConflictReturnsSameIDAndRefreshesName(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	firstID, err := s.UpsertCompany(ctx, "PT. Gojek Indonesia", "gojek indonesia")
	if err != nil {
		t.Fatalf("first UpsertCompany() error = %v", err)
	}

	secondID, err := s.UpsertCompany(ctx, "Gojek Indonesia", "gojek indonesia")
	if err != nil {
		t.Fatalf("second UpsertCompany() error = %v", err)
	}

	if firstID != secondID {
		t.Errorf("second UpsertCompany() returned a different id: %s vs %s", secondID, firstID)
	}

	var name string
	if err := s.pool.QueryRow(ctx, "SELECT name FROM companies WHERE id = $1", firstID).Scan(&name); err != nil {
		t.Fatalf("read back company: %v", err)
	}
	if name != "Gojek Indonesia" {
		t.Errorf("stored name after conflict = %q, want the latest display name %q", name, "Gojek Indonesia")
	}

	var count int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM companies WHERE name_normalized = $1", "gojek indonesia").Scan(&count); err != nil {
		t.Fatalf("count companies: %v", err)
	}
	if count != 1 {
		t.Errorf("companies with name_normalized %q = %d, want 1 (no duplicate row)", "gojek indonesia", count)
	}
}
