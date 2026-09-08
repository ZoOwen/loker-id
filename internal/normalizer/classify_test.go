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
		{"no signal at all", "Backend Developer", "Jakarta, Indonesia", "", ModeUnknown},
		{"hybrid tag in location, as our own scraper produces it", "Software Engineer", "Bandung, Indonesia (Hybrid)", "", ModeHybrid},
		{"remote tag in location", "Software Engineer", "Jakarta, Indonesia (Remote)", "", ModeRemote},
		{"wfh keyword in description", "Software Engineer", "Jakarta, Indonesia", "Kandidat wajib WFH dari rumah", ModeRemote},
		{"onsite keyword in description", "Software Engineer", "Jakarta, Indonesia", "Bekerja onsite di kantor pusat", ModeOnsite},
		{"on-site with dash", "Software Engineer", "Jakarta, Indonesia", "Posisi ini on-site di Jakarta", ModeOnsite},
		{"hybrid wins over remote and onsite mentions", "Backend Engineer", "Remote (Indonesia)", "Hybrid, 2 hari WFO", ModeHybrid},
		{"remote in title", "Backend Developer (Remote)", "", "", ModeRemote},
		{"empty everything", "", "", "", ModeUnknown},
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
		name  string
		title string
		want  ExperienceLevel
	}{
		{"senior", "Senior Backend Engineer", LevelSenior},
		{"junior", "Junior Data Analyst", LevelJunior},
		{"lead", "Lead Product Manager", LevelLead},
		{"intern english", "Internship - Software Engineering", LevelIntern},
		{"magang indonesian", "Program Magang Backend Developer", LevelIntern},
		{"no keyword at all defaults to unknown", "Backend Engineer", LevelUnknown},
		{"lead takes priority when both lead and senior appear", "Senior Lead Architect", LevelLead},
		{"mid is never guessed even when title implies it", "Mid-level Backend Engineer", LevelUnknown},
		{"empty title", "", LevelUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DetectLevel(tt.title); got != tt.want {
				t.Errorf("DetectLevel(%q) = %q, want %q", tt.title, got, tt.want)
			}
		})
	}
}
