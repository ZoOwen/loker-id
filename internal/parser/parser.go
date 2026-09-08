// Package parser turns free-text salary strings from Indonesian job
// listings into structured Salary values.
package parser

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

type SalaryConfidence string

const (
	ConfExact     SalaryConfidence = "exact"
	ConfRange     SalaryConfidence = "range"
	ConfEstimated SalaryConfidence = "estimated"
	ConfUnknown   SalaryConfidence = "unknown"
)

type Salary struct {
	Min, Max int64
	Conf     SalaryConfidence
	Raw      string
}

type modifier int

const (
	modNone modifier = iota
	modMin
	modMax
)

const (
	unitWords = `(jt|juta|jutaan|rb|ribu|ribuan)`
	sepWords  = `-|–|—|~|s/d|s\.d\.?|sampai(?:\s+dengan)?|hingga`
)

var (
	vagueRe   = regexp.MustCompile(`(?i)\b(negotiable|competitive|nego|kompetitif|undisclosed|confidential|tbd)\b`)
	foreignRe = regexp.MustCompile(`(?i)[$€£]|\b(usd|sgd|myr|eur|gbp)\b`)

	currencyMarkerRe = regexp.MustCompile(`(?i)\b(rp|idr)\.?\s*`)
	periodSuffixRe   = regexp.MustCompile(`(?i)/\s*(bulan|bln|month)\b|\bper\s*(bulan|month)\b`)

	rangeRe  = regexp.MustCompile(`(?i)(\d[\d.,]*)\s*` + unitWords + `?\s*(?:` + sepWords + `)\s*(\d[\d.,]*)\s*` + unitWords + `?`)
	singleRe = regexp.MustCompile(`(?i)(\d[\d.,]*)\s*` + unitWords + `?`)

	maxWordRe = regexp.MustCompile(`(?i)\b(up to|hingga|maksimal|maks|max|sampai(?:\s+dengan)?)\b`)
	minWordRe = regexp.MustCompile(`(?i)\b(mulai dari|minimal|min|dari|mulai|start from|from)\b`)

	numSepRe = regexp.MustCompile(`[.,]`)
)

// ParseSalary extracts a structured Salary from a free-text salary string.
//
// Pipeline: normalize -> detect currency/vagueness -> strip noise ->
// extract numbers -> decide range vs single.
func ParseSalary(raw string) Salary {
	result := Salary{Raw: raw}

	normalized := normalize(raw)
	if normalized == "" {
		result.Conf = ConfUnknown
		return result
	}

	if vagueRe.MatchString(normalized) || foreignRe.MatchString(normalized) {
		result.Conf = ConfUnknown
		return result
	}

	cleaned := stripNoise(normalized)

	if min, max, ok := extractRange(cleaned); ok {
		result.Min, result.Max, result.Conf = min, max, ConfRange
		return result
	}

	amount, mod, ok := extractSingle(cleaned)
	if !ok {
		result.Conf = ConfUnknown
		return result
	}

	switch mod {
	case modMax:
		result.Max, result.Conf = amount, ConfEstimated
	case modMin:
		result.Min, result.Conf = amount, ConfEstimated
	default:
		result.Min, result.Max, result.Conf = amount, amount, ConfExact
	}
	return result
}

func normalize(raw string) string {
	return strings.TrimSpace(raw)
}

// stripNoise removes currency markers (Rp/IDR) and time-period suffixes
// (/bulan, per month, ...) that carry no numeric information, so the
// range/single extractors only ever see numbers, units and separators.
func stripNoise(s string) string {
	s = currencyMarkerRe.ReplaceAllString(s, "")
	s = periodSuffixRe.ReplaceAllString(s, "")
	return s
}

// extractRange looks for two numbers joined by a range separator (dash,
// en/em-dash, tilde, "s/d", "sampai", "hingga"). A unit suffix attached to
// only one side is shared with the other side, e.g. "8-12jt" means 8jt-12jt.
func extractRange(s string) (min, max int64, ok bool) {
	m := rangeRe.FindStringSubmatch(s)
	if m == nil {
		return 0, 0, false
	}

	num1, unit1, num2, unit2 := m[1], m[2], m[3], m[4]
	if unit1 == "" {
		unit1 = unit2
	}
	if unit2 == "" {
		unit2 = unit1
	}

	a, ok1 := parseAmountToken(num1, unit1)
	b, ok2 := parseAmountToken(num2, unit2)
	if !ok1 || !ok2 {
		return 0, 0, false
	}

	if a > b {
		a, b = b, a
	}
	return a, b, true
}

// extractSingle looks for one number and classifies it using the words
// preceding it: "up to"/"hingga"/... means max-only, "mulai dari"/"min"/...
// means min-only, otherwise it's treated as an exact figure.
func extractSingle(s string) (amount int64, mod modifier, ok bool) {
	loc := singleRe.FindStringSubmatchIndex(s)
	if loc == nil {
		return 0, modNone, false
	}

	numStr := s[loc[2]:loc[3]]
	var unit string
	if loc[4] != -1 {
		unit = s[loc[4]:loc[5]]
	}

	amt, ok := parseAmountToken(numStr, unit)
	if !ok {
		return 0, modNone, false
	}

	prefix := s[:loc[0]]
	switch {
	case maxWordRe.MatchString(prefix):
		return amt, modMax, true
	case minWordRe.MatchString(prefix):
		return amt, modMin, true
	default:
		return amt, modNone, true
	}
}

// parseAmountToken parses a numeric string plus optional unit word
// (jt/juta/jutaan/rb/ribu/ribuan) into a rupiah amount.
func parseAmountToken(numStr, unit string) (int64, bool) {
	multiplier := 1.0
	switch strings.ToLower(unit) {
	case "jt", "juta", "jutaan":
		multiplier = 1_000_000
	case "rb", "ribu", "ribuan":
		multiplier = 1_000
	}

	value, ok := parseNumericCore(numStr, multiplier > 1)
	if !ok {
		return 0, false
	}
	return int64(math.Round(value * multiplier)), true
}

// parseNumericCore parses a digit string that uses "." or "," as a
// thousands separator (Indonesian style: "10.000.000" = ten million). When
// allowDecimal is true (a unit word like "jt" follows, so the raw number is
// small), a lone trailing group of 1-2 digits is instead treated as a
// decimal fraction ("6,5jt" = 6.5 juta), since real thousands groups are
// always exactly 3 digits.
func parseNumericCore(s string, allowDecimal bool) (float64, bool) {
	s = strings.TrimRight(s, ".,")
	groups := numSepRe.Split(s, -1)

	for _, g := range groups {
		if g == "" {
			return 0, false
		}
	}

	if len(groups) == 1 {
		v, err := strconv.ParseFloat(groups[0], 64)
		return v, err == nil
	}

	last := groups[len(groups)-1]
	if allowDecimal && len(groups) == 2 && len(last) <= 2 {
		v, err := strconv.ParseFloat(groups[0]+"."+last, 64)
		return v, err == nil
	}

	v, err := strconv.ParseFloat(strings.Join(groups, ""), 64)
	return v, err == nil
}
