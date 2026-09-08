package normalizer

import (
	"regexp"
	"strings"
)

var (
	companyPerseroRe = regexp.MustCompile(`(?i)\(\s*persero\s*\)`)
	companyTbkRe     = regexp.MustCompile(`(?i)\btbk\b`)
	companyPunctRe   = regexp.MustCompile(`[.,]`)
	// Anchored prefix match, not a bare "pt"/"cv" substring — otherwise
	// "CVS Health" or "PTS Corp" would lose their first two letters.
	companyPrefixRe = regexp.MustCompile(`(?i)^\s*(pt|cv)\b\s*`)
	companySpaceRe  = regexp.MustCompile(`\s+`)
)

// NormalizeCompany produces a dedup key for a company name: lowercased,
// with the "PT"/"CV" legal-form prefix, ", Tbk" suffix, "(Persero)" tag,
// periods and extra whitespace all stripped. "PT. Gojek Indonesia" and
// "Gojek Indonesia" normalize to the same string.
func NormalizeCompany(name string) string {
	s := strings.ToLower(name)
	s = companyPerseroRe.ReplaceAllString(s, " ")
	s = companyTbkRe.ReplaceAllString(s, " ")
	s = companyPunctRe.ReplaceAllString(s, "")
	s = companyPrefixRe.ReplaceAllString(s, "")
	s = companySpaceRe.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}
