// Package config handles application configuration loading from environment variables and config files.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/providers"
	"github.com/NorthAIProject/north-client/internal/quota"
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

	// AutoMigrate controls whether a process applies pending goose migrations
	// as it starts. True is right everywhere a single process owns the
	// database — local development, docker compose, `task dev`.
	//
	// It is false in Kubernetes, where the web and worker Deployments would
	// otherwise race goose against each other on every rollout. There the
	// migration runs once, as `main migrate`, in a PreSync hook ahead of both.
	AutoMigrate bool

	// SMTP delivers transactional email. Optional, like the credentials below
	// it: without a host the password reset journey turns itself off rather
	// than pretending to send.
	SMTP SMTPConfig

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

	Telegram TelegramConfig

	// Push signs Web Push requests. Optional: without a key pair the settings
	// page offers no button and nudges stay in the bell and on Telegram.
	Push PushConfig

	AI         AIConfig
	Storage    StorageConfig
	Embedding  EmbeddingConfig
	Encryption EncryptionConfig
	PostHog    PostHogConfig
	Quota      QuotaConfig
	QuotaPro   QuotaConfig

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

	// MetricsListenAddr is where Prometheus scrapes. Loopback by default, and
	// empty turns the listener off entirely.
	//
	// Deliberately its own listener rather than a route on the public router:
	// request rates, model spend and job failure counts describe the business
	// to anyone who asks for them. /healthz is public because it answers one
	// bit; this answers rather more.
	MetricsListenAddr string
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

	// Variants are the chain entries written as "provider=model": the same
	// backend as their base provider, pinned to a different model.
	//
	// They exist because a chain entry names a provider and a provider carries
	// exactly one model, which leaves "OpenRouter on Sonnet, then OpenRouter on
	// a free model" inexpressible. Derived from Chain and FreeChain during
	// Load; ProviderOptions turns each one into its own registered client.
	Variants []ProviderVariant

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

	// OpenRouterFreeAPIKey funds the free floor — the tail of every chain,
	// reached by users with no key of their own and by deployments with no key
	// at all. Optional: unset, the free variants inherit OpenRouter's ordinary
	// key.
	//
	// It is separate so North's own floor can sit on a different account from
	// an operator's paid credit, and because a key handed to every user needs a
	// spend ceiling. That ceiling is enforced in Load: with this set, every
	// OpenRouter variant must name a ":free" model.
	OpenRouterFreeAPIKey string
}

// ProviderVariant is one chain entry of the form "provider=model".
//
// Entry is also the registry key. A variant registers under the exact string
// that named it, so Registry.Resolve finds it without a lookup table and the
// chain, the logs, and the registry all say the same thing.
type ProviderVariant struct {
	Entry string
	Base  string
	Model string
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

// Sealer builds the encryptor these keys describe, or nil when none are
// configured.
//
// (nil, nil) is a supported result and the callers depend on it: a deployment
// without a key runs everything else, and the features that would have to store
// somebody's credential report themselves unavailable instead. An error here
// means keys were supplied and are unusable, which Load has already rejected —
// so a caller that sees one should refuse to start rather than continue without
// encryption it was told to use.
func (e EncryptionConfig) Sealer() (*secret.Sealer, error) {
	if !e.Enabled() {
		return nil, nil
	}
	return secret.NewSealer(e.Keys...)
}

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

// SMTPConfig points at a mail submission endpoint.
//
// SMTP rather than a provider SDK because every transactional provider speaks
// it, so switching from one to another is an environment change rather than a
// code change — and it keeps a dependency out of go.mod for something the
// standard library already does.
type SMTPConfig struct {
	Host string
	Port int

	// Username and Password may be empty for a relay that authenticates by IP.
	// Hosted providers all want them.
	Username string
	Password string

	// From must be an address the provider has been configured to send for.
	// They reject anything else, so this is not free-form.
	From     string
	FromName string
}

// Enabled reports whether mail can actually be delivered.
//
// Host and From are the two that cannot be guessed. Everything else has a
// working default or is genuinely optional.
func (c SMTPConfig) Enabled() bool {
	return c.Host != "" && c.From != ""
}

// QuotaConfig bounds how often one account may take an action that costs money
// or real work. Every value is per hour.
//
// Zero means "use the package default" rather than "forbid", so an operator who
// mistypes a variable name gets the shipped bound rather than a feature that
// refuses everything. Deliberately not a map keyed by action name: an
// unrecognised key in an env var would be silent, and a misspelled limit that
// looks configured is worse than one that plainly is not.
//
// The same shape serves both tiers. Config.Quota holds the fallback, which is
// what free accounts and anything without a tier of its own get; Config.QuotaPro
// holds the paid ceilings. The variables an operator already sets are the
// fallback, so introducing a paid tier changed nobody's existing limits.
//
// Only the counts differ between tiers, never the window — see quota.NewLimits
// for why a per-tier window would hand a user a fresh budget for upgrading.
type QuotaConfig struct {
	CoachMessages     int
	DocumentUploads   int
	DocumentReindexes int
	ReportGenerations int
	MediaAnalyses     int
	AccountExports    int

	// QuickCaptures bounds the parse, which is a model call. The commit that
	// follows costs a transaction and is deliberately unguarded: refusing it
	// after somebody has already paid for the parse is the worst possible
	// place to stop them.
	QuickCaptures int
}

// Limits renders the config as the per-action budgets the quota package wants.
// The window is left at zero so quota.NewLimits applies its own default and
// stays the single owner of that decision.
func (q QuotaConfig) Limits() map[quota.Action]quota.Limit {
	return map[quota.Action]quota.Limit{
		quota.CoachMessage:    {PerWindow: q.CoachMessages},
		quota.DocumentUpload:  {PerWindow: q.DocumentUploads},
		quota.DocumentReindex: {PerWindow: q.DocumentReindexes},
		quota.ReportGenerate:  {PerWindow: q.ReportGenerations},
		quota.MediaAnalysis:   {PerWindow: q.MediaAnalyses},
		quota.AccountExport:   {PerWindow: q.AccountExports},
		quota.QuickCapture:    {PerWindow: q.QuickCaptures},
	}
}

// QuotaLimits maps tiers to their budgets, ready for quota.NewService.
//
// Only the free tier gets an entry of its own, for the same reason ChainSet
// does it that way: anything else — "pro" today, whatever billing invents
// later — falls back rather than resolving to no budget at all.
func (c *Config) QuotaLimits() quota.Limits {
	return quota.NewLimits(c.Quota.Limits(), map[string]map[quota.Action]quota.Limit{
		string(users.TierPro): c.QuotaPro.Limits(),
	})
}

// TelegramConfig configures the Telegram messaging adapter.
//
// Optional, like Google's and Strava's credentials: without a token the
// adapter is not built at all, so a developer with no bot still runs
// everything else.
type TelegramConfig struct {
	// BotToken is the credential from @BotFather. Empty disables messaging.
	BotToken string

	// WebhookSecret selects how updates arrive, because the two modes need
	// different things from the environment and only one can run per bot.
	//
	// Set, North serves a webhook — the production shape, and it requires a
	// public HTTPS URL. Empty, North long-polls instead, which needs nothing
	// but an outbound connection and is therefore the only mode that works on
	// localhost. Making the mode follow from the secret rather than from a
	// third variable means there is no combination that configures a webhook
	// with no secret, which would be an open endpoint.
	WebhookSecret string

	// BotUsername is shown in Settings so a person can find the bot. Cosmetic;
	// nothing authenticates against it.
	BotUsername string
}

// Enabled reports whether the messaging adapter should be built.
func (c TelegramConfig) Enabled() bool { return c.BotToken != "" }

// UsesWebhook reports which inbound edge to run.
func (c TelegramConfig) UsesWebhook() bool { return c.WebhookSecret != "" }

// PushConfig is the VAPID key pair that identifies this deployment to the
// browsers' push services.
//
// Generated once with `main vapid-keygen`. The public key is embedded in the
// page and stored by every browser that subscribes, so replacing it silently
// orphans every existing subscription — treat the pair like a signing key,
// not like a password that is rotated on a schedule.
type PushConfig struct {
	VAPIDPublicKey  string
	VAPIDPrivateKey string

	// Subject is how a push service operator reaches whoever runs this
	// server: a mailto: address or an https URL. Defaults to BASE_URL.
	Subject string
}

// Enabled reports whether push can be sent at all.
func (c PushConfig) Enabled() bool {
	return c.VAPIDPublicKey != "" && c.VAPIDPrivateKey != ""
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

		SMTP: SMTPConfig{
			Host:     strings.TrimSpace(os.Getenv("SMTP_HOST")),
			Username: strings.TrimSpace(os.Getenv("SMTP_USERNAME")),
			Password: os.Getenv("SMTP_PASSWORD"),
			From:     strings.TrimSpace(os.Getenv("SMTP_FROM")),
			FromName: optional("SMTP_FROM_NAME", "Khepri"),
		},

		GoogleClientID:     strings.TrimSpace(os.Getenv("GOOGLE_CLIENT_ID")),
		GoogleClientSecret: strings.TrimSpace(os.Getenv("GOOGLE_CLIENT_SECRET")),

		StravaClientID:     strings.TrimSpace(os.Getenv("STRAVA_CLIENT_ID")),
		StravaClientSecret: strings.TrimSpace(os.Getenv("STRAVA_CLIENT_SECRET")),

		MCPListenAddr:     optional("MCP_LISTEN_ADDR", "127.0.0.1:8093"),
		MetricsListenAddr: optional("METRICS_LISTEN_ADDR", "127.0.0.1:9090"),

		Embedding: EmbeddingConfig{
			Provider: optional("EMBEDDING_PROVIDER", ""),
			Model:    optional("EMBEDDING_MODEL", ""),
		},
		MCPAllowedOrigins: commaList("MCP_ALLOWED_ORIGINS"),

		WebAuthnRPID:        optional("WEBAUTHN_RP_ID", ""),
		WebAuthnDisplayName: optional("WEBAUTHN_RP_DISPLAY_NAME", "Khepri"),

		Telegram: TelegramConfig{
			BotToken:      strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN")),
			WebhookSecret: strings.TrimSpace(os.Getenv("TELEGRAM_WEBHOOK_SECRET")),
			BotUsername:   strings.TrimPrefix(strings.TrimSpace(os.Getenv("TELEGRAM_BOT_USERNAME")), "@"),
		},

		Push: PushConfig{
			VAPIDPublicKey:  strings.TrimSpace(os.Getenv("VAPID_PUBLIC_KEY")),
			VAPIDPrivateKey: strings.TrimSpace(os.Getenv("VAPID_PRIVATE_KEY")),
			Subject:         strings.TrimSpace(os.Getenv("VAPID_SUBJECT")),
		},

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
			OpenRouterSiteName: optional("OPENROUTER_SITE_NAME", "Khepri"),

			OpenRouterFreeAPIKey: strings.TrimSpace(os.Getenv("OPENROUTER_FREE_API_KEY")),
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

	// Per hour, not per minute: these are human actions with bursty shapes, and
	// three coach messages in a row is somebody thinking out loud rather than
	// somebody abusing the service.
	for _, q := range []struct {
		key   string
		def   int
		field *int
	}{
		{"QUOTA_COACH_MESSAGES_PER_HOUR", 30, &cfg.Quota.CoachMessages},
		{"QUOTA_DOCUMENT_UPLOADS_PER_HOUR", 60, &cfg.Quota.DocumentUploads},
		{"QUOTA_DOCUMENT_REINDEX_PER_HOUR", 10, &cfg.Quota.DocumentReindexes},
		{"QUOTA_REPORT_GENERATIONS_PER_HOUR", 10, &cfg.Quota.ReportGenerations},
		{"QUOTA_MEDIA_ANALYSES_PER_HOUR", 20, &cfg.Quota.MediaAnalyses},
		{"QUOTA_QUICK_CAPTURES_PER_HOUR", 60, &cfg.Quota.QuickCaptures},
		// Low because an export reads the whole account — every document out of
		// the bucket included — and nobody needs their entire history four times
		// in an hour.
		{"QUOTA_ACCOUNT_EXPORTS_PER_HOUR", 3, &cfg.Quota.AccountExports},

		// Paid ceilings. Generous multiples rather than "unlimited": these are
		// an abuse stop, not a product limit, and a paying account that somehow
		// runs a retry loop should still hit something.
		{"QUOTA_PRO_COACH_MESSAGES_PER_HOUR", 300, &cfg.QuotaPro.CoachMessages},
		{"QUOTA_PRO_DOCUMENT_UPLOADS_PER_HOUR", 300, &cfg.QuotaPro.DocumentUploads},
		{"QUOTA_PRO_DOCUMENT_REINDEX_PER_HOUR", 60, &cfg.QuotaPro.DocumentReindexes},
		{"QUOTA_PRO_REPORT_GENERATIONS_PER_HOUR", 60, &cfg.QuotaPro.ReportGenerations},
		{"QUOTA_PRO_MEDIA_ANALYSES_PER_HOUR", 100, &cfg.QuotaPro.MediaAnalyses},
		{"QUOTA_PRO_QUICK_CAPTURES_PER_HOUR", 600, &cfg.QuotaPro.QuickCaptures},
		// Not raised as far as the rest: an export reads every document in the
		// account out of the bucket, and paying for the product does not make
		// that cheap.
		{"QUOTA_PRO_ACCOUNT_EXPORTS_PER_HOUR", 10, &cfg.QuotaPro.AccountExports},
	} {
		v, quotaErr := intValue(q.key, q.def)
		if quotaErr != nil {
			problems = append(problems, quotaErr.Error())
			continue
		}
		if v < 0 {
			problems = append(problems, q.key+" must not be negative")
			continue
		}
		*q.field = v
	}

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

	autoMigrate, err := boolValue("AUTO_MIGRATE", true)
	if err != nil {
		problems = append(problems, err.Error())
	}
	cfg.AutoMigrate = autoMigrate

	// 587 is the submission port, upgraded with STARTTLS. 465 is implicit TLS
	// and the mailer dials it encrypted; anything else is treated as 587-like.
	smtpPort, err := intValue("SMTP_PORT", 587)
	if err != nil {
		problems = append(problems, err.Error())
	}
	cfg.SMTP.Port = smtpPort

	// A half-configured mailer is refused rather than left to fail on the first
	// reset request. Somebody who set SMTP_HOST has said they want mail; a
	// missing SMTP_FROM at that point is a typo, not a decision, and finding
	// out at boot is cheaper than finding out from a locked-out user.
	if cfg.SMTP.Host != "" && cfg.SMTP.From == "" {
		problems = append(problems, "SMTP_FROM is required when SMTP_HOST is set")
	}
	if cfg.SMTP.From != "" && cfg.SMTP.Host == "" {
		problems = append(problems, "SMTP_HOST is required when SMTP_FROM is set")
	}

	// Half a key pair is a typo, not a decision, for the same reason as SMTP.
	// The subject defaults to the site itself, which is what a push service
	// operator would look up anyway.
	if (cfg.Push.VAPIDPublicKey == "") != (cfg.Push.VAPIDPrivateKey == "") {
		problems = append(problems, "VAPID_PUBLIC_KEY and VAPID_PRIVATE_KEY must be set together")
	}
	if cfg.Push.Subject == "" {
		cfg.Push.Subject = cfg.BaseURL
	}
	if cfg.Push.Enabled() &&
		!strings.HasPrefix(cfg.Push.Subject, "mailto:") &&
		!strings.HasPrefix(cfg.Push.Subject, "https://") {
		problems = append(problems, "VAPID_SUBJECT must be a mailto: address or an https URL")
	}

	// AI_PROVIDER is the older single-provider form. Honouring it as a
	// one-element chain keeps existing .env files and deployments working.
	//
	// The free floor is appended only when nothing was configured. An operator
	// who wrote a chain gets the chain they wrote — silently extending an
	// explicit list would send requests to a provider they did not name.
	if len(cfg.AI.Chain) == 0 {
		cfg.AI.Chain = append([]string{optional("AI_PROVIDER", "gemini")}, freeFloor...)
	}
	if len(cfg.AI.FreeChain) == 0 {
		cfg.AI.FreeChain = append([]string{}, freeFloor...)
	}

	// Only the names are checked here. Whether a named provider has its
	// credential is settled in providers.Build, which skips the ones that do
	// not and fails only if nothing usable is left — that way one chain can be
	// shared between a laptop with two keys and a deployment with five.
	//
	// Reported once per distinct name: the free chain defaults to the main one,
	// so a single typo would otherwise be listed twice.
	reported := make(map[string]bool)
	seenVariant := make(map[string]bool)
	for _, entry := range append(append([]string{}, cfg.AI.Chain...), cfg.AI.FreeChain...) {
		base, model := parseChainEntry(entry)

		if !knownProviders[base] {
			if !reported[base] {
				reported[base] = true
				problems = append(problems, fmt.Sprintf(
					"%q is not a known AI provider (%s)", base, strings.Join(knownProviderNames(), ", ")))
			}
			continue
		}

		if model == "" || seenVariant[entry] {
			continue
		}
		seenVariant[entry] = true

		// Only the OpenAI-dialect backends are one client with a swappable
		// model. Gemini is a different SDK, and nothing needs a pinned Gemini
		// model yet, so asking for one is a typo worth reporting rather than a
		// branch worth writing.
		if !compatibleProviders[base] {
			problems = append(problems, fmt.Sprintf(
				"%q pins a model to %q, which is not an OpenAI-dialect provider; only %s accept provider=model",
				entry, base, strings.Join(compatibleProviderNames(), ", ")))
			continue
		}

		cfg.AI.Variants = append(cfg.AI.Variants, ProviderVariant{Entry: entry, Base: base, Model: model})
	}

	// The free key is North's own, offered to every user who has nothing else.
	// Restricting it to models that bill nothing is what makes that safe by
	// construction rather than by convention: a paid model on this key would
	// charge North for anonymous traffic.
	if cfg.AI.OpenRouterFreeAPIKey != "" {
		for _, v := range cfg.AI.Variants {
			if v.Base == "openrouter" && !strings.HasSuffix(v.Model, ":free") {
				problems = append(problems, fmt.Sprintf(
					"OPENROUTER_FREE_API_KEY is set, so %q must name a %q model; as written it would bill Khepri's own account",
					v.Entry, ":free"))
			}
		}
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
// LogReady writes which providers will answer, and names any the chain asked
// for that were skipped because their credentials are missing.
//
// The usual footgun is AI_PROVIDER_CHAIN=hermes,fake with an empty
// HERMES_API_KEY: Hermes is skipped, fake becomes the default, and the coach
// replies with the fake string while MCP still works. That is not an error —
// a missing key is a normal laptop state — but it has to be loud at boot.
func (c AIConfig) LogReady(log *slog.Logger, r *ai.Registry) {
	if log == nil || r == nil {
		return
	}
	log.Info("ai providers ready",
		slog.String("default", r.DefaultName()),
		slog.Any("registered", r.Names()),
	)

	// providers.Build falls back to the fake client outside production rather
	// than refusing to start. That is a deliberate convenience and an easy
	// thing to spend an afternoon confused by, so it is stated in as many words
	// rather than left to be inferred from default=fake above.
	if r.DefaultName() == "fake" {
		log.Warn("no AI provider has its credentials set; the coach will reply with canned text",
			slog.Any("chain", c.Chain),
			slog.String("fix", "set OPENROUTER_API_KEY, or any other provider key named in AI_PROVIDER_CHAIN"),
		)
	}

	have := make(map[string]bool, len(r.Names()))
	for _, name := range r.Names() {
		have[name] = true
	}
	seen := make(map[string]bool)
	for _, name := range append(append([]string{}, c.Chain...), c.FreeChain...) {
		if name == "" || name == "fake" || have[name] || seen[name] {
			continue
		}
		seen[name] = true
		msg := "provider named in AI_PROVIDER_CHAIN was skipped (missing credentials)"
		if name == "hermes" {
			msg = "hermes named in AI_PROVIDER_CHAIN but HERMES_API_KEY or HERMES_BASE_URL is empty; coach will not use the gateway"
		}
		log.Warn(msg, slog.String("provider", name))
	}
}

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
//
// The environment is a parameter rather than a field on AIConfig because it
// decides one thing only: whether a chain that resolves to nothing is a warning
// or a refusal to start.
func (c AIConfig) ProviderOptions(env Environment) providers.Options {
	opts := providers.Options{
		Chain:         c.Chain,
		AllowFakeHead: !env.IsProduction(),
		GeminiAPIKey:  c.GeminiAPIKey,
		GeminiModel:   c.GeminiModel,
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

	// A variant is its base backend with one field changed. Copying the base
	// spec keeps the address, the credential, and the dialect quirks in one
	// place: nothing about OpenRouter is stated twice, so nothing about it can
	// drift.
	for _, v := range c.Variants {
		spec, ok := findCompatible(opts.Compatible, v.Base)
		if !ok {
			continue
		}

		spec.Name = v.Entry
		spec.Model = v.Model

		// Load has already established that with a free key set, every
		// OpenRouter variant is a ":free" model.
		if v.Base == "openrouter" && c.OpenRouterFreeAPIKey != "" {
			spec.APIKey = c.OpenRouterFreeAPIKey
		}

		opts.Compatible = append(opts.Compatible, spec)
	}

	return opts
}

// findCompatible returns the base spec a variant is derived from. Missing is
// not an error: Load has already rejected variants on providers that have no
// OpenAI-dialect spec.
func findCompatible(specs []providers.Compatible, name string) (providers.Compatible, bool) {
	for _, spec := range specs {
		if spec.Name == name {
			return spec, true
		}
	}
	return providers.Compatible{}, false
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
	return sortedNames(knownProviders)
}

// compatibleProviders is the subset of knownProviders that speak the OpenAI
// chat dialect, and so are one client with an interchangeable model. Only these
// can carry a "provider=model" variant.
var compatibleProviders = map[string]bool{
	"openrouter": true,
	"nvidia":     true,
	"xai":        true,
	"hermes":     true,
}

func compatibleProviderNames() []string {
	return sortedNames(compatibleProviders)
}

func sortedNames(set map[string]bool) []string {
	names := make([]string, 0, len(set))
	for name := range set {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// freeFloor is the last resort of every chain: OpenRouter models that cost
// nothing to call.
//
// It is the answer to "the coach must work on any machine". A checkout with one
// OpenRouter key — or a user with no key of their own, or an account that ran
// out of credit mid-conversation — still reaches a model, because these two
// cannot refuse on billing grounds. They are slower than the paid models above
// them in the chain, which is why they are last and not first.
//
// Free-tier slugs are retired and renamed by OpenRouter from time to time. When
// one stops resolving, the symptom is a 404 that ai.Failover walks past, not an
// outage.
var freeFloor = []string{
	"openrouter=z-ai/glm-5.2:free",
	"openrouter=nvidia/nemotron-3-ultra-550b-a55b:free",
}

// parseChainEntry splits a chain entry into its provider and its optional model
// override, returning an empty model for a plain provider name.
//
// The split is on the first "=" only. A model slug carries its own punctuation
// — "z-ai/glm-5.2:free" is a single name, not a nested key — so everything
// after the first separator belongs to the model.
func parseChainEntry(entry string) (base, model string) {
	base, model, found := strings.Cut(entry, "=")
	if !found {
		return strings.TrimSpace(entry), ""
	}
	return strings.TrimSpace(base), strings.TrimSpace(model)
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
