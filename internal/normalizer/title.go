package normalizer

import (
	"regexp"
	"strings"
)

// modeTagRe matches a work-mode badge in parentheses, as our own scrapers
// append to location strings (e.g. "Bandung, Indonesia (Hybrid)") and as
// sometimes shows up tacked onto a job title.
var modeTagRe = regexp.MustCompile(`(?i)\(\s*(hybrid|remote|onsite)\s*\)`)

// titleSeniorityPrefixRe strips a leading seniority word — the same
// vocabulary DetectLevel classifies on — so that "Senior Backend Developer"
// and "Backend Developer" fingerprint identically; DetectLevel is what
// records the seniority distinction, not the title dedup key.
var titleSeniorityPrefixRe = regexp.MustCompile(`(?i)^(senior|junior|lead|intern|magang)\b[\s\-:.,]*`)

var titleNonAlnumRe = regexp.MustCompile(`[^a-z0-9]+`)

// NormalizeTitle produces a dedup key for a job title: lowercased, with
// the leading seniority word, any "(Hybrid)"/"(Remote)"/"(Onsite)" tag, and
// all non-alphanumeric characters (including whitespace) stripped.
func NormalizeTitle(title string) string {
	s := strings.ToLower(title)
	s = modeTagRe.ReplaceAllString(s, " ")
	s = titleSeniorityPrefixRe.ReplaceAllString(s, "")
	s = titleNonAlnumRe.ReplaceAllString(s, "")
	return s
}
