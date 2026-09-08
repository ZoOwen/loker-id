// Package normalizer turns a scraper.RawJob into an insert-ready row:
// company/title dedup keys, a curated tech-stack extraction, work mode and
// experience level classification, and a dedup fingerprint. Pure
// functions only — nothing here touches the database.
package normalizer

import (
	"strings"

	"github.com/ZoOwen/loker-id/internal/parser"
	"github.com/ZoOwen/loker-id/internal/scraper"
)

// WorkMode mirrors the work_mode enum in migrations/001_init.sql.
type WorkMode string

const (
	ModeOnsite  WorkMode = "onsite"
	ModeRemote  WorkMode = "remote"
	ModeHybrid  WorkMode = "hybrid"
	ModeUnknown WorkMode = "unknown"
)

// ExperienceLevel mirrors the experience_level enum in
// migrations/001_init.sql. LevelMid exists for schema completeness —
// DetectLevel never returns it, since nothing in a title reliably implies
// "mid" the way "senior"/"junior"/"lead"/"intern" do.
type ExperienceLevel string

const (
	LevelIntern  ExperienceLevel = "intern"
	LevelJunior  ExperienceLevel = "junior"
	LevelMid     ExperienceLevel = "mid"
	LevelSenior  ExperienceLevel = "senior"
	LevelLead    ExperienceLevel = "lead"
	LevelUnknown ExperienceLevel = "unknown"
)

// NormalizedJob is a RawJob transformed into an insert-ready row.
type NormalizedJob struct {
	Title, TitleNormalized         string
	CompanyName, CompanyNormalized string
	Description                    string
	Salary                         parser.Salary
	Stack                          []string
	Mode                           WorkMode
	Level                          ExperienceLevel
	LocationCity                   string
	Fingerprint                    string
}

// Normalize converts a raw scraped job posting into a NormalizedJob.
func Normalize(raw scraper.RawJob) NormalizedJob {
	titleNorm := NormalizeTitle(raw.Title)
	companyNorm := NormalizeCompany(raw.Company)
	// Description is HTML straight from the source (Kalibrr's rich-text
	// job posts are full of <p>/<li>/class="..." markup). Clean it once,
	// up front, and derive everything downstream from the clean version:
	// storing raw HTML bloats the API response, and — separately —
	// leaving it in place for ExtractStack risks an inline tag splitting
	// a tech name mid-word (e.g. "Doc<b>ker</b>") so it never matches at
	// all. Both problems share the one fix.
	description := SanitizeDescription(raw.Description)

	return NormalizedJob{
		Title:             strings.TrimSpace(raw.Title),
		TitleNormalized:   titleNorm,
		CompanyName:       strings.TrimSpace(raw.Company),
		CompanyNormalized: companyNorm,
		Description:       description,
		Salary:            parser.ParseSalary(raw.SalaryRaw),
		Stack:             ExtractStack(raw.Title, description),
		Mode:              DetectMode(raw.Title, raw.Location, description),
		Level:             DetectLevel(raw.Title, description),
		LocationCity:      extractCity(raw.Location),
		Fingerprint:       Fingerprint(companyNorm, titleNorm),
	}
}

// extractCity pulls just the city out of a location string shaped like
// "Jakarta, Indonesia" or "Bandung, Indonesia (Hybrid)" (the format our own
// scrapers produce): everything before the first comma, with any trailing
// mode tag stripped if there was no comma to begin with.
func extractCity(location string) string {
	city := location
	if idx := strings.Index(city, ","); idx >= 0 {
		city = city[:idx]
	}
	city = modeTagRe.ReplaceAllString(city, "")
	return strings.TrimSpace(city)
}
