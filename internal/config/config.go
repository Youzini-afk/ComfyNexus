// Package config loads runtime configuration from environment variables.
//
// Configuration is intentionally environment-first to play well with Zeabur's
// service Variables tab. A local .env file (loaded via godotenv) is supported
// for development convenience.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	// Bind is the HTTP listen address, e.g. ":8080".
	Bind string
	// DataDir is the persistent data directory. SQLite, thumbnails, and
	// caches live here. On Zeabur, mount a Volume to this path.
	DataDir string
	// MasterKey is the seed used to derive at-rest encryption keys (AES-GCM).
	// Required: must be at least 32 raw bytes when base64-decoded, or 32
	// characters when used as a passphrase.
	MasterKey string
	// LogLevel: debug | info | warn | error
	LogLevel string
	// TrustProxy enables parsing X-Forwarded-* headers (true behind Zeabur).
	TrustProxy bool
	// CivitaiAPIKey is forwarded to the Civitai client when set.
	CivitaiAPIKey string
	// HFToken is forwarded to remote aria2c when downloading from HuggingFace.
	HFToken string
	// SessionTTLDays controls how long a login session remains valid.
	SessionTTLDays int
	// CookieSecure forces the Secure flag on the session cookie. Defaults to
	// true in production (TrustProxy=true). Allow override for local dev.
	CookieSecure bool
}

func Load() (*Config, error) {
	c := &Config{
		Bind:           getenv("COMFYNEXUS_BIND", getenv("PORT", "8080")),
		DataDir:        getenv("COMFYNEXUS_DATA_DIR", "./data"),
		MasterKey:      os.Getenv("COMFYNEXUS_MASTER_KEY"),
		LogLevel:       strings.ToLower(getenv("COMFYNEXUS_LOG_LEVEL", "info")),
		TrustProxy:     getenvBool("COMFYNEXUS_TRUST_PROXY", true),
		CivitaiAPIKey:  os.Getenv("CIVITAI_API_KEY"),
		HFToken:        os.Getenv("HF_TOKEN"),
		SessionTTLDays: getenvInt("COMFYNEXUS_SESSION_TTL_DAYS", 30),
	}
	c.CookieSecure = getenvBool("COMFYNEXUS_COOKIE_SECURE", c.TrustProxy)

	// Allow PORT to be a bare number or a colon-prefixed/full bind addr.
	if !strings.Contains(c.Bind, ":") {
		c.Bind = ":" + c.Bind
	}

	if c.MasterKey == "" {
		return nil, errors.New("COMFYNEXUS_MASTER_KEY is required (generate with `openssl rand -base64 48`)")
	}
	if len(c.MasterKey) < 16 {
		return nil, fmt.Errorf("COMFYNEXUS_MASTER_KEY too short: need >=16 chars, got %d", len(c.MasterKey))
	}

	abs, err := filepath.Abs(c.DataDir)
	if err != nil {
		return nil, fmt.Errorf("resolve data dir: %w", err)
	}
	c.DataDir = abs
	if err := os.MkdirAll(c.DataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	return c, nil
}

// SQLitePath returns the absolute path of the primary SQLite database file.
func (c *Config) SQLitePath() string { return filepath.Join(c.DataDir, "comfynexus.db") }

// ThumbnailDir returns the directory used for generated thumbnails.
func (c *Config) ThumbnailDir() string { return filepath.Join(c.DataDir, "thumbnails") }

// CacheDir returns the directory used for ephemeral caches (Civitai, etc).
func (c *Config) CacheDir() string { return filepath.Join(c.DataDir, "cache") }

func getenv(k, def string) string {
	if v, ok := os.LookupEnv(k); ok && v != "" {
		return v
	}
	return def
}

func getenvBool(k string, def bool) bool {
	v, ok := os.LookupEnv(k)
	if !ok || v == "" {
		return def
	}
	switch strings.ToLower(v) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return def
}

func getenvInt(k string, def int) int {
	v, ok := os.LookupEnv(k)
	if !ok || v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}
