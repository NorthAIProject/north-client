// Package config handles application configuration loading from environment variables and config files.
package config

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/providers"
	"github.com/NorthAIProject/north-client/internal/shared/secret"
	"github.com/NorthAIProject/north-client/internal/users"
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

	// Google OAuth (optional). Empty credentials disable the feature.
	GoogleClientID     string
	GoogleClientSecret string

	// Strava credentials are optional, like Google's: without them the
	// integration reports itself unavailable rather than failing the boot,
	// so a developer with no Strava app can still run everything else.
	StravaClientID     string
	StravaClientSecret string

	// WebAuthn relying party. RPID defaults to the host of BaseURL when empty.
	WebAuthnRPID        string
	WebAuthnDisplayName string

	AI         AIConfig
	Storage    StorageConfig
	Embedding  EmbeddingConfig
	Encryption EncryptionConfig
	PostHog    PostHogConfig

	// MCPListenAddr is where cmd/mcp-server listens. It defaults to the
	// loopback interface rather than all of them: the MCP surface authenticates
	// with one static token for one account, so binding it wide by accident is
	// the mistake worth making impossible by default.
	MCPListenAddr string

	// MCPAllowedOrigins are browser origins permitted to reach /mcp, comma
	// separated in MCP_ALLOWED_ORIGINS.
	//
	// Empty by default, which rejects every browser: a real MCP client sends no
	// Origin header at all, so the only thing an Origin identifies is a web
	// page — and a web page reaching a loopback server is the DNS-rebinding
	// case, not a user.
	MCPAllowedOrigins []string

	// MCPRequestsPerMinute bounds the single token's call rate. Zero uses the
	// package default. ask_coach spends money on every call.
	MCPRequestsPerMinute int
}

// AIConfig selects and configures the AI providers. Provider names must match a
// client registered in internal/ai.
type AIConfig struct {
	// Chain is the ordered preference list of providers. The first one whose
	// credentials are present and which answers a request wins; the rest are
	// there for when it refuses.
	Chain []string

	// FreeChain is the equivalent for users on the free tier, so the cheap and
	// self-hosted backends carry them by default.
	FreeChain []string

	// Model overrides the head provider's default model. Empty means each
	// provider uses its own configured model.
	Model     string
	FastModel string

	GeminiAPIKey string
	GeminiModel  string

	// UploadProvider handles work that needs a file upload API — form video
	// analysis. Not part of a chain: the OpenAI-dialect backends have no upload
	// endpoint, so there is nothing useful to fall back to.
	UploadProvider string

	OpenRouter OpenAICompatConfig
	NVIDIA     OpenAICompatConfig
	XAI        OpenAICompatConfig
	Hermes     OpenAICompatConfig

	// OpenRouter's attribution headers. Its convention, not the dialect's.
	OpenRouterSiteURL  string
	OpenRouterSiteName string
}

// EmbeddingConfig turns on semantic retrieval.
//
// Off unless Provider and Model are both set, and that default is deliberate:
// with no embeddings configured North retrieves by full text alone, which is
// what it did before and is a complete feature rather than a degraded one.
type EmbeddingConfig struct {
	// Provider names a registered OpenAI-dialect client that serves
	// /embeddings. NVIDIA NIM does; OpenRouter and xAI do not.
	Provider string

	Model string

	// Dimensions must match both the model and the vector column in
	// migrations/20260808192429_create_chunk_embeddings.sql. Checked at
	// startup, because the alternative is finding out mid-reindex.
	Dimensions int
}

// Enabled reports whether semantic retrieval is configured.
func (e EmbeddingConfig) Enabled() bool {
	return e.Provider != "" && e.Model != "" && e.Dimensions > 0
}

// EncryptionConfig holds the keys that seal user-supplied secrets at rest.
//
// Off unless ENCRYPTION_KEY is set, and that default is deliberate: a
// deployment with no key runs everything except the features that would have
// to store somebody's credential, which report themselves unavailable rather
// than storing it in the clear.
//
// The first key seals; the rest only open. See internal/shared/secret.
type EncryptionConfig struct {
	Keys []secret.Key
}

// Enabled reports whether secrets can be stored.
func (e EncryptionConfig) Enabled() bool { return len(e.Keys) > 0 }

// PostHogConfig reports the coach's LLM calls to PostHog's AI Observability.
//
// Optional, like Strava and Google: an empty APIKey in production just means
// no events. Unlike them, an empty APIKey in development fails the boot —
// see cmd/web/main.go — because a missing analytics key is a silent gap that
// only shows up as an empty dashboard, and every other optional integration
// here at least reports its own absence somewhere a person will see it.
type PostHogConfig struct {
	APIKey string
	Host   string
}

// OpenAICompatConfig configures one backend speaking the OpenAI chat dialect.
type OpenAICompatConfig struct {
	APIKey  string
	BaseURL string
	Model   string
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

		GoogleClientID:     strings.TrimSpace(os.Getenv("GOOGLE_CLIENT_ID")),
		GoogleClientSecret: strings.TrimSpace(os.Getenv("GOOGLE_CLIENT_SECRET")),

		StravaClientID:     strings.TrimSpace(os.Getenv("STRAVA_CLIENT_ID")),
		StravaClientSecret: strings.TrimSpace(os.Getenv("STRAVA_CLIENT_SECRET")),

		MCPListenAddr: optional("MCP_LISTEN_ADDR", "127.0.0.1:8093"),

		Embedding: EmbeddingConfig{
			Provider: optional("EMBEDDING_PROVIDER", ""),
			Model:    optional("EMBEDDING_MODEL", ""),
		},
		MCPAllowedOrigins: commaList("MCP_ALLOWED_ORIGINS"),

		WebAuthnRPID:        optional("WEBAUTHN_RP_ID", ""),
		WebAuthnDisplayName: optional("WEBAUTHN_RP_DISPLAY_NAME", "North"),

		AI: AIConfig{
			Chain:     providerChain("AI_PROVIDER_CHAIN"),
			FreeChain: providerChain("AI_PROVIDER_CHAIN_FREE"),

			// Both default to empty, meaning "whatever model the provider that
			// answers is configured with". A concrete default here would name a
			// model from one vendor and fail against every other.
			Model:     optional("AI_MODEL", ""),
			FastModel: optional("AI_FAST_MODEL", ""),

			GeminiAPIKey: os.Getenv("GEMINI_API_KEY"),
			GeminiModel:  optional("GEMINI_MODEL", "gemini-2.5-pro"),

			UploadProvider: optional("AI_UPLOAD_PROVIDER", "gemini"),

			OpenRouter: OpenAICompatConfig{
				APIKey:  os.Getenv("OPENROUTER_API_KEY"),
				BaseURL: optional("OPENROUTER_BASE_URL", "https://openrouter.ai/api/v1"),
				Model:   optional("OPENROUTER_MODEL", "anthropic/claude-sonnet-4.5"),
			},
			NVIDIA: OpenAICompatConfig{
				APIKey:  os.Getenv("NVIDIA_API_KEY"),
				BaseURL: optional("NVIDIA_BASE_URL", "https://integrate.api.nvidia.com/v1"),
				Model:   optional("NVIDIA_MODEL", "meta/llama-3.3-70b-instruct"),
			},
			XAI: OpenAICompatConfig{
				APIKey:  os.Getenv("XAI_API_KEY"),
				BaseURL: optional("XAI_BASE_URL", "https://api.x.ai/v1"),
				Model:   optional("XAI_MODEL", "grok-4.5"),
			},
			Hermes: OpenAICompatConfig{
				APIKey: os.Getenv("HERMES_API_KEY"),
				// No default: Hermes is self-hosted and has no public address.
				BaseURL: os.Getenv("HERMES_BASE_URL"),
				Model:   optional("HERMES_MODEL", "hermes-3"),
			},

			OpenRouterSiteURL:  os.Getenv("OPENROUTER_SITE_URL"),
			OpenRouterSiteName: optional("OPENROUTER_SITE_NAME", "North"),
		},

		Storage: StorageConfig{
			Endpoint:  optional("STORAGE_ENDPOINT", "http://localhost:9000"),
			Region:    optional("STORAGE_REGION", "us-east-1"),
			Bucket:    optional("STORAGE_BUCKET", "north-media"),
			AccessKey: os.Getenv("STORAGE_ACCESS_KEY"),
			SecretKey: os.Getenv("STORAGE_SECRET_KEY"),
		},

		PostHog: PostHogConfig{
			APIKey: os.Getenv("POSTHOG_API_KEY"),
			Host:   optional("POSTHOG_HOST", "https://us.i.posthog.com"),
		},
	}

	port, err := intValue("PORT", 8090)
	if err != nil {
		problems = append(problems, err.Error())
	}
	cfg.Port = port

	mcpRate, err := intValue("MCP_REQUESTS_PER_MINUTE", 0)
	if err != nil {
		problems = append(problems, err.Error())
	}
	cfg.MCPRequestsPerMinute = mcpRate

	// 1024 is the width of the vector column. A model of another width needs a
	// migration, so a mismatch is refused at startup rather than at the first
	// insert of somebody's reindex.
	dims, err := intValue("EMBEDDING_DIMENSIONS", 1024)
	if err != nil {
		problems = append(problems, err.Error())
	}
	cfg.Embedding.Dimensions = dims
	if cfg.Embedding.Enabled() && dims != 1024 {
		problems = append(problems, "EMBEDDING_DIMENSIONS must be 1024 to match the chunk_embeddings column")
	}
	if (cfg.Embedding.Provider == "") != (cfg.Embedding.Model == "") {
		problems = append(problems, "EMBEDDING_PROVIDER and EMBEDDING_MODEL must be set together")
	}

	// A malformed key is always a problem, even though an absent one is not.
	// Somebody who set this variable meant to turn encryption on, and booting
	// anyway is the failure mode where credentials get written in the clear.
	// The error names the shape and never the value.
	keys, err := secret.ParseKeys(os.Getenv("ENCRYPTION_KEY"))
	if err != nil {
		problems = append(problems, "ENCRYPTION_KEY is invalid: "+err.Error())
	}
	cfg.Encryption.Keys = keys

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

	// AI_PROVIDER is the older single-provider form. Honouring it as a
	// one-element chain keeps existing .env files and deployments working.
	if len(cfg.AI.Chain) == 0 {
		cfg.AI.Chain = []string{optional("AI_PROVIDER", "gemini")}
	}
	if len(cfg.AI.FreeChain) == 0 {
		cfg.AI.FreeChain = cfg.AI.Chain
	}

	// Only the names are checked here. Whether a named provider has its
	// credential is settled in providers.Build, which skips the ones that do
	// not and fails only if nothing usable is left — that way one chain can be
	// shared between a laptop with two keys and a deployment with five.
	//
	// Reported once per distinct name: the free chain defaults to the main one,
	// so a single typo would otherwise be listed twice.
	reported := make(map[string]bool)
	for _, name := range append(append([]string{}, cfg.AI.Chain...), cfg.AI.FreeChain...) {
		if knownProviders[name] || reported[name] {
			continue
		}
		reported[name] = true
		problems = append(problems, fmt.Sprintf(
			"%q is not a known AI provider (%s)", name, strings.Join(knownProviderNames(), ", ")))
	}

	if len(problems) > 0 {
		return nil, fmt.Errorf("invalid configuration:\n  - %s", strings.Join(problems, "\n  - "))
	}
	return cfg, nil
}

// Addr is the listen address for the HTTP server.
func (c *Config) Addr() string { return ":" + strconv.Itoa(c.Port) }

// ChainSet renders the configured chains as the coach's provider preference.
//
// Only the free tier gets an entry of its own. Anything else — "pro" today,
// whatever billing invents later — falls back to the main chain, so adding a
// tier does not silently leave its users with no provider at all.
func (c AIConfig) ChainSet() ai.ChainSet {
	return ai.NewChainSet(c.Chain, map[string][]string{
		string(users.TierFree): c.FreeChain,
	})
}

// ProviderOptions renders the AI configuration as the registry's build input.
//
// It lives here rather than in each main so that cmd/web and cmd/worker cannot
// drift apart — they previously repeated the same literal, and a provider added
// to one but not the other would have failed only in whichever process happened
// to need it.
//
// The dependency runs config -> providers, never the reverse: providers stays
// ignorant of how North reads its environment.
func (c AIConfig) ProviderOptions() providers.Options {
	return providers.Options{
		Chain:        c.Chain,
		GeminiAPIKey: c.GeminiAPIKey,
		GeminiModel:  c.GeminiModel,
		Compatible: []providers.Compatible{
			{
				Name:    "openrouter",
				BaseURL: c.OpenRouter.BaseURL,
				APIKey:  c.OpenRouter.APIKey,
				Model:   c.OpenRouter.Model,
				Headers: map[string]string{
					"HTTP-Referer": c.OpenRouterSiteURL,
					"X-Title":      c.OpenRouterSiteName,
				},
				SupportsJSONSchema: true,
			},
			{
				Name:    "nvidia",
				BaseURL: c.NVIDIA.BaseURL,
				APIKey:  c.NVIDIA.APIKey,
				Model:   c.NVIDIA.Model,
				// NIM's strict json_schema support varies by model, and a
				// request it rejects fails outright. Asking in the prompt costs
				// a retry at worst.
				SupportsJSONSchema: false,
			},
			{
				Name:               "xai",
				BaseURL:            c.XAI.BaseURL,
				APIKey:             c.XAI.APIKey,
				Model:              c.XAI.Model,
				SupportsJSONSchema: true,
			},
			{
				Name:    "hermes",
				BaseURL: c.Hermes.BaseURL,
				APIKey:  c.Hermes.APIKey,
				Model:   c.Hermes.Model,
				// Depends on whichever model the gateway is fronting, so the
				// safe assumption is the weaker one.
				SupportsJSONSchema: false,
			},
		},
	}
}

func optional(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

// knownProviders is the set of names internal/ai/providers can build. It exists
// so a typo in a chain is caught at boot rather than becoming a silently
// skipped provider — Resolve drops unknown names, which is the right behaviour
// at runtime and the wrong one for a misspelled configuration.
var knownProviders = map[string]bool{
	"gemini":     true,
	"openrouter": true,
	"nvidia":     true,
	"xai":        true,
	"hermes":     true,
	"fake":       true,
}

func knownProviderNames() []string {
	names := make([]string, 0, len(knownProviders))
	for name := range knownProviders {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// providerChain reads a comma-separated preference list, discarding blanks so
// a trailing comma or a padded value is not treated as a provider named "".
func providerChain(key string) []string {
	return commaList(key)
}

// commaList reads a comma-separated setting, discarding blanks.
func commaList(key string) []string {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return nil
	}

	parts := strings.Split(raw, ",")
	chain := make([]string, 0, len(parts))
	for _, part := range parts {
		if name := strings.TrimSpace(part); name != "" {
			chain = append(chain, name)
		}
	}
	return chain
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
