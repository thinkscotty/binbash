// Package config loads binbash's runtime configuration from an optional
// TOML file and/or environment variables. Precedence is built-in defaults <
// config file < environment variables, so any BINBASH_* env var always wins
// over the file -- letting a single value be overridden (e.g. Docker's -e
// flag, or a quick local test) without editing the file.
package config

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
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

	defaultAITagCount = 3
	maxAITagCount     = 8

	// defaultConfigPath is checked when BINBASH_CONFIG isn't set. Its
	// absence isn't an error -- pure env-var configuration, with no file at
	// all, remains fully supported.
	defaultConfigPath = "./binbash.toml"
)

type Config struct {
	Port          string
	Password      string
	DBPath        string
	AIBaseURL     string
	AIAPIKey      string
	AIModel       string
	AITagCount    int
	AITagBreadth  string
	AutoBackupDir string
}

// fileConfig is the TOML file's shape. Every field is optional -- decoding
// leaves absent ones at their Go zero value, and applyFileConfig only
// overwrites a Config field when the file actually set it. TagCount is a
// *int specifically so "absent from the file" (leave the default) can be
// told apart from "explicitly set to 0" (a valid value: keeps AI tagging
// enabled but requests zero tags).
type fileConfig struct {
	Port          string `toml:"port"`
	Password      string `toml:"password"`
	DBPath        string `toml:"db_path"`
	AutoBackupDir string `toml:"auto_backup_dir"`
	AI            struct {
		BaseURL    string `toml:"base_url"`
		APIKey     string `toml:"api_key"`
		Model      string `toml:"model"`
		TagCount   *int   `toml:"tag_count"`
		TagBreadth string `toml:"tag_breadth"`
	} `toml:"ai"`
}

func Load() (*Config, error) {
	cfg := &Config{
		Port:         "8080",
		DBPath:       "./data/binbash.db",
		AITagCount:   defaultAITagCount,
		AITagBreadth: "moderate",
	}

	path, explicit := os.LookupEnv("BINBASH_CONFIG")
	if !explicit {
		path = defaultConfigPath
	}
	if _, err := os.Stat(path); err == nil {
		var fc fileConfig
		if _, err := toml.DecodeFile(path, &fc); err != nil {
			return nil, fmt.Errorf("parsing config file %s: %w", path, err)
		}
		applyFileConfig(cfg, fc)
		log.Printf("config: loaded %s", path)
	} else if explicit {
		return nil, fmt.Errorf("config file %s: %w", path, err)
	}

	// Env vars always win, whether or not a file was loaded.
	cfg.Port = getEnv("BINBASH_PORT", cfg.Port)
	cfg.Password = getEnv("BINBASH_PASSWORD", cfg.Password)
	cfg.DBPath = getEnv("BINBASH_DB_PATH", cfg.DBPath)
	cfg.AutoBackupDir = getEnv("BINBASH_AUTO_BACKUP_DIR", cfg.AutoBackupDir)
	cfg.AIBaseURL = getEnv("BINBASH_AI_BASE_URL", cfg.AIBaseURL)
	cfg.AIAPIKey = getEnv("BINBASH_AI_API_KEY", cfg.AIAPIKey)
	cfg.AIModel = getEnv("BINBASH_AI_MODEL", cfg.AIModel)

	if cfg.Password == "" {
		return nil, fmt.Errorf("password must be set, via BINBASH_PASSWORD or the config file")
	}
	if len(cfg.Password) < minBootstrapPasswordLen {
		return nil, fmt.Errorf("password must be at least %d characters", minBootstrapPasswordLen)
	}
	if len(cfg.Password) > maxBootstrapPasswordLen {
		return nil, fmt.Errorf("password must be at most %d bytes long", maxBootstrapPasswordLen)
	}

	tagCountStr := getEnv("BINBASH_AI_TAG_COUNT", strconv.Itoa(cfg.AITagCount))
	tagCount, err := strconv.Atoi(tagCountStr)
	if err != nil || tagCount < 0 || tagCount > maxAITagCount {
		return nil, fmt.Errorf("AI tag count must be a number between 0 and %d", maxAITagCount)
	}
	cfg.AITagCount = tagCount

	breadth := strings.ToLower(getEnv("BINBASH_AI_TAG_BREADTH", cfg.AITagBreadth))
	if breadth != "narrow" && breadth != "moderate" && breadth != "broad" {
		return nil, fmt.Errorf("AI tag breadth must be one of: narrow, moderate, broad")
	}
	cfg.AITagBreadth = breadth

	return cfg, nil
}

// AIEnabled reports whether AI tagging is configured.
func (c *Config) AIEnabled() bool {
	return c.AIBaseURL != ""
}

func applyFileConfig(cfg *Config, fc fileConfig) {
	if fc.Port != "" {
		cfg.Port = fc.Port
	}
	if fc.Password != "" {
		cfg.Password = fc.Password
	}
	if fc.DBPath != "" {
		cfg.DBPath = fc.DBPath
	}
	if fc.AutoBackupDir != "" {
		cfg.AutoBackupDir = fc.AutoBackupDir
	}
	if fc.AI.BaseURL != "" {
		cfg.AIBaseURL = fc.AI.BaseURL
	}
	if fc.AI.APIKey != "" {
		cfg.AIAPIKey = fc.AI.APIKey
	}
	if fc.AI.Model != "" {
		cfg.AIModel = fc.AI.Model
	}
	if fc.AI.TagCount != nil {
		cfg.AITagCount = *fc.AI.TagCount
	}
	if fc.AI.TagBreadth != "" {
		cfg.AITagBreadth = fc.AI.TagBreadth
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
