// Package config loads binbash's runtime configuration from environment variables.
package config

import (
	"fmt"
	"os"
)

type Config struct {
	Port      string
	Password  string
	DBPath    string
	AIBaseURL string
	AIAPIKey  string
	AIModel   string
}

func Load() (*Config, error) {
	cfg := &Config{
		Port:      getEnv("BINBASH_PORT", "8080"),
		Password:  os.Getenv("BINBASH_PASSWORD"),
		DBPath:    getEnv("BINBASH_DB_PATH", "./data/binbash.db"),
		AIBaseURL: os.Getenv("BINBASH_AI_BASE_URL"),
		AIAPIKey:  os.Getenv("BINBASH_AI_API_KEY"),
		AIModel:   os.Getenv("BINBASH_AI_MODEL"),
	}

	if cfg.Password == "" {
		return nil, fmt.Errorf("BINBASH_PASSWORD must be set")
	}

	return cfg, nil
}

// AIEnabled reports whether AI tagging is configured.
func (c *Config) AIEnabled() bool {
	return c.AIBaseURL != ""
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
