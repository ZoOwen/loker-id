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

	return NormalizedJob{
		Title:             strings.TrimSpace(raw.Title),
		TitleNormalized:   titleNorm,
		CompanyName:       strings.TrimSpace(raw.Company),
		CompanyNormalized: companyNorm,
		Salary:            parser.ParseSalary(raw.SalaryRaw),
		Stack:             ExtractStack(raw.Title, raw.Description),
		Mode:              DetectMode(raw.Title, raw.Location, raw.Description),
		Level:             DetectLevel(raw.Title),
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
