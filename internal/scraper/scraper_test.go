package scraper

import "testing"

func TestClampMaxPages(t *testing.T) {
	tests := []struct {
		name string
		in   int
		want int
	}{
		{"zero uses the default", 0, DefaultMaxPages},
		{"negative uses the default", -5, DefaultMaxPages},
		{"within range passes through unchanged", 10, 10},
		{"exactly the default passes through unchanged", 3, 3},
		{"exactly the cap passes through unchanged", 20, 20},
		{"above the cap is clamped down to it", 999, MaxPagesCap},
		{"one is valid and passes through unchanged", 1, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClampMaxPages(tt.in); got != tt.want {
				t.Errorf("ClampMaxPages(%d) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}
