// Package config loads binbash's runtime configuration from environment variables.
package config

import (
	"fmt"
	"os"
)

// Bootstrap password length limits. These mirror minPasswordLen/
// maxPasswordLen in internal/handlers/validation.go, which govern in-app
// password rotation -- duplicated here (rather than imported) to avoid the
// config package depending on the handlers package. maxBootstrapPasswordLen
// is bcrypt's hard limit, in bytes: bcrypt.GenerateFromPassword returns a
// hard error above it rather than truncating, so without this check a too-long
// BINBASH_PASSWORD would make the app fail to start with a bcrypt error deep
// in the auth bootstrap path, with nothing telling the operator that password
// length is the actual problem.
const (
	minBootstrapPasswordLen = 8
	maxBootstrapPasswordLen = 72
)

type Config struct {
	Port          string
	Password      string
	DBPath        string
	AIBaseURL     string
	AIAPIKey      string
	AIModel       string
	AutoBackupDir string
}

func Load() (*Config, error) {
	cfg := &Config{
		Port:          getEnv("BINBASH_PORT", "8080"),
		Password:      os.Getenv("BINBASH_PASSWORD"),
		DBPath:        getEnv("BINBASH_DB_PATH", "./data/binbash.db"),
		AIBaseURL:     os.Getenv("BINBASH_AI_BASE_URL"),
		AIAPIKey:      os.Getenv("BINBASH_AI_API_KEY"),
		AIModel:       os.Getenv("BINBASH_AI_MODEL"),
		AutoBackupDir: os.Getenv("BINBASH_AUTO_BACKUP_DIR"),
	}

	if cfg.Password == "" {
		return nil, fmt.Errorf("BINBASH_PASSWORD must be set")
	}
	if len(cfg.Password) < minBootstrapPasswordLen {
		return nil, fmt.Errorf("BINBASH_PASSWORD must be at least %d characters", minBootstrapPasswordLen)
	}
	if len(cfg.Password) > maxBootstrapPasswordLen {
		return nil, fmt.Errorf("BINBASH_PASSWORD must be at most %d bytes long", maxBootstrapPasswordLen)
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
