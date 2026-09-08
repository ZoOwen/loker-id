package normalizer

import "regexp"

var (
	modeHybridRe = regexp.MustCompile(`(?i)\bhybrid\b`)
	modeRemoteRe = regexp.MustCompile(`(?i)\bremote\b|\bwfh\b|work from home|kerja dari rumah`)
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

// DetectLevel classifies experience level from the job title only — never
// the description, so a level is never guessed from surrounding prose that
// merely mentions e.g. "3+ years of experience". No match means unknown,
// not a guess at "mid".
func DetectLevel(title string) ExperienceLevel {
	for _, p := range levelPatterns {
		if p.re.MatchString(title) {
			return p.level
		}
	}
	return LevelUnknown
}
