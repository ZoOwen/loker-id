package parser

import "testing"

func TestParseSalary(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		wantMin  int64
		wantMax  int64
		wantConf SalaryConfidence
	}{
		{"range with dash, shared jt suffix", "8-12jt", 8_000_000, 12_000_000, ConfRange},
		{"range with juta word", "8-10 juta", 8_000_000, 10_000_000, ConfRange},
		{"exact with Rp and space", "Rp 10.000.000", 10_000_000, 10_000_000, ConfExact},
		{"exact with Rp no space", "Rp10.000.000", 10_000_000, 10_000_000, ConfExact},
		{"exact with Rp dot abbreviation", "Rp. 7.500.000", 7_500_000, 7_500_000, ConfExact},
		{"range IDR comma thousands", "IDR 8,000,000 - 12,000,000", 8_000_000, 12_000_000, ConfRange},
		{"range IDR labeled both sides", "IDR 8.000.000 - IDR 12.000.000", 8_000_000, 12_000_000, ConfRange},
		{"up to juta", "up to 15 juta", 0, 15_000_000, ConfEstimated},
		{"hingga juta", "hingga 20 juta", 0, 20_000_000, ConfEstimated},
		{"maksimal jt", "maksimal 7jt", 0, 7_000_000, ConfEstimated},
		{"maks decimal jt", "maks 6.5jt", 0, 6_500_000, ConfEstimated},
		{"mulai dari jt", "mulai dari 8jt", 8_000_000, 0, ConfEstimated},
		{"minimal juta", "minimal 5 juta", 5_000_000, 0, ConfEstimated},
		{"dari jt", "dari 4jt", 4_000_000, 0, ConfEstimated},
		{"range jt with slash bulan suffix", "8jt - 12jt/bulan", 8_000_000, 12_000_000, ConfRange},
		{"range en-dash Rp both sides", "Rp8.000.000 – Rp12.000.000", 8_000_000, 12_000_000, ConfRange},
		{"range em-dash Rp both sides spaced", "Rp 8.000.000 — Rp 12.000.000", 8_000_000, 12_000_000, ConfRange},
		{"range tilde separator", "8jt ~ 10jt", 8_000_000, 10_000_000, ConfRange},
		{"negotiable", "negotiable", 0, 0, ConfUnknown},
		{"competitive", "competitive", 0, 0, ConfUnknown},
		{"empty string", "", 0, 0, ConfUnknown},
		{"dollar sign is not idr", "$3000", 0, 0, ConfUnknown},
		{"usd word range is not idr", "USD 500 - 800", 0, 0, ConfUnknown},
		{"range dot thousands trailing IDR label", "8.000.000 - 12.000.000 IDR", 8_000_000, 12_000_000, ConfRange},
		{"exact with leading label text", "Gaji: Rp 5.000.000", 5_000_000, 5_000_000, ConfExact},
		{"range no spaces around dash", "Rp3jt-5jt", 3_000_000, 5_000_000, ConfRange},
		{"range shared suffix with slash bulan", "3-5jt/bulan", 3_000_000, 5_000_000, ConfRange},
		{"exact with slash bulan suffix", "Rp 15.000.000/bulan", 15_000_000, 15_000_000, ConfExact},
		{"nego indonesian slang", "gaji nego", 0, 0, ConfUnknown},
		{"kompetitif indonesian", "Gaji kompetitif", 0, 0, ConfUnknown},
		{"tbd placeholder", "TBD", 0, 0, ConfUnknown},
		{"lone dash placeholder", "-", 0, 0, ConfUnknown},
		{"range s/d abbreviation", "8jt s/d 12jt", 8_000_000, 12_000_000, ConfRange},
		{"range sampai word", "8 sampai 12 juta", 8_000_000, 12_000_000, ConfRange},
		{"exact IDR prefix with slash bulan", "IDR 10.000.000/bulan", 10_000_000, 10_000_000, ConfExact},
		{"range with per bulan phrase", "10-15jt per bulan", 10_000_000, 15_000_000, ConfRange},
		{"exact plain grouped thousands", "Rp 12.500.000", 12_500_000, 12_500_000, ConfExact},
		{"exact decimal comma juta", "7,5 juta", 7_500_000, 7_500_000, ConfExact},
		{"exact decimal dot jt", "12.5jt", 12_500_000, 12_500_000, ConfExact},
		{"no digits, umr reference", "sesuai UMR", 0, 0, ConfUnknown},
		{"no digits, experience based", "Gaji sesuai pengalaman", 0, 0, ConfUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseSalary(tt.raw)

			if got.Min != tt.wantMin || got.Max != tt.wantMax || got.Conf != tt.wantConf {
				t.Errorf("ParseSalary(%q) = {Min:%d Max:%d Conf:%s}, want {Min:%d Max:%d Conf:%s}",
					tt.raw, got.Min, got.Max, got.Conf, tt.wantMin, tt.wantMax, tt.wantConf)
			}
			if got.Raw != tt.raw {
				t.Errorf("ParseSalary(%q).Raw = %q, want %q", tt.raw, got.Raw, tt.raw)
			}
		})
	}
}
