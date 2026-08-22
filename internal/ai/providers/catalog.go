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

	// VerifyPath is a cheap GET, relative to BaseURL, that answers 401 or 403
	// when the key is wrong. It exists to tell somebody their key is bad while
	// they are still looking at the form.
	//
	// Empty means no such endpoint was found for this provider, and the key is
	// stored unverified. That is the honest state for three of the five:
	//
	//   openrouter  /key      401 on a bad key. NOT /models, which is public
	//                         and answers 200 to anything.
	//   openai      /models   401 on a bad key.
	//   nvidia      —         /models is public; answers 200 to anything.
	//   xai         —         /models and /api-key both answer 400, which is
	//                         indistinguishable from a malformed request, so
	//                         treating it as rejection would refuse good keys.
	//   gemini      —         not an OpenAI-dialect backend; no equivalent
	//                         endpoint under a shared base URL.
	//
	// Each of these was probed with a deliberately invalid key rather than
	// assumed. Re-probe before adding one.
	VerifyPath string

	SupportsJSONSchema bool

	// Note is one line under the option in the settings page.
	Note string

	// RequiresBaseURL means this backend has no public address: the user
	// names theirs (their Hermes gateway). The URL is validated before
	// Khepri will dial it; see ParseGatewayURL.
	RequiresBaseURL bool
}

// Catalog is deliberately a different list from config.knownProviders.
//
// That one answers "which providers can this server build from its own
// environment", and includes the fake client and the self-hosted Hermes
// gateway — both of which belong to Khepri rather than to a user. This one
// answers "which providers may a person point their own key at".
var Catalog = []BYOProvider{
	{
		Name: "openrouter", Label: "OpenRouter",
		BaseURL: "https://openrouter.ai/api/v1", DefaultModel: "anthropic/claude-sonnet-4.5",
		KeyHint: "sk-or-v1-…", VerifyPath: "/key", SupportsJSONSchema: true,
		Note: "One key, most models — including Claude and GPT. The simplest choice.",
	},
	{
		Name: "openai", Label: "OpenAI",
		BaseURL: "https://api.openai.com/v1", DefaultModel: "gpt-4.1",
		KeyHint: "sk-…", VerifyPath: "/models", SupportsJSONSchema: true,
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
		Note:    "Free models, and what Khepri's own free tier already runs on.",
	},
	{
		// No VerifyPath: Gemini is not an OpenAI-dialect backend and has no
		// equivalent endpoint under a shared base URL, so its keys are stored
		// without a live check.
		Name: "gemini", Label: "Google Gemini",
		DefaultModel: "gemini-2.5-pro", KeyHint: "AIza…",
	},
	{
		// BaseURL is supplied per user: each person runs their own Hermes
		// gateway. The catalogue cannot name one address.
		Name: "hermes", Label: "Hermes (your gateway)",
		DefaultModel: "hermes-3", KeyHint: "gateway API_SERVER_KEY",
		VerifyPath: "/models", RequiresBaseURL: true,
		Note: "Your own Hermes instance — tailnet, LAN, or public. URL and key stay on this account.",
	},
}

// ByName finds a catalogue entry.
//
// Anthropic is deliberately absent. Its native API is a different dialect, and
// its OpenAI-compatibility endpoint is a documented shim with parity caveats —
// not something to ship on the strength of a plan. Claude is reachable today
// through OpenRouter with a model beginning "anthropic/", which is how Khepri
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
type UserSpec struct {
	Provider string
	APIKey   string

	// Model may be empty, meaning the catalogue's default.
	Model string

	// BaseURL is set only for providers with RequiresBaseURL (Hermes).
	// Catalogue URLs are used otherwise; a value here for OpenRouter is ignored.
	BaseURL string

	// Meter records what this client spends, marked as the user's own rather
	// than ours. Optional; nil leaves the client unwrapped.
	Meter ai.Meter

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

	baseURL := entry.BaseURL
	httpClient := spec.HTTPClient
	if entry.RequiresBaseURL {
		parsed, err := ParseGatewayURL(spec.BaseURL)
		if err != nil {
			return nil, fmt.Errorf("providers: cannot build a %s client for this credential", entry.Name)
		}
		baseURL = parsed
		httpClient = GatewayHTTPClient(spec.HTTPClient)
	}

	if entry.Name == "gemini" {
		client, err := gemini.New(ctx, gemini.Options{APIKey: spec.APIKey, DefaultModel: model})
		if err != nil {
			// Not wrapped with the spec: it holds the key.
			return nil, fmt.Errorf("providers: cannot build a gemini client for this credential")
		}
		// byok: the tokens are real and worth recording, but the bill is the
		// user's. Pricing them with our rates would overstate what we spend on
		// precisely the accounts that cost us least.
		return ai.Metered(client, spec.Meter, true), nil
	}

	client, err := openaicompat.New(openaicompat.Options{
		Name:               entry.Name,
		BaseURL:            baseURL,
		APIKey:             spec.APIKey,
		DefaultModel:       model,
		SupportsJSONSchema: entry.SupportsJSONSchema,
		HTTPClient:         httpClient,
	})
	if err != nil {
		return nil, fmt.Errorf("providers: cannot build a %s client for this credential", entry.Name)
	}
	return ai.Metered(client, spec.Meter, true), nil
}
