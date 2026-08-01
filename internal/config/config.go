// Package config handles application configuration loading from environment variables and config files.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the complete configuration of a North process. It is loaded once at
// startup and passed explicitly to whatever needs it. There is deliberately no
// package-level instance: a global would let any package reach configuration it
// has no business reading.
type Config struct {
	Env      Environment
	Port     int
	BaseURL  string
	LogLevel string

	DatabaseURL string

	SessionLifetime time.Duration

	AI      AIConfig
	Storage StorageConfig
}

// AIConfig selects and configures the AI provider. Provider names must match a
// client registered in internal/ai.
type AIConfig struct {
	Provider  string
	Model     string
	FastModel string

	GeminiAPIKey string

	OpenRouterAPIKey  string
	OpenRouterSiteURL string
	OpenRouterSiteApp string
}

// StorageConfig describes an S3-compatible bucket. The same shape serves MinIO
// in development, S3, and Cloudflare R2.
type StorageConfig struct {
	Endpoint     string
	Region       string
	Bucket       string
	AccessKey    string
	SecretKey    string
	UsePathStyle bool
}

// Environment distinguishes development from production behaviour: asset
// caching, cookie flags, and log formatting all key off it.
type Environment string

const (
	EnvDevelopment Environment = "development"
	EnvProduction  Environment = "production"
)

// IsProduction reports whether the process is running in production. Anything
// that is not explicitly "production" is treated as development, so a missing
// or misspelled value fails safe towards the noisier, less trusting mode.
func (e Environment) IsProduction() bool { return e == EnvProduction }

// Load reads configuration from the environment and validates it.
//
// It fails fast: a process that starts with a missing database URL only to
// panic on the first request is harder to diagnose than one that refuses to
// start. All problems are collected and reported together so a fresh checkout
// does not require a dozen restart cycles to configure.
func Load() (*Config, error) {
	var problems []string

	require := func(key string) string {
		v := strings.TrimSpace(os.Getenv(key))
		if v == "" {
			problems = append(problems, fmt.Sprintf("%s is required", key))
		}
		return v
	}

	cfg := &Config{
		Env:      Environment(optional("GO_ENV", string(EnvDevelopment))),
		BaseURL:  optional("BASE_URL", "http://localhost:8090"),
		LogLevel: optional("LOG_LEVEL", "info"),

		DatabaseURL: require("DATABASE_URL"),

		AI: AIConfig{
			Provider:  optional("AI_PROVIDER", "gemini"),
			Model:     optional("AI_MODEL", "gemini-2.5-pro"),
			FastModel: optional("AI_FAST_MODEL", "gemini-2.5-flash"),

			GeminiAPIKey: os.Getenv("GEMINI_API_KEY"),

			OpenRouterAPIKey:  os.Getenv("OPENROUTER_API_KEY"),
			OpenRouterSiteURL: os.Getenv("OPENROUTER_SITE_URL"),
			OpenRouterSiteApp: optional("OPENROUTER_SITE_NAME", "North"),
		},

		Storage: StorageConfig{
			Endpoint:  optional("STORAGE_ENDPOINT", "http://localhost:9000"),
			Region:    optional("STORAGE_REGION", "us-east-1"),
			Bucket:    optional("STORAGE_BUCKET", "north-media"),
			AccessKey: os.Getenv("STORAGE_ACCESS_KEY"),
			SecretKey: os.Getenv("STORAGE_SECRET_KEY"),
		},
	}

	port, err := intValue("PORT", 8090)
	if err != nil {
		problems = append(problems, err.Error())
	}
	cfg.Port = port

	lifetime, err := durationValue("SESSION_LIFETIME", 30*24*time.Hour)
	if err != nil {
		problems = append(problems, err.Error())
	}
	cfg.SessionLifetime = lifetime

	pathStyle, err := boolValue("STORAGE_USE_PATH_STYLE", true)
	if err != nil {
		problems = append(problems, err.Error())
	}
	cfg.Storage.UsePathStyle = pathStyle

	// The selected provider needs its credential; the others do not. Checking
	// only what is in use keeps a Gemini-only setup from demanding four keys.
	switch cfg.AI.Provider {
	case "gemini":
		if cfg.AI.GeminiAPIKey == "" {
			problems = append(problems, "GEMINI_API_KEY is required when AI_PROVIDER=gemini")
		}
	case "openrouter":
		if cfg.AI.OpenRouterAPIKey == "" {
			problems = append(problems, "OPENROUTER_API_KEY is required when AI_PROVIDER=openrouter")
		}
	case "fake":
		// The fake provider exists for tests and for running the application
		// without spending money. It needs no credential.
	default:
		problems = append(problems, fmt.Sprintf("AI_PROVIDER %q is not a known provider (gemini, openrouter, fake)", cfg.AI.Provider))
	}

	if len(problems) > 0 {
		return nil, fmt.Errorf("invalid configuration:\n  - %s", strings.Join(problems, "\n  - "))
	}
	return cfg, nil
}

// Addr is the listen address for the HTTP server.
func (c *Config) Addr() string { return ":" + strconv.Itoa(c.Port) }

func optional(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func intValue(key string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return fallback, fmt.Errorf("%s must be a number, got %q", key, raw)
	}
	return v, nil
}

func boolValue(key string, fallback bool) (bool, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback, fmt.Errorf("%s must be a boolean, got %q", key, raw)
	}
	return v, nil
}

func durationValue(key string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	v, err := time.ParseDuration(raw)
	if err != nil {
		return fallback, fmt.Errorf("%s must be a duration such as 720h, got %q", key, raw)
	}
	return v, nil
}
