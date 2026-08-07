// Package eval holds North's evaluations of real model behaviour.
//
// The tests here are the only ones that call a paid provider. They are guarded
// by the `live` build tag and run with `task test:live`, so an ordinary
// `go test ./...` never spends money and never fails because a provider is
// having a bad afternoon.
//
// What they check is not that the plumbing works — the offline tests cover
// that. They check the thing North's usefulness actually rests on: that the
// coach refuses to invent facts it was not given, and that structured output
// comes back in the shape the schema demanded.
package eval

import (
	"context"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/gemini"
	"github.com/NorthAIProject/north-client/internal/ai/openaicompat"
)

// timeout bounds a single live call. Generous, because a large model under load
// is slow, not broken.
const timeout = 2 * time.Minute

// compatible describes the OpenAI-dialect backends the evals can run against.
// A table rather than a switch, so a new backend is one line here and the list
// in the error message stays correct by construction.
var compatible = map[string]struct {
	keyEnv             string
	baseURLEnv         string
	defaultBaseURL     string
	defaultModel       string
	supportsJSONSchema bool
}{
	"openrouter": {"OPENROUTER_API_KEY", "OPENROUTER_BASE_URL", "https://openrouter.ai/api/v1", "anthropic/claude-sonnet-4.5", true},
	"nvidia":     {"NVIDIA_API_KEY", "NVIDIA_BASE_URL", "https://integrate.api.nvidia.com/v1", "meta/llama-3.3-70b-instruct", false},
	"xai":        {"XAI_API_KEY", "XAI_BASE_URL", "https://api.x.ai/v1", "grok-4.5", true},
	"hermes":     {"HERMES_API_KEY", "HERMES_BASE_URL", "", "hermes-3", false},
}

// Provider builds the client under test, skipping when its key is absent so a
// contributor with only one key can still run the evals they can afford.
func Provider(t *testing.T) ai.Client {
	t.Helper()

	name := strings.ToLower(os.Getenv("EVAL_PROVIDER"))
	if name == "" {
		name = "gemini"
	}

	if name == "gemini" {
		key := os.Getenv("GEMINI_API_KEY")
		if key == "" {
			t.Skip("GEMINI_API_KEY not set; skipping live evaluation")
		}
		model := envOr("EVAL_MODEL", "gemini-2.5-flash")
		c, err := gemini.New(context.Background(), gemini.Options{APIKey: key, DefaultModel: model})
		if err != nil {
			t.Fatalf("build gemini client: %v", err)
		}
		return c
	}

	spec, ok := compatible[name]
	if !ok {
		t.Fatalf("unknown EVAL_PROVIDER %q (want gemini, %s)", name, strings.Join(compatibleNames(), ", "))
		return nil
	}

	key := os.Getenv(spec.keyEnv)
	if key == "" {
		t.Skipf("%s not set; skipping live evaluation", spec.keyEnv)
	}
	baseURL := envOr(spec.baseURLEnv, spec.defaultBaseURL)
	if baseURL == "" {
		t.Skipf("%s not set; skipping live evaluation", spec.baseURLEnv)
	}

	c, err := openaicompat.New(openaicompat.Options{
		Name:               name,
		BaseURL:            baseURL,
		APIKey:             key,
		DefaultModel:       envOr("EVAL_MODEL", spec.defaultModel),
		SupportsJSONSchema: spec.supportsJSONSchema,
	})
	if err != nil {
		t.Fatalf("build %s client: %v", name, err)
	}
	return c
}

func compatibleNames() []string {
	names := make([]string, 0, len(compatible))
	for name := range compatible {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Context bounds a live call.
func Context(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	t.Cleanup(cancel)
	return ctx
}

// Collect drains a stream into a single string.
func Collect(ch <-chan ai.StreamChunk) (string, error) {
	var out strings.Builder
	for chunk := range ch {
		if chunk.Err != nil {
			return out.String(), chunk.Err
		}
		out.WriteString(chunk.Text)
	}
	return out.String(), nil
}

// ContainsAny reports whether s contains any of the phrases, case-insensitively.
//
// Model output is graded on substance rather than exact wording: a refusal can
// be phrased many ways and all of them are correct.
func ContainsAny(s string, phrases ...string) bool {
	lower := strings.ToLower(s)
	for _, p := range phrases {
		if strings.Contains(lower, strings.ToLower(p)) {
			return true
		}
	}
	return false
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
