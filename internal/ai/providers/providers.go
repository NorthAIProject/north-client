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
	"github.com/NorthAIProject/north-client/internal/ai/openaicompat"
)

// Options is what the registry needs to build itself, expressed without
// reference to config.Config so the ai layer stays independent of how North
// happens to read its environment.
type Options struct {
	// Chain is the ordered list of providers to try, most preferred first. Its
	// head becomes the registry default.
	Chain []string

	// AllowFakeHead lets a chain that resolves to nothing fall back to the fake
	// client instead of failing the boot. Set outside production, where a
	// checkout with no credentials at all should still start and be clickable.
	//
	// In production the same state is a misconfiguration: a server that boots
	// and answers every user as the fake coach looks alive and is not.
	AllowFakeHead bool

	GeminiAPIKey string
	GeminiModel  string

	// Compatible holds every backend that speaks the OpenAI chat dialect —
	// OpenRouter, NVIDIA, xAI, and a self-hosted Hermes gateway are all the
	// same client with different settings.
	Compatible []Compatible
}

// Compatible describes one OpenAI-dialect backend.
type Compatible struct {
	Name               string
	BaseURL            string
	APIKey             string
	Model              string
	Headers            map[string]string
	SupportsJSONSchema bool
}

// Build constructs every provider whose credentials are present and makes the
// head of the chain the default.
//
// A provider without a key is skipped rather than failing the boot: having only
// a Gemini key is a normal state, and demanding five credentials to start the
// server would make North harder to run than it needs to be.
//
// The fake provider is always registered. It costs nothing, and it means the
// application can be run and demonstrated with no credentials at all.
func Build(ctx context.Context, opts Options) (*ai.Registry, error) {
	r := ai.NewRegistry()

	if opts.GeminiAPIKey != "" {
		client, err := gemini.New(ctx, gemini.Options{
			APIKey:       opts.GeminiAPIKey,
			DefaultModel: opts.GeminiModel,
		})
		if err != nil {
			return nil, fmt.Errorf("providers: build gemini: %w", err)
		}
		r.Register(client)
	}

	for _, spec := range opts.Compatible {
		// A backend is configured by its key and its address together. Hermes
		// in particular has no public endpoint to fall back on.
		if spec.APIKey == "" || spec.BaseURL == "" {
			continue
		}

		client, err := openaicompat.New(openaicompat.Options{
			Name:               spec.Name,
			BaseURL:            spec.BaseURL,
			APIKey:             spec.APIKey,
			DefaultModel:       spec.Model,
			Headers:            spec.Headers,
			SupportsJSONSchema: spec.SupportsJSONSchema,
		})
		if err != nil {
			return nil, fmt.Errorf("providers: build %s: %w", spec.Name, err)
		}
		r.Register(client)
	}

	r.Register(fake.Text(
		"This is the fake coach. Set AI_PROVIDER and the matching API key to talk to a real model.",
	))

	// The chain may name providers whose keys are absent; those were skipped
	// above and Resolve drops them. A chain with nothing left in it is the one
	// state worth deciding about, because every AI call would otherwise fail at
	// runtime rather than at boot.
	//
	// Outside production that is a laptop with no credentials, and refusing to
	// start makes the whole application unrunnable over a feature the developer
	// may not be working on. The fake client is always registered, so there is
	// something to fall back to; LogReady says so loudly. In production the
	// same state means a deployment shipped without its keys, and a server
	// answering every user as the fake coach is worse than one that never
	// started.
	usable := r.Resolve(opts.Chain)
	if len(usable) == 0 {
		if !opts.AllowFakeHead {
			return nil, fmt.Errorf(
				"providers: no provider in AI_PROVIDER_CHAIN %v has its credentials set (registered: %v)",
				opts.Chain, r.Names(),
			)
		}
		// Named explicitly rather than taken from the registry default, which
		// is whichever client registered first — possibly a real provider the
		// chain deliberately left out.
		fakeClient, err := r.Get("fake")
		if err != nil {
			return nil, fmt.Errorf("providers: no usable provider and no fake to fall back to: %w", err)
		}
		usable = []ai.Client{fakeClient}
	}

	if err := r.SetDefault(usable[0].Name()); err != nil {
		return nil, fmt.Errorf("providers: set default: %w", err)
	}

	return r, nil
}
