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
	"strings"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/gemini"
	"github.com/NorthAIProject/north-client/internal/ai/openrouter"
)

// timeout bounds a single live call. Generous, because a large model under load
// is slow, not broken.
const timeout = 2 * time.Minute

// Provider builds the client under test, skipping when its key is absent so a
// contributor with only one key can still run the evals they can afford.
func Provider(t *testing.T) ai.Client {
	t.Helper()

	switch strings.ToLower(os.Getenv("EVAL_PROVIDER")) {
	case "", "gemini":
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

	case "openrouter":
		key := os.Getenv("OPENROUTER_API_KEY")
		if key == "" {
			t.Skip("OPENROUTER_API_KEY not set; skipping live evaluation")
		}
		model := envOr("EVAL_MODEL", "anthropic/claude-sonnet-4.5")
		c, err := openrouter.New(openrouter.Options{APIKey: key, DefaultModel: model})
		if err != nil {
			t.Fatalf("build openrouter client: %v", err)
		}
		return c

	default:
		t.Fatalf("unknown EVAL_PROVIDER %q (want gemini or openrouter)", os.Getenv("EVAL_PROVIDER"))
		return nil
	}
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
