package normalizer

import (
	"reflect"
	"strings"
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
	if got.Description != raw.Description {
		t.Errorf("Description = %q, want %q (already plain text, so sanitizing is a no-op here)", got.Description, raw.Description)
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

// TestNormalize_SanitizesHTMLDescription is the end-to-end version of the
// bug 1 -> bug 4 chain: Normalize must store cleaned text (not raw HTML)
// in Description, and ExtractStack/DetectMode/DetectLevel must all see
// that same cleaned text rather than raw markup.
func TestNormalize_SanitizesHTMLDescription(t *testing.T) {
	raw := scraper.RawJob{
		Title:    "DevOps Engineer",
		Company:  "Acme",
		Location: "Jakarta, Indonesia",
		Description: "<ul>" +
			"<li>Manage <b>Doc</b><b>ker</b> containers and Kubernetes clusters</li>" +
			"<li>3 years of experience with Jenkins and Ansible</li>" +
			"<li class=\"text-justify\">Remote-friendly team, fully remote position</li>" +
			"</ul>",
	}

	got := Normalize(raw)

	if strings.Contains(got.Description, "<") || strings.Contains(got.Description, "class=") {
		t.Errorf("Description still contains raw HTML: %q", got.Description)
	}
	if !strings.HasPrefix(got.Description, "- Manage Docker containers") {
		t.Errorf("Description = %q, want it to start with the cleaned first bullet (with Doc+ker rejoined into Docker)", got.Description)
	}

	wantStack := []string{"Ansible", "Docker", "Jenkins", "Kubernetes"}
	if !reflect.DeepEqual(got.Stack, wantStack) {
		t.Errorf("Stack = %v, want %v (extracted from the cleaned description, including the tag-split \"Docker\")", got.Stack, wantStack)
	}

	if got.Mode != ModeRemote {
		t.Errorf("Mode = %q, want %q (detected from \"fully remote\" in the cleaned description)", got.Mode, ModeRemote)
	}

	if got.Level != LevelMid {
		t.Errorf("Level = %q, want %q (fallback from \"3 years of experience\" in the cleaned description, no title keyword present)", got.Level, LevelMid)
	}
}
