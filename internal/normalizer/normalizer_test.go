package normalizer

import (
	"reflect"
	"testing"

	"github.com/ZoOwen/loker-id/internal/parser"
	"github.com/ZoOwen/loker-id/internal/scraper"
)

func TestNormalize(t *testing.T) {
	raw := scraper.RawJob{
		Title:       "Senior Backend Engineer (Hybrid)",
		Company:     "PT. Gojek Indonesia",
		SalaryRaw:   "IDR 15000000 - 25000000/month",
		Location:    "Jakarta, Indonesia (Hybrid)",
		Description: "Kami mencari Backend Engineer berpengalaman dengan Golang, PostgreSQL, dan Docker.",
		SourceURL:   "https://example.com/jobs/1",
		SourceJobID: "1",
	}

	got := Normalize(raw)

	if got.Title != raw.Title {
		t.Errorf("Title = %q, want raw title %q unchanged", got.Title, raw.Title)
	}
	if got.CompanyName != raw.Company {
		t.Errorf("CompanyName = %q, want raw company %q unchanged", got.CompanyName, raw.Company)
	}

	wantTitleNorm := NormalizeTitle(raw.Title)
	if got.TitleNormalized != wantTitleNorm {
		t.Errorf("TitleNormalized = %q, want %q", got.TitleNormalized, wantTitleNorm)
	}

	wantCompanyNorm := NormalizeCompany(raw.Company)
	if got.CompanyNormalized != wantCompanyNorm {
		t.Errorf("CompanyNormalized = %q, want %q", got.CompanyNormalized, wantCompanyNorm)
	}

	if got.Salary.Conf != parser.ConfRange || got.Salary.Min != 15_000_000 || got.Salary.Max != 25_000_000 {
		t.Errorf("Salary = %+v, want Min=15000000 Max=25000000 Conf=range", got.Salary)
	}

	wantStack := []string{"Docker", "Go", "PostgreSQL"}
	if !reflect.DeepEqual(got.Stack, wantStack) {
		t.Errorf("Stack = %v, want %v", got.Stack, wantStack)
	}

	if got.Mode != ModeHybrid {
		t.Errorf("Mode = %q, want %q", got.Mode, ModeHybrid)
	}
	if got.Level != LevelSenior {
		t.Errorf("Level = %q, want %q", got.Level, LevelSenior)
	}
	if got.LocationCity != "Jakarta" {
		t.Errorf("LocationCity = %q, want %q", got.LocationCity, "Jakarta")
	}

	wantFingerprint := Fingerprint(wantCompanyNorm, wantTitleNorm)
	if got.Fingerprint != wantFingerprint {
		t.Errorf("Fingerprint = %q, want %q", got.Fingerprint, wantFingerprint)
	}
}

func TestNormalize_NoSignals(t *testing.T) {
	raw := scraper.RawJob{
		Title:   "Backend Engineer",
		Company: "Acme",
	}

	got := Normalize(raw)

	if got.Mode != ModeUnknown {
		t.Errorf("Mode = %q, want %q", got.Mode, ModeUnknown)
	}
	if got.Level != LevelUnknown {
		t.Errorf("Level = %q, want %q", got.Level, LevelUnknown)
	}
	if len(got.Stack) != 0 {
		t.Errorf("Stack = %v, want empty", got.Stack)
	}
	if got.Salary.Conf != parser.ConfUnknown {
		t.Errorf("Salary.Conf = %q, want %q", got.Salary.Conf, parser.ConfUnknown)
	}
	if got.LocationCity != "" {
		t.Errorf("LocationCity = %q, want empty", got.LocationCity)
	}
}
