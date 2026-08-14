package providers

import (
	"context"
	"fmt"
	"net/http"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/gemini"
	"github.com/NorthAIProject/north-client/internal/ai/openaicompat"
)

// BYOProvider is one backend a user may bring their own key for.
//
// Data, not configuration: each entry needs a base URL and a dialect somebody
// verified, so adding one is a code change rather than an environment
// variable. That is also why there is no "custom, enter your own base URL"
// entry — see UserSpec.
type BYOProvider struct {
	// Name is what user_ai_credentials.provider stores.
	Name string

	// Label is how it is written for a person.
	Label string

	// BaseURL is empty for Gemini, which is not an OpenAI-dialect backend and
	// is reached through its own SDK.
	BaseURL string

	// DefaultModel is used when the user leaves the model blank. It exists
	// because openaicompat.New refuses an empty model, so "use the provider's
	// default" has to be spelled out somewhere.
	DefaultModel string

	// KeyHint is the shape of the credential, shown as the field placeholder
	// so somebody can tell at a glance whether they pasted the right thing.
	KeyHint string

	SupportsJSONSchema bool

	// Note is one line under the option in the settings page.
	Note string
}

// Catalog is deliberately a different list from config.knownProviders.
//
// That one answers "which providers can this server build from its own
// environment", and includes the fake client and the self-hosted Hermes
// gateway — both of which belong to North rather than to a user. This one
// answers "which providers may a person point their own key at".
var Catalog = []BYOProvider{
	{
		Name: "openrouter", Label: "OpenRouter",
		BaseURL: "https://openrouter.ai/api/v1", DefaultModel: "anthropic/claude-sonnet-4.5",
		KeyHint: "sk-or-v1-…", SupportsJSONSchema: true,
		Note: "One key, most models — including Claude and GPT. The simplest choice.",
	},
	{
		Name: "openai", Label: "OpenAI",
		BaseURL: "https://api.openai.com/v1", DefaultModel: "gpt-4.1",
		KeyHint: "sk-…", SupportsJSONSchema: true,
		Note: "Needs no new code: openaicompat speaks OpenAI's dialect already.",
	},
	{
		Name: "xai", Label: "xAI (Grok)",
		BaseURL: "https://api.x.ai/v1", DefaultModel: "grok-4",
		KeyHint: "xai-…", SupportsJSONSchema: true,
	},
	{
		Name: "nvidia", Label: "NVIDIA",
		BaseURL: "https://integrate.api.nvidia.com/v1", DefaultModel: "meta/llama-3.3-70b-instruct",
		KeyHint: "nvapi-…",
		Note:    "Free models, and what North's own free tier already runs on.",
	},
	{
		Name: "gemini", Label: "Google Gemini",
		DefaultModel: "gemini-2.5-pro", KeyHint: "AIza…",
	},
}

// ByName finds a catalogue entry.
//
// Anthropic is deliberately absent. Its native API is a different dialect, and
// its OpenAI-compatibility endpoint is a documented shim with parity caveats —
// not something to ship on the strength of a plan. Claude is reachable today
// through OpenRouter with a model beginning "anthropic/", which is how North
// reaches it already.
func ByName(name string) (BYOProvider, bool) {
	for _, p := range Catalog {
		if p.Name == name {
			return p, true
		}
	}
	return BYOProvider{}, false
}

// UserSpec is one person's credential, ready to become a client.
//
// There is no BaseURL field, and that is a security decision rather than an
// oversight. A user-supplied base URL makes North's server issue an outbound
// request to an address that user chose — into the cluster's own services, the
// cloud metadata endpoint, or a database port. Supporting it needs https-only,
// resolution followed by rejection of private ranges, a DialContext that
// re-checks the resolved address at connect time to defeat rebinding, and no
// redirect following. Until that exists, the URL comes from the catalogue.
type UserSpec struct {
	Provider string
	APIKey   string

	// Model may be empty, meaning the catalogue's default.
	Model string

	// HTTPClient is shared across every user's client so a per-user provider
	// gets connection reuse rather than a TLS handshake per coach turn.
	// openaicompat.New builds a fresh Transport when this is nil, which is the
	// detail that would otherwise make per-request construction expensive.
	HTTPClient *http.Client
}

// User builds one client from a credential a person brought themselves.
//
// Deliberately not a registry. ai.Registry is built once at startup and read
// without locking — "Registering after the server is serving would be a bug,
// not a feature" — and a per-user client registered into it would be precisely
// that bug. This is the same construction Build performs, handing back the
// client instead of storing it.
func User(ctx context.Context, spec UserSpec) (ai.Client, error) {
	entry, ok := ByName(spec.Provider)
	if !ok {
		return nil, fmt.Errorf("providers: %q is not a provider a key can be brought for", spec.Provider)
	}

	model := spec.Model
	if model == "" {
		model = entry.DefaultModel
	}

	if entry.Name == "gemini" {
		client, err := gemini.New(ctx, gemini.Options{APIKey: spec.APIKey, DefaultModel: model})
		if err != nil {
			// Not wrapped with the spec: it holds the key.
			return nil, fmt.Errorf("providers: cannot build a gemini client for this credential")
		}
		return client, nil
	}

	client, err := openaicompat.New(openaicompat.Options{
		Name:               entry.Name,
		BaseURL:            entry.BaseURL,
		APIKey:             spec.APIKey,
		DefaultModel:       model,
		SupportsJSONSchema: entry.SupportsJSONSchema,
		HTTPClient:         spec.HTTPClient,
	})
	if err != nil {
		return nil, fmt.Errorf("providers: cannot build a %s client for this credential", entry.Name)
	}
	return client, nil
}
