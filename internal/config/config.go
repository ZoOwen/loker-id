// Package config loads application configuration from environment variables.
package config

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	AppEnv        string
	Port          string
	DatabaseURL   string
	InternalToken string
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
