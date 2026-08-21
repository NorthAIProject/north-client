package config

import (
	"strings"
	"testing"
)

// loadWith runs Load with DATABASE_URL satisfied and the given AI settings.
//
// t.Setenv restores the previous value and forbids t.Parallel, which is what
// makes reading process-wide environment safe to test at all.
func loadWith(t *testing.T, env map[string]string) (*Config, error) {
	t.Helper()

	t.Setenv("DATABASE_URL", "postgres://localhost/test")

	// Cleared so a developer's own .env, loaded by godotenv in the binaries but
	// not here, cannot decide the outcome of an assertion.
	for _, key := range []string{
		"AI_PROVIDER", "AI_PROVIDER_CHAIN", "AI_PROVIDER_CHAIN_FREE",
		"OPENROUTER_API_KEY", "OPENROUTER_FREE_API_KEY",
	} {
		t.Setenv(key, "")
	}
	for key, value := range env {
		t.Setenv(key, value)
	}

	return Load()
}

func TestParseChainEntrySplitsOnTheFirstEqualsOnly(t *testing.T) {
	// The model slug is the interesting case: it carries both a slash and a
	// colon, and an over-eager split would truncate it to "z-ai/glm-5.2".
	base, model := parseChainEntry("openrouter=z-ai/glm-5.2:free")
	if base != "openrouter" {
		t.Errorf("base = %q, want openrouter", base)
	}
	if model != "z-ai/glm-5.2:free" {
		t.Errorf("model = %q, want z-ai/glm-5.2:free", model)
	}
}

func TestParseChainEntryLeavesAPlainProviderWithoutAModel(t *testing.T) {
	base, model := parseChainEntry(" hermes ")
	if base != "hermes" {
		t.Errorf("base = %q, want hermes", base)
	}
	if model != "" {
		t.Errorf("model = %q, want empty", model)
	}
}

func TestChainVariantsAreCollectedFromBothChains(t *testing.T) {
	cfg, err := loadWith(t, map[string]string{
		"AI_PROVIDER_CHAIN":      "xai,openrouter=z-ai/glm-5.2:free",
		"AI_PROVIDER_CHAIN_FREE": "openrouter=z-ai/glm-5.2:free,nvidia=meta/llama-3.3-70b-instruct",
	})
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	// The GLM entry appears in both chains and must be collected once: a
	// duplicate would register the same client name twice.
	if len(cfg.AI.Variants) != 2 {
		t.Fatalf("variants = %+v, want 2", cfg.AI.Variants)
	}
	if cfg.AI.Variants[0].Entry != "openrouter=z-ai/glm-5.2:free" {
		t.Errorf("first variant = %+v", cfg.AI.Variants[0])
	}
	if cfg.AI.Variants[0].Model != "z-ai/glm-5.2:free" {
		t.Errorf("first variant model = %q", cfg.AI.Variants[0].Model)
	}
	if cfg.AI.Variants[1].Base != "nvidia" {
		t.Errorf("second variant = %+v", cfg.AI.Variants[1])
	}
}

func TestChainRejectsAModelPinnedToANonCompatibleProvider(t *testing.T) {
	_, err := loadWith(t, map[string]string{
		"AI_PROVIDER_CHAIN": "gemini=gemini-2.5-flash",
	})
	if err == nil {
		t.Fatal("expected an error for a pinned Gemini model")
	}
	if !strings.Contains(err.Error(), "not an OpenAI-dialect provider") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestChainValidatesTheBaseNameNotTheWholeEntry(t *testing.T) {
	_, err := loadWith(t, map[string]string{
		"AI_PROVIDER_CHAIN": "nosuchvendor=some/model",
	})
	if err == nil {
		t.Fatal("expected an error for an unknown provider")
	}
	if !strings.Contains(err.Error(), `"nosuchvendor" is not a known AI provider`) {
		t.Fatalf("error should name the base, not the entry: %v", err)
	}
}

func TestFreeFloorIsAppendedOnlyWhenNoChainWasConfigured(t *testing.T) {
	cfg, err := loadWith(t, map[string]string{})
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	// Nothing configured: the legacy single-provider default, then the floor.
	// This is the state a fresh checkout is in, and it has to reach a model.
	if len(cfg.AI.Chain) != 1+len(freeFloor) {
		t.Fatalf("chain = %v, want gemini plus the floor", cfg.AI.Chain)
	}
	if cfg.AI.Chain[len(cfg.AI.Chain)-1] != freeFloor[len(freeFloor)-1] {
		t.Errorf("chain does not end with the floor: %v", cfg.AI.Chain)
	}

	// The free tier gets the floor and nothing else.
	if len(cfg.AI.FreeChain) != len(freeFloor) {
		t.Errorf("free chain = %v, want exactly the floor", cfg.AI.FreeChain)
	}
}

func TestAnExplicitChainIsNotExtendedWithTheFreeFloor(t *testing.T) {
	// Silently appending would send requests to a provider the operator did not
	// name, which is the one thing a hand-written chain is for.
	cfg, err := loadWith(t, map[string]string{
		"AI_PROVIDER_CHAIN": "hermes",
	})
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if len(cfg.AI.Chain) != 1 || cfg.AI.Chain[0] != "hermes" {
		t.Fatalf("chain = %v, want [hermes] exactly", cfg.AI.Chain)
	}
}

func TestFreeKeyRejectsAPaidModel(t *testing.T) {
	// The whole point of the separate key: it answers for users who brought
	// nothing, so it must not be attachable to a model that bills.
	_, err := loadWith(t, map[string]string{
		"AI_PROVIDER_CHAIN":       "openrouter=anthropic/claude-sonnet-4.5",
		"OPENROUTER_FREE_API_KEY": "sk-or-test",
	})
	if err == nil {
		t.Fatal("expected an error for a paid model on the free key")
	}
	if !strings.Contains(err.Error(), "bill") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFreeKeyAcceptsTheFreeFloor(t *testing.T) {
	if _, err := loadWith(t, map[string]string{
		"OPENROUTER_FREE_API_KEY": "sk-or-test",
	}); err != nil {
		t.Fatalf("the default floor must be valid on the free key: %v", err)
	}
}

func TestVariantsInheritTheirBaseSpecAndTheFreeKey(t *testing.T) {
	cfg, err := loadWith(t, map[string]string{
		"AI_PROVIDER_CHAIN":       "openrouter,openrouter=z-ai/glm-5.2:free",
		"OPENROUTER_API_KEY":      "sk-or-paid",
		"OPENROUTER_FREE_API_KEY": "sk-or-free",
	})
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	opts := cfg.AI.ProviderOptions(EnvDevelopment)

	spec, ok := findCompatible(opts.Compatible, "openrouter=z-ai/glm-5.2:free")
	if !ok {
		t.Fatal("variant was not rendered as a provider spec")
	}
	if spec.Model != "z-ai/glm-5.2:free" {
		t.Errorf("model = %q", spec.Model)
	}
	if spec.APIKey != "sk-or-free" {
		t.Errorf("variant should be funded by the free key, got %q", spec.APIKey)
	}
	// Inherited rather than restated, so the address cannot drift from the base.
	if spec.BaseURL != "https://openrouter.ai/api/v1" {
		t.Errorf("base url = %q", spec.BaseURL)
	}

	base, ok := findCompatible(opts.Compatible, "openrouter")
	if !ok {
		t.Fatal("base provider is missing")
	}
	if base.APIKey != "sk-or-paid" {
		t.Errorf("the base must keep its own key, got %q", base.APIKey)
	}
}

func TestProviderOptionsAllowsTheFakeHeadOutsideProductionOnly(t *testing.T) {
	cfg, err := loadWith(t, map[string]string{})
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if !cfg.AI.ProviderOptions(EnvDevelopment).AllowFakeHead {
		t.Error("development should tolerate a chain with no credentials")
	}
	if cfg.AI.ProviderOptions(EnvProduction).AllowFakeHead {
		t.Error("production must not boot onto the fake coach")
	}
}
