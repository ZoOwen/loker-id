// Package config loads application configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

// defaultDBMaxConns caps the pgxpool at a size a Neon free-tier project's
// connection limit can absorb even with a couple of instances/deploys
// overlapping, rather than pgxpool's own CPU-count-scaled default. See
// database.NewPool.
const defaultDBMaxConns = 5

type Config struct {
	AppEnv        string
	Port          string
	DatabaseURL   string
	InternalToken string
	DBMaxConns    int32
}

// Load reads configuration from the environment, loading a .env file first
// if one is present (silently skipped otherwise, e.g. in production).
func Load() (*Config, error) {
	_ = godotenv.Load()

	cfg := &Config{
		AppEnv:        getEnv("APP_ENV", "development"),
		Port:          getEnv("PORT", "8080"),
		DatabaseURL:   os.Getenv("DATABASE_URL"),
		InternalToken: os.Getenv("INTERNAL_TOKEN"),
		DBMaxConns:    getEnvInt("DB_MAX_CONNS", defaultDBMaxConns),
	}

	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("config: DATABASE_URL is required")
	}
	// Required, not defaulted: POST /internal/scrape checks requests
	// against this. Silently falling back to an empty value would leave
	// that endpoint permanently (and confusingly) unreachable rather
	// than failing loudly at startup.
	if cfg.InternalToken == "" {
		return nil, fmt.Errorf("config: INTERNAL_TOKEN is required")
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// getEnvInt reads an integer env var, falling back (rather than failing
// Load outright) on either an unset or a malformed value — this only ever
// tunes a pool size, not something worth taking the whole app down over a
// typo.
func getEnvInt(key string, fallback int32) int32 {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return int32(n)
}
