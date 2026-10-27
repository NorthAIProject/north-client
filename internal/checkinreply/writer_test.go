package checkinreply_test

import (
	"context"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/fake"
	"github.com/NorthAIProject/north-client/internal/checkinreply"
	"github.com/NorthAIProject/north-client/internal/users"
)

// The note is one cheap call: the fast model, a small ceiling, no sampling
// parameters (Haiku 5.5 refuses a non-default temperature), and the person's
// own check-ins and language in the prompt.
func TestAIWriterAsksTheFastModelForAShortNote(t *testing.T) {
	t.Parallel()

	client := fake.Text("Boa caminhada.")
	registry := ai.NewRegistry()
	registry.Register(client)
	w := checkinreply.NewAIWriter(ai.NewRunner(registry, ai.NewChainSet([]string{client.Name()}, nil)), "fast-model")

	user := users.User{Locale: users.LocalePTPT, Tier: users.TierFree}
	got, err := w.Write(context.Background(), user, checkinreply.Prompt{
		Today:   "27 Oct — mood 3/5, energy 2/5",
		Recent:  []string{"26 Oct — mood 4/5, energy 4/5"},
		Pattern: "On days they rated sleep 4 or 5, energy averaged 4.3; on days they rated it 1 or 2, 2.0.",
	})
	if err != nil || got != "Boa caminhada." {
		t.Fatalf("got %q, %v", got, err)
	}

	req := client.LastCall()
	if req.Model != "fast-model" || req.MaxTokens != 120 || req.Temperature != nil {
		t.Fatalf("model %q, max tokens %d, temperature %v; want the fast model, 120, none", req.Model, req.MaxTokens, req.Temperature)
	}
	if len(req.Messages) != 1 || len(req.Messages[0].Parts) != 1 {
		t.Fatalf("want one user message, got %+v", req.Messages)
	}
	prompt := req.Messages[0].Parts[0].Text
	for _, want := range []string{"27 Oct — mood 3/5", "26 Oct — mood 4/5", "energy averaged 4.3", "European Portuguese"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}
