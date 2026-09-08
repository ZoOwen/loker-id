package normalizer

import (
	"regexp"
	"strconv"
	"strings"
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
//
// A listing with no hybrid/remote/onsite keyword at all but a real
// location defaults to onsite, not unknown: the overwhelming majority of
// job posts that bother stating a city and say nothing else about work
// arrangement are just ordinary in-office roles that didn't feel the need
// to say so explicitly — "unknown" is reserved for when there's truly no
// signal to go on, i.e. no location either.
func DetectMode(title, location, description string) WorkMode {
	text := title + " " + location + " " + description

	switch {
	case modeHybridRe.MatchString(text):
		return ModeHybrid
	case modeRemoteRe.MatchString(text):
		return ModeRemote
	case modeOnsiteRe.MatchString(text):
		return ModeOnsite
	case strings.TrimSpace(location) != "":
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

// yearOrPlural matches "year", "years", "year's", or "years'" — a real
// posting we saw wrote "3 year's experiences", and \s+ right after "year"
// would otherwise fail to reach the apostrophe, silently dropping the
// whole match. (?:'?s)?'? covers all four: the inner group takes an
// optional apostrophe then "s" (-> "years" or "year's"), and the trailing
// '? catches an apostrophe left over after "s" (-> "years'").
const yearOrPlural = `year(?:'?s)?'?`

// yearsOfExperiencePatterns each have exactly one capture group: the
// number of years. Checked in order — the range form must come before
// the plain form, or "3-5 years experience" would match the plain
// pattern first, from its *second* number ("5"), rather than the range
// pattern's intended lower bound ("3"). reject, when set, is checked
// against the text immediately following a match; a match is discarded
// if it matches — used to keep the standalone "N+ years" pattern (no
// adjacent "experience" required) from picking up an age requirement
// like "25+ years old" as if it were work experience.
var yearsOfExperiencePatterns = []struct {
	find   *regexp.Regexp
	reject *regexp.Regexp
}{
	{find: regexp.MustCompile(`(?i)(\d+)\s*(?:-|to)\s*\d+\+?\s*` + yearOrPlural + `\s+(?:of\s+)?experience`)},
	{find: regexp.MustCompile(`(?i)(\d+)\+?\s*` + yearOrPlural + `\s+(?:of\s+)?experience`)},
	{find: regexp.MustCompile(`(?i)experience\s*:?\s*(?:of\s+)?(?:at\s+least\s+|minimum\s+)?(\d+)\+?\s*` + yearOrPlural)},
	// Standalone "at least"/"minimum N years" — no "experience" required
	// nearby at all, e.g. "candidates should have at least 3 years in a
	// similar role".
	{find: regexp.MustCompile(`(?i)at\s+least\s+(\d+)\+?\s*` + yearOrPlural)},
	{find: regexp.MustCompile(`(?i)minimum\s+(\d+)\+?\s*` + yearOrPlural)},
	// Standalone "N+ years" — the "+" alone is a strong enough signal to
	// not require "experience" adjacent, but exclude the "N+ years old"
	// age-requirement phrasing it would otherwise also catch.
	{
		find:   regexp.MustCompile(`(?i)(\d+)\+\s*` + yearOrPlural + `\b`),
		reject: regexp.MustCompile(`(?i)^\s*old\b`),
	},
	{find: regexp.MustCompile(`(?i)minimal\s*(\d+)\s*tahun`)},
	// "min." / "min" is the common abbreviated form of "minimal" in
	// Indonesian job posts.
	{find: regexp.MustCompile(`(?i)\bmin\.?\s*(\d+)\s*tahun`)},
	{find: regexp.MustCompile(`(?i)(\d+)\s*tahun\s*pengalaman`)},
	{find: regexp.MustCompile(`(?i)pengalaman\s*(?:kerja\s*)?(?:minimal\s*)?(\d+)\s*tahun`)},
}

// yearsOfExperience extracts a stated years-of-experience figure from
// free text, if any pattern matches.
func yearsOfExperience(description string) (int, bool) {
	for _, p := range yearsOfExperiencePatterns {
		loc := p.find.FindStringSubmatchIndex(description)
		if loc == nil {
			continue
		}
		if p.reject != nil && p.reject.MatchString(description[loc[1]:]) {
			continue
		}
		n, err := strconv.Atoi(description[loc[2]:loc[3]])
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
