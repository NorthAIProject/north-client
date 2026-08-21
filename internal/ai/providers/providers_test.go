package providers_test

import (
	"context"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/ai/providers"
)

// compatSpec is a credentialled OpenAI-dialect backend. Build only checks that
// a key and an address are present, so no request is ever made.
func compatSpec(name, model string) providers.Compatible {
	return providers.Compatible{
		Name:    name,
		BaseURL: "https://example.test/v1",
		APIKey:  "sk-test",
		Model:   model,
	}
}

func TestBuildRegistersAVariantUnderItsChainEntry(t *testing.T) {
	// The entry string is the registry key. That is what lets Resolve find a
	// variant with no lookup table, so it is worth pinning down.
	const entry = "openrouter=z-ai/glm-5.2:free"

	r, err := providers.Build(context.Background(), providers.Options{
		Chain: []string{"openrouter", entry},
		Compatible: []providers.Compatible{
			compatSpec("openrouter", "anthropic/claude-sonnet-4.5"),
			compatSpec(entry, "z-ai/glm-5.2:free"),
		},
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	client, err := r.Get(entry)
	if err != nil {
		t.Fatalf("variant not registered: %v", err)
	}
	if client.Name() != entry {
		t.Errorf("name = %q, want the entry verbatim", client.Name())
	}

	// Both must survive as distinct providers, or the chain cannot express
	// "the same backend twice at different price points".
	if got := r.Resolve([]string{"openrouter", entry}); len(got) != 2 {
		t.Errorf("resolved %d providers, want 2", len(got))
	}
}

func TestBuildFailsInProductionWhenNoProviderHasCredentials(t *testing.T) {
	_, err := providers.Build(context.Background(), providers.Options{
		Chain:         []string{"openrouter"},
		AllowFakeHead: false,
		Compatible:    []providers.Compatible{{Name: "openrouter", BaseURL: "https://example.test/v1"}},
	})
	if err == nil {
		t.Fatal("a production boot with no usable provider must fail")
	}
	if !strings.Contains(err.Error(), "AI_PROVIDER_CHAIN") {
		t.Errorf("the error should say what to fix: %v", err)
	}
}

func TestBuildFallsBackToFakeOutsideProduction(t *testing.T) {
	// A checkout with no credentials still has to start, or a developer who is
	// not working on the AI cannot run the application at all.
	r, err := providers.Build(context.Background(), providers.Options{
		Chain:         []string{"openrouter"},
		AllowFakeHead: true,
		Compatible:    []providers.Compatible{{Name: "openrouter", BaseURL: "https://example.test/v1"}},
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if r.DefaultName() != "fake" {
		t.Errorf("default = %q, want fake", r.DefaultName())
	}
}

func TestBuildPrefersTheChainHeadOverTheFirstRegistered(t *testing.T) {
	// Registration order is an implementation detail of Build; the chain is the
	// stated preference, and the fallback must not quietly promote a provider
	// the chain left out.
	r, err := providers.Build(context.Background(), providers.Options{
		Chain: []string{"xai"},
		Compatible: []providers.Compatible{
			compatSpec("openrouter", "anthropic/claude-sonnet-4.5"),
			compatSpec("xai", "grok-4.5"),
		},
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if r.DefaultName() != "xai" {
		t.Errorf("default = %q, want xai", r.DefaultName())
	}
}
