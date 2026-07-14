// Package config loads binbash's runtime configuration from an optional
// TOML file and/or environment variables. Precedence is built-in defaults <
// config file < environment variables, so any BINBASH_* env var always wins
// over the file -- letting a single value be overridden (e.g. Docker's -e
// flag, or a quick local test) without editing the file.
package config

import (
	"errors"
	"fmt"
	"log"
	"net/netip"
	"os"
	"runtime"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/thinkscotty/binbash/internal/auth"
)

const (
	defaultAITagCount = 3
	maxAITagCount     = 8

	// defaultConfigPath is checked when BINBASH_CONFIG isn't set. Its
	// absence isn't an error -- pure env-var configuration, with no file at
	// all, remains fully supported.
	defaultConfigPath = "./binbash.toml"

	// defaultBindAddress keeps binbash reachable on every interface, which is
	// what a LAN install wants and what it has always done. On a public VPS
	// this is the wrong answer -- it publishes the app in plaintext on
	// http://<public-ip>:8080, straight past the HTTPS reverse proxy -- so
	// there the operator sets bind_address to 127.0.0.1 and lets only the
	// local proxy through.
	defaultBindAddress = "0.0.0.0"
)

type Config struct {
	Port           string
	BindAddress    string
	TrustedProxies []string
	Password       string
	DBPath         string
	AIBaseURL      string
	AIAPIKey       string
	AIModel        string
	AITagCount     int
	AITagBreadth   string
	AITagPrompt    string
	AutoBackupDir  string
}

// fileConfig is the TOML file's shape. Every field is optional -- decoding
// leaves absent ones at their Go zero value, and applyFileConfig only
// overwrites a Config field when the file actually set it. TagCount is a
// *int specifically so "absent from the file" (leave the default) can be
// told apart from "explicitly set to 0" (a valid value: keeps AI tagging
// enabled but requests zero tags).
type fileConfig struct {
	Port          string `toml:"port"`
	BindAddress   string `toml:"bind_address"`
	Password      string `toml:"password"`
	DBPath        string `toml:"db_path"`
	AutoBackupDir string `toml:"auto_backup_dir"`
	// TrustedProxies is a *[]string for the same reason TagCount is a *int:
	// "absent from the file" (keep the loopback default) has to be
	// distinguishable from "explicitly set to an empty list" (trust nothing,
	// the way to opt out of X-Forwarded-* handling entirely).
	TrustedProxies *[]string `toml:"trusted_proxies"`
	AI             struct {
		BaseURL    string `toml:"base_url"`
		APIKey     string `toml:"api_key"`
		Model      string `toml:"model"`
		TagCount   *int   `toml:"tag_count"`
		TagBreadth string `toml:"tag_breadth"`
		TagPrompt  string `toml:"tag_prompt"`
	} `toml:"ai"`
}

func Load() (*Config, error) {
	cfg := &Config{
		Port:        "8080",
		BindAddress: defaultBindAddress,
		// Loopback only. See auth.TrustedProxies for why this default is both
		// safe and necessary: safe because RemoteAddr can't be forged, and
		// necessary because the documented VPS setup puts the TLS proxy on
		// loopback, where believing it is the only way to learn the real
		// client's IP and scheme.
		TrustedProxies: []string{"127.0.0.1", "::1"},
		DBPath:         "./data/binbash.db",
		AITagCount:     defaultAITagCount,
		AITagBreadth:   "moderate",
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
		warnIfReadableByOthers(path)
	} else if explicit {
		return nil, fmt.Errorf("config file %s: %w", path, err)
	}

	// Env vars always win, whether or not a file was loaded.
	cfg.Port = getEnv("BINBASH_PORT", cfg.Port)
	cfg.BindAddress = getEnv("BINBASH_BIND_ADDRESS", cfg.BindAddress)
	cfg.Password = getEnv("BINBASH_PASSWORD", cfg.Password)
	cfg.DBPath = getEnv("BINBASH_DB_PATH", cfg.DBPath)
	cfg.AutoBackupDir = getEnv("BINBASH_AUTO_BACKUP_DIR", cfg.AutoBackupDir)
	cfg.AIBaseURL = getEnv("BINBASH_AI_BASE_URL", cfg.AIBaseURL)
	cfg.AIAPIKey = getEnv("BINBASH_AI_API_KEY", cfg.AIAPIKey)
	cfg.AIModel = getEnv("BINBASH_AI_MODEL", cfg.AIModel)
	cfg.AITagPrompt = getEnv("BINBASH_AI_TAG_PROMPT", cfg.AITagPrompt)
	if v := os.Getenv("BINBASH_TRUSTED_PROXIES"); v != "" {
		cfg.TrustedProxies = splitList(v)
	}

	// A hostname here would be a quiet footgun: it binds to whatever the name
	// resolves to at startup, which is not necessarily what the operator meant
	// to expose. Requiring a literal address keeps "what am I listening on"
	// answerable by reading the config.
	if _, err := netip.ParseAddr(cfg.BindAddress); err != nil {
		return nil, fmt.Errorf("bind address must be an IP address like 0.0.0.0 or 127.0.0.1, not %q", cfg.BindAddress)
	}

	// An absent password is no longer an error here. It is only needed to
	// create the account on first run; from then on the database holds the
	// (hashed) password and this value is ignored entirely, so leaving it in
	// the config file just means a plaintext secret loitering on disk for no
	// benefit. auth.New raises the error if it turns out there's no account
	// yet and nothing to create one from -- it's the only layer that knows.
	if cfg.Password != "" {
		if problem := auth.ValidatePassword(cfg.Password); problem != "" {
			return nil, errors.New(problem)
		}
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

// warnIfReadableByOthers complains when the config file can be read by anyone
// but its owner. The file holds the bootstrap password and any AI API key in
// plaintext, which is the ordinary way self-hosted software stores such things
// -- the control that makes it safe is the file's permissions, not the format.
//
// This warns rather than fixing it, because the config file is the operator's
// own: silently rewriting the permissions on a file a human wrote and owns is
// a surprise, whereas the database (which binbash creates and owns) is fair
// game to tighten. This is what OpenSSH does with a too-permissive private
// key, minus the refusing-to-start part.
func warnIfReadableByOthers(path string) {
	// Unix permission bits are meaningless on Windows -- Stat reports 0666 and
	// we would warn on every startup with no way for the user to satisfy us.
	if runtime.GOOS == "windows" {
		return
	}

	info, err := os.Stat(path)
	if err != nil {
		return // it just loaded, so this is nothing worth failing over
	}
	perm := info.Mode().Perm()
	if perm&0o077 == 0 {
		return
	}

	log.Printf("warning: %s is readable by other users on this machine (mode %#o).", path, perm)
	log.Printf("warning: it contains your password and any API keys. Fix with: chmod 600 %s", path)
}

func applyFileConfig(cfg *Config, fc fileConfig) {
	if fc.Port != "" {
		cfg.Port = fc.Port
	}
	if fc.BindAddress != "" {
		cfg.BindAddress = fc.BindAddress
	}
	if fc.TrustedProxies != nil {
		cfg.TrustedProxies = *fc.TrustedProxies
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
	if fc.AI.TagPrompt != "" {
		cfg.AITagPrompt = fc.AI.TagPrompt
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// splitList parses a comma-separated env var into a list, dropping blanks so
// that stray commas and trailing separators are harmless.
func splitList(v string) []string {
	var out []string
	for _, part := range strings.Split(v, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
