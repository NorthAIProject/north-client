package config

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/fake"
)

func TestLogReadyWarnsWhenHermesIsNamedButMissing(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	r := ai.NewRegistry()
	r.Register(fake.Text("unused"))
	if err := r.SetDefault("fake"); err != nil {
		t.Fatal(err)
	}

	AIConfig{Chain: []string{"hermes", "fake"}}.LogReady(log, r)

	out := buf.String()
	if !strings.Contains(out, "ai providers ready") {
		t.Fatalf("missing ready line: %s", out)
	}
	if !strings.Contains(out, "HERMES_API_KEY") {
		t.Fatalf("missing hermes skip warning: %s", out)
	}
}

func TestLogReadyIsQuietWhenNamedProvidersAreRegistered(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	r := ai.NewRegistry()
	r.Register(fake.Text("unused"))
	if err := r.SetDefault("fake"); err != nil {
		t.Fatal(err)
	}

	AIConfig{Chain: []string{"fake"}}.LogReady(log, r)

	if strings.Contains(buf.String(), "skipped") || strings.Contains(buf.String(), "HERMES_API_KEY") {
		t.Fatalf("unexpected warning: %s", buf.String())
	}
}

func TestAnthropicLeadsBothChainsWhenSwitchedOn(t *testing.T) {
	cfg, err := loadWith(t, map[string]string{
		"AI_PROVIDER_CHAIN":      "openrouter,anthropic,nvidia",
		"AI_PROVIDER_CHAIN_FREE": "nvidia,hermes",
		"ANTHROPIC_API_KEY":      "sk-ant-test",
		"ANTHROPIC_FIRST":        "true",
	})
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	// Named once, at the head: a provider that refused is not asked again.
	if got := strings.Join(cfg.AI.Chain, ","); got != "anthropic,openrouter,nvidia" {
		t.Errorf("chain = %s, want anthropic,openrouter,nvidia", got)
	}
	// Scope defaults to all, so free users get it too.
	if got := strings.Join(cfg.AI.FreeChain, ","); got != "anthropic,nvidia,hermes" {
		t.Errorf("free chain = %s, want anthropic,nvidia,hermes", got)
	}

	a := cfg.AI.Anthropic
	if a.Model != "claude-haiku-5-5" || a.Thinking != "adaptive" || a.Effort != "" || a.Scope != "all" {
		t.Errorf("defaults = %+v", a)
	}
}

func TestAnthropicScopePaidLeavesTheFreeChainAlone(t *testing.T) {
	cfg, err := loadWith(t, map[string]string{
		"ANTHROPIC_API_KEY": "sk-ant-test",
		"ANTHROPIC_FIRST":   "true",
		"ANTHROPIC_SCOPE":   "paid",
	})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.AI.Chain[0] != "anthropic" {
		t.Errorf("chain = %v, want anthropic first", cfg.AI.Chain)
	}
	// The defaults are still applied underneath: AI_PROVIDER, then the floor.
	if len(cfg.AI.Chain) != 2+len(freeFloor) || cfg.AI.Chain[1] != "gemini" {
		t.Errorf("chain = %v, want anthropic in front of the default chain", cfg.AI.Chain)
	}
	if len(cfg.AI.FreeChain) != len(freeFloor) || cfg.AI.FreeChain[0] == "anthropic" {
		t.Errorf("free chain = %v, want the floor only", cfg.AI.FreeChain)
	}
}

// Either half missing is today's configuration: the switch without a key, or
// a sealed key with the switch off. The second is the rollback.
func TestAnthropicChangesNothingUnlessKeyAndSwitchAreBothSet(t *testing.T) {
	cases := map[string]map[string]string{
		"switch, no key":  {"ANTHROPIC_FIRST": "true"},
		"key, no switch":  {"ANTHROPIC_API_KEY": "sk-ant-test"},
		"key, switch off": {"ANTHROPIC_API_KEY": "sk-ant-test", "ANTHROPIC_FIRST": "false"},
	}
	for name, env := range cases {
		t.Run(name, func(t *testing.T) {
			env["AI_PROVIDER_CHAIN"] = "openrouter,nvidia"
			env["AI_PROVIDER_CHAIN_FREE"] = "nvidia"
			cfg, err := loadWith(t, env)
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			if got := strings.Join(cfg.AI.Chain, ","); got != "openrouter,nvidia" {
				t.Errorf("chain = %s, want openrouter,nvidia", got)
			}
			if got := strings.Join(cfg.AI.FreeChain, ","); got != "nvidia" {
				t.Errorf("free chain = %s, want nvidia", got)
			}
		})
	}
}

func TestAnthropicSettingsAreValidated(t *testing.T) {
	cases := map[string]map[string]string{
		"effort":               {"ANTHROPIC_EFFORT": "lowest"},
		"thinking":             {"ANTHROPIC_THINKING": "enabled"},
		"scope":                {"ANTHROPIC_SCOPE": "free"},
		"first":                {"ANTHROPIC_FIRST": "yes please"},
		"openrouter model":     {"ANTHROPIC_MODEL": "anthropic/claude-haiku-5-5"},
		"no thinking at xhigh": {"ANTHROPIC_THINKING": "disabled", "ANTHROPIC_EFFORT": "xhigh"},
	}
	for name, env := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := loadWith(t, env); err == nil {
				t.Errorf("%v was accepted", env)
			}
		})
	}
}

func TestProviderOptionsCarriesTheAnthropicSettings(t *testing.T) {
	cfg, err := loadWith(t, map[string]string{
		"ANTHROPIC_API_KEY":  "sk-ant-test",
		"ANTHROPIC_EFFORT":   "low",
		"ANTHROPIC_THINKING": "disabled",
	})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	got := cfg.AI.ProviderOptions(EnvDevelopment).Anthropic
	if got.APIKey != "sk-ant-test" || got.Model != "claude-haiku-5-5" || got.Effort != "low" || got.Thinking != "disabled" {
		t.Errorf("options = %+v", got)
	}
}
