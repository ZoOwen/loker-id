package normalizer

import "testing"

func TestDetectMode(t *testing.T) {
	tests := []struct {
		name        string
		title       string
		location    string
		description string
		want        WorkMode
	}{
		{"a clear location with no remote/hybrid/onsite keyword anywhere defaults to onsite", "Backend Developer", "Jakarta, Indonesia", "", ModeOnsite},
		{"no location and no keyword anywhere is the only case left as unknown", "Backend Developer", "", "", ModeUnknown},
		{"whitespace-only location counts as no location", "Backend Developer", "   ", "", ModeUnknown},
		{"hybrid tag in location, as our own scraper produces it", "Software Engineer", "Bandung, Indonesia (Hybrid)", "", ModeHybrid},
		{"remote tag in location", "Software Engineer", "Jakarta, Indonesia (Remote)", "", ModeRemote},
		{"wfh keyword in description", "Software Engineer", "Jakarta, Indonesia", "Kandidat wajib WFH dari rumah", ModeRemote},
		{"onsite keyword in description", "Software Engineer", "Jakarta, Indonesia", "Bekerja onsite di kantor pusat", ModeOnsite},
		{"on-site with dash", "Software Engineer", "Jakarta, Indonesia", "Posisi ini on-site di Jakarta", ModeOnsite},
		{"hybrid wins over remote and onsite mentions", "Backend Engineer", "Remote (Indonesia)", "Hybrid, 2 hari WFO", ModeHybrid},
		{"remote in title", "Backend Developer (Remote)", "", "", ModeRemote},
		{"empty everything", "", "", "", ModeUnknown},
		{"WFA (work from anywhere) in description", "Backend Engineer", "Jakarta, Indonesia", "Sistem kerja WFA, bebas dari mana saja", ModeRemote},
		{"fully remote phrase", "Backend Engineer", "", "This is a fully remote position", ModeRemote},
		{"100% remote phrase", "Backend Engineer", "", "100% remote team, work from anywhere", ModeRemote},
		{"remote-based phrase", "Backend Engineer", "", "Remote-based role, async first", ModeRemote},
		{"kerja jarak jauh (Indonesian for remote work)", "Backend Engineer", "", "Sistem kerja jarak jauh diterapkan penuh", ModeRemote},
		{"description content alone, with no location and no mode keyword, still stays unknown", "Backend Developer", "", "Great benefits and a fun team culture.", ModeUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DetectMode(tt.title, tt.location, tt.description); got != tt.want {
				t.Errorf("DetectMode(%q, %q, %q) = %q, want %q", tt.title, tt.location, tt.description, got, tt.want)
			}
		})
	}
}

func TestDetectLevel(t *testing.T) {
	tests := []struct {
		name        string
		title       string
		description string
		want        ExperienceLevel
	}{
		{"senior", "Senior Backend Engineer", "", LevelSenior},
		{"junior", "Junior Data Analyst", "", LevelJunior},
		{"lead", "Lead Product Manager", "", LevelLead},
		{"intern english", "Internship - Software Engineering", "", LevelIntern},
		{"magang indonesian", "Program Magang Backend Developer", "", LevelIntern},
		{"no keyword anywhere defaults to unknown", "Backend Engineer", "Great team, nice office.", LevelUnknown},
		{"lead takes priority when both lead and senior appear", "Senior Lead Architect", "", LevelLead},
		{"mid is never guessed from the title alone", "Mid-level Backend Engineer", "", LevelUnknown},
		{"empty title and description", "", "", LevelUnknown},

		// Fallback: title has no seniority keyword, so years-of-experience
		// in the description decides it. Title keywords still always win
		// when present — these fallback cases all have a "clean" title.
		{"0 years -> junior", "Backend Engineer", "Fresh graduates welcome, 0 years experience required.", LevelJunior},
		{"1 year -> junior", "Backend Engineer", "Requires 1 year of experience in Go.", LevelJunior},
		{"2 years -> mid (upper bound of junior band is exclusive)", "Backend Engineer", "Requires 2 years of experience.", LevelMid},
		{"3 years -> mid", "Backend Engineer", "Minimum 3 years experience with distributed systems.", LevelMid},
		{"4 years -> mid", "Backend Engineer", "4+ years of experience preferred.", LevelMid},
		{"5 years -> senior (upper bound of mid band is exclusive)", "Backend Engineer", "At least 5 years of experience required.", LevelSenior},
		{"8 years -> senior", "Backend Engineer", "8 years of experience in backend development.", LevelSenior},
		{"range takes the lower bound: 3-5 years -> mid", "Backend Engineer", "3-5 years of experience needed.", LevelMid},
		{"range takes the lower bound: 3 to 5 years -> mid", "Backend Engineer", "3 to 5 years of experience needed.", LevelMid},
		{"indonesian: minimal N tahun -> mid", "Backend Engineer", "Minimal 3 tahun pengalaman di bidang yang relevan.", LevelMid},
		{"indonesian: N tahun pengalaman -> senior", "Backend Engineer", "Memiliki 6 tahun pengalaman sebagai software engineer.", LevelSenior},
		{"indonesian: pengalaman kerja minimal N tahun -> junior", "Backend Engineer", "Pengalaman kerja minimal 1 tahun di posisi serupa.", LevelJunior},
		{"title keyword wins over a conflicting description years hint", "Senior Backend Engineer", "1 year of experience required.", LevelSenior},
		{"no years pattern in description at all stays unknown", "Backend Engineer", "We are a fast-growing startup looking for a great engineer.", LevelUnknown},
		{"reversed phrasing: experience minimum N years -> senior", "Backend Engineer", "Experience: minimum 5 years in backend development.", LevelSenior},

		// A real posting's exact wording: apostrophe in "year's", plural
		// "experiences", and "More than" preceding the number.
		{"real-world case: More than N year's experiences (apostrophe) -> mid", "Backend Engineer", "More than 3 year's experiences in backend development required.", LevelMid},
		{"years' (apostrophe after s) also tolerated", "Backend Engineer", "More than 6 years' experience in software engineering.", LevelSenior},
		{"standalone at least N years, no adjacent \"experience\" word -> mid", "Backend Engineer", "Candidates should have at least 3 years in a similar role.", LevelMid},
		{"standalone minimum N years, no adjacent \"experience\" word -> junior", "Backend Engineer", "Minimum 1 year in a backend role.", LevelJunior},
		{"standalone N+ years with no \"experience\" nearby at all -> senior", "Backend Engineer", "5+ years building distributed systems.", LevelSenior},
		{"N+ years old (age, not experience) must NOT be picked up", "Backend Engineer", "Candidates must be 25+ years old to apply for this role.", LevelUnknown},
		{"indonesian abbreviated: min. N tahun (with period) -> mid", "Backend Engineer", "Min. 3 tahun pengalaman di bidang terkait.", LevelMid},
		{"indonesian abbreviated: min N tahun (no period) -> junior", "Backend Engineer", "Min 1 tahun pengalaman sebagai backend developer.", LevelJunior},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DetectLevel(tt.title, tt.description); got != tt.want {
				t.Errorf("DetectLevel(%q, %q) = %q, want %q", tt.title, tt.description, got, tt.want)
			}
		})
	}
}
