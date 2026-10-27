package checkinreply

import (
	"context"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/prompts"
	"github.com/NorthAIProject/north-client/internal/shared/aiattr"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/spend"
	"github.com/NorthAIProject/north-client/internal/users"
)

// writerMaxTokens is the backstop on a two-sentence note. It also keeps the
// Anthropic adapter's adaptive thinking off, which a note does not need.
const writerMaxTokens = 120

// AIWriter is the production Writer: one call to the fast model, walking the
// provider chain like the briefing does.
type AIWriter struct {
	runner *ai.Runner
	model  string
}

// NewAIWriter builds the writer. model may be empty for the chain's default;
// a fast model is enough for two sentences.
func NewAIWriter(runner *ai.Runner, model string) *AIWriter {
	return &AIWriter{runner: runner, model: model}
}

func (w *AIWriter) Write(ctx context.Context, user users.User, p Prompt) (string, error) {
	locale := users.LocaleDefault
	if user.Locale.Valid() {
		locale = user.Locale
	}
	prompt, err := prompts.Render(prompts.CheckInReply, map[string]any{
		"Language": locale.Language(),
		"Today":    p.Today,
		"Recent":   p.Recent,
		"Pattern":  p.Pattern,
	})
	if err != nil {
		return "", apperr.Wrap(err, "render the check-in reply prompt")
	}
	ctx = aiattr.WithUser(ctx, user.ID, spend.SurfaceCheckInReply)

	var text string
	_, err = w.runner.Run(ctx, ai.RunOptions{Tier: string(user.Tier)}, func(client ai.Client) error {
		// No temperature: Haiku 5.5 answers a non-default one with a 400.
		resp, genErr := client.Generate(ctx, ai.Request{
			Model:     w.model,
			Messages:  []ai.Message{ai.UserText(prompt)},
			MaxTokens: writerMaxTokens,
		})
		if genErr != nil {
			return apperr.Wrap(genErr, "write a check-in reply")
		}
		text = resp.Text
		return nil
	})
	return text, err
}
