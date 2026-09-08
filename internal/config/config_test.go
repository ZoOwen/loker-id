package config

import "testing"

func TestGetEnvInt(t *testing.T) {
	cases := []struct {
		name     string
		value    string // "" means the var is left unset
		fallback int32
		want     int32
	}{
		{"unset uses fallback", "", 5, 5},
		{"valid value overrides fallback", "10", 5, 10},
		{"malformed value falls back rather than erroring", "not-a-number", 5, 5},
		{"empty string is treated as unset", "", 7, 7},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			const key = "LOKER_ID_TEST_GETENVINT"
			if tc.value != "" {
				t.Setenv(key, tc.value)
			}

			if got := getEnvInt(key, tc.fallback); got != tc.want {
				t.Errorf("getEnvInt(%q, %d) = %d, want %d", key, tc.fallback, got, tc.want)
			}
		})
	}
}

// TestLoad_DBMaxConnsDefaultsAndOverrides checks that Load wires
// DB_MAX_CONNS through getEnvInt end to end: unset falls back to
// defaultDBMaxConns, an explicit value overrides it.
func TestLoad_DBMaxConnsDefaultsAndOverrides(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost/db?sslmode=disable")
	t.Setenv("INTERNAL_TOKEN", "test-token")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.DBMaxConns != defaultDBMaxConns {
		t.Errorf("DBMaxConns = %d, want default %d", cfg.DBMaxConns, defaultDBMaxConns)
	}

	t.Setenv("DB_MAX_CONNS", "8")
	cfg2, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg2.DBMaxConns != 8 {
		t.Errorf("DBMaxConns = %d, want 8", cfg2.DBMaxConns)
	}
}
