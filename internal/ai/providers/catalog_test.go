package providers_test

import (
	"context"
	"testing"

	"github.com/NorthAIProject/north-client/internal/ai/providers"
)

func TestAnthropicIsAProviderAKeyCanBeBroughtFor(t *testing.T) {
	entry, ok := providers.ByName("anthropic")
	if !ok {
		t.Fatal("anthropic is not in the catalogue")
	}
	if entry.DefaultModel != "claude-opus-5" || entry.KeyHeader != "x-api-key" || entry.VerifyPath != "/v1/models" {
		t.Errorf("entry = %+v", entry)
	}

	client, err := providers.User(context.Background(), providers.UserSpec{Provider: "anthropic", APIKey: "sk-ant-test"})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if client.Name() != "anthropic" {
		t.Errorf("built %q", client.Name())
	}
}
