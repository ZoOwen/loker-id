package normalizer

import "testing"

func TestNormalizeCompany(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"PT prefix with space", "PT Gojek Indonesia", "gojek indonesia"},
		{"PT. prefix with period", "PT. Gojek Indonesia", "gojek indonesia"},
		{"no prefix at all", "Gojek Indonesia", "gojek indonesia"},
		{"CV prefix", "CV Maju Jaya", "maju jaya"},
		{"comma Tbk suffix", "PT Telkom Indonesia, Tbk", "telkom indonesia"},
		{"space Tbk suffix no comma", "PT Telkom Indonesia Tbk", "telkom indonesia"},
		{"Persero suffix", "PT Kereta Api Indonesia (Persero)", "kereta api indonesia"},
		{"Persero and Tbk and periods combined", "PT. Bank Rakyat Indonesia (Persero), Tbk.", "bank rakyat indonesia"},
		{"extra internal and outer whitespace", "  PT   Contoh   Aja  ", "contoh aja"},
		{"CVS Health must not lose CV as a false prefix match", "CVS Health", "cvs health"},
		{"PTS Corp must not lose PT as a false prefix match", "PTS Corp", "pts corp"},
		{"empty string", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeCompany(tt.in); got != tt.want {
				t.Errorf("NormalizeCompany(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestNormalizeCompany_PTAndPlainFormsMatch(t *testing.T) {
	a := NormalizeCompany("PT. Gojek Indonesia")
	b := NormalizeCompany("Gojek Indonesia")
	if a != b {
		t.Errorf("NormalizeCompany(%q) = %q, NormalizeCompany(%q) = %q, want equal", "PT. Gojek Indonesia", a, "Gojek Indonesia", b)
	}
}
