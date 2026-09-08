package normalizer

import (
	"regexp"
	"strconv"
)

var (
	modeHybridRe = regexp.MustCompile(`(?i)\bhybrid\b`)
	// remote(?:ly)? catches "work remotely" too, not just the noun form;
	// wfa/"work from anywhere"/"kerja jarak jauh" are real, common
	// Indonesian-tech-job phrasings the plain "remote"/"wfh" set missed.
	// Note "fully remote", "100% remote", "remote-based" etc. don't need
	// their own patterns — \bremote\b already matches "remote" as a
	// standalone word inside any of those phrases.
	modeRemoteRe = regexp.MustCompile(`(?i)\bremote(?:ly)?\b|\bwfh\b|\bwfa\b|work from home|work from anywhere|kerja dari rumah|kerja jarak jauh`)
	modeOnsiteRe = regexp.MustCompile(`(?i)\bon[\s-]?site\b|\bwfo\b|work from office|kerja di kantor`)
)

// DetectMode classifies work mode from title, location and description
// text. Hybrid is checked first: a listing that mentions both "hybrid" and
// e.g. "2 days WFO" should read as hybrid, not onsite.
func DetectMode(title, location, description string) WorkMode {
	text := title + " " + location + " " + description

	switch {
	case modeHybridRe.MatchString(text):
		return ModeHybrid
	case modeRemoteRe.MatchString(text):
		return ModeRemote
	case modeOnsiteRe.MatchString(text):
		return ModeOnsite
	default:
		return ModeUnknown
	}
}

// levelPatterns is checked in order, highest seniority first, so a title
// naming more than one level (e.g. "Senior Lead Architect") resolves to
// the higher one.
var levelPatterns = []struct {
	level ExperienceLevel
	re    *regexp.Regexp
}{
	{LevelLead, regexp.MustCompile(`(?i)\blead\b`)},
	{LevelSenior, regexp.MustCompile(`(?i)\bsenior\b`)},
	{LevelJunior, regexp.MustCompile(`(?i)\bjunior\b`)},
	{LevelIntern, regexp.MustCompile(`(?i)\bintern(?:ship)?\b|\bmagang\b`)},
}

// yearsOfExperiencePatterns each have exactly one capture group: the
// number of years. Checked in order — the range form must come before
// the plain form, or "3-5 years experience" would match the plain
// pattern first, from its *second* number ("5"), rather than the range
// pattern's intended lower bound ("3").
var yearsOfExperiencePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(\d+)\s*(?:-|to)\s*\d+\+?\s*years?\s+(?:of\s+)?experience`),
	regexp.MustCompile(`(?i)(\d+)\+?\s*years?\s+(?:of\s+)?experience`),
	regexp.MustCompile(`(?i)experience\s*:?\s*(?:of\s+)?(?:at\s+least\s+|minimum\s+)?(\d+)\+?\s*years?`),
	regexp.MustCompile(`(?i)minimal\s*(\d+)\s*tahun`),
	regexp.MustCompile(`(?i)(\d+)\s*tahun\s*pengalaman`),
	regexp.MustCompile(`(?i)pengalaman\s*(?:kerja\s*)?(?:minimal\s*)?(\d+)\s*tahun`),
}

// yearsOfExperience extracts a stated years-of-experience figure from
// free text, if any pattern matches.
func yearsOfExperience(description string) (int, bool) {
	for _, re := range yearsOfExperiencePatterns {
		m := re.FindStringSubmatch(description)
		if m == nil {
			continue
		}
		n, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		return n, true
	}
	return 0, false
}

// levelFromYears bands a years-of-experience figure into a level, per
// spec: 0-<2y junior, 2-<5y mid, 5y+ senior.
func levelFromYears(years int) ExperienceLevel {
	switch {
	case years < 2:
		return LevelJunior
	case years < 5:
		return LevelMid
	default:
		return LevelSenior
	}
}

// DetectLevel classifies experience level, primarily from an explicit
// seniority word in the title. When the title has none, it falls back to
// a stated years-of-experience figure in the description (see
// yearsOfExperiencePatterns and levelFromYears) — a fuzzier signal than
// an explicit title keyword (years-of-experience bands are an imprecise
// proxy for a level), but leaving every job with no title keyword at
// "unknown" makes level-based filtering useless: a lower-confidence guess
// beats no signal at all.
func DetectLevel(title, description string) ExperienceLevel {
	for _, p := range levelPatterns {
		if p.re.MatchString(title) {
			return p.level
		}
	}
	if years, ok := yearsOfExperience(description); ok {
		return levelFromYears(years)
	}
	return LevelUnknown
}
