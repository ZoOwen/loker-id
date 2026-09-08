package normalizer

import "testing"

func TestNormalizeTitle(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain title lowercased and squashed", "Backend Developer", "backenddeveloper"},
		{"senior prefix stripped", "Senior Backend Developer", "backenddeveloper"},
		{"junior prefix stripped", "Junior Data Analyst", "dataanalyst"},
		{"lead prefix stripped", "Lead Product Manager", "productmanager"},
		{"intern prefix stripped", "Intern Software Engineer", "softwareengineer"},
		{"magang prefix stripped", "Magang Backend Developer", "backenddeveloper"},
		{"remote tag stripped", "Backend Developer (Remote)", "backenddeveloper"},
		{"hybrid tag stripped", "Backend Developer (Hybrid)", "backenddeveloper"},
		{"onsite tag stripped", "Full Stack Developer (Onsite)", "fullstackdeveloper"},
		{"seniority and mode tag both stripped", "Senior Backend Developer (Hybrid)", "backenddeveloper"},
		{"dash and punctuation removed", "Software Engineer - Go", "softwareengineergo"},
		{"digits kept", "Data Engineer II", "dataengineerii"},
		{"seniority word only stripped as a prefix, not mid-title", "Backend Developer for Senior Management", "backenddeveloperforseniormanagement"},
		{"empty string", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeTitle(tt.in); got != tt.want {
				t.Errorf("NormalizeTitle(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestNormalizeTitle_SeniorityIgnoredForFingerprint(t *testing.T) {
	a := NormalizeTitle("Senior Backend Developer (Hybrid)")
	b := NormalizeTitle("Backend Developer")
	if a != b {
		t.Errorf("NormalizeTitle with seniority/mode noise = %q, plain = %q, want equal", a, b)
	}
}
