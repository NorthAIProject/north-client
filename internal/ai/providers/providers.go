// Package providers assembles the AI registry from configuration.
//
// It lives outside internal/ai on purpose. The ai package defines the interface
// and must not import its own implementations — beyond the import cycle that
// would create, an interface that knows its implementers is not an abstraction.
// Wiring is a startup concern, so it lives in its own package and is called
// once from main.
package providers

import (
	"context"
	"fmt"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/fake"
	"github.com/NorthAIProject/north-client/internal/ai/gemini"
	"github.com/NorthAIProject/north-client/internal/ai/openrouter"
)

// Options is what the registry needs to build itself, expressed without
// reference to config.Config so the ai layer stays independent of how North
// happens to read its environment.
type Options struct {
	// Default names the provider used when a caller does not ask for one.
	Default string

	// Model is the default model for the selected provider.
	Model string

	GeminiAPIKey string

	OpenRouterAPIKey  string
	OpenRouterSiteURL string
	OpenRouterSiteApp string
}

// Build constructs every provider whose credentials are present and makes the
// configured one the default.
//
// A provider without a key is skipped rather than failing the boot: having only
// a Gemini key is a normal state, and demanding four credentials to start the
// server would make North harder to run than it needs to be.
//
// The fake provider is always registered. It costs nothing, and it means the
// application can be run and demonstrated with no credentials at all.
func Build(ctx context.Context, opts Options) (*ai.Registry, error) {
	r := ai.NewRegistry()

	if opts.GeminiAPIKey != "" {
		client, err := gemini.New(ctx, gemini.Options{
			APIKey:       opts.GeminiAPIKey,
			DefaultModel: modelFor("gemini", opts),
		})
		if err != nil {
			return nil, fmt.Errorf("providers: build gemini: %w", err)
		}
		r.Register(client)
	}

	if opts.OpenRouterAPIKey != "" {
		client, err := openrouter.New(openrouter.Options{
			APIKey:       opts.OpenRouterAPIKey,
			DefaultModel: modelFor("openrouter", opts),
			SiteURL:      opts.OpenRouterSiteURL,
			SiteName:     opts.OpenRouterSiteApp,
		})
		if err != nil {
			return nil, fmt.Errorf("providers: build openrouter: %w", err)
		}
		r.Register(client)
	}

	r.Register(fake.Text(
		"This is the fake coach. Set AI_PROVIDER and the matching API key to talk to a real model.",
	))

	if err := r.SetDefault(opts.Default); err != nil {
		return nil, fmt.Errorf("%w (is the API key for %q set?)", err, opts.Default)
	}

	return r, nil
}

// modelFor returns the configured model only to the provider it was meant for.
//
// AI_MODEL holds one name, and that name belongs to whichever provider is
// selected. Handing "gemini-2.5-pro" to OpenRouter as its default would fail
// confusingly the first time someone switched providers without changing it.
func modelFor(name string, opts Options) string {
	if opts.Default == name {
		return opts.Model
	}
	return ""
}
