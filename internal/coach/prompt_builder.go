package coach

import (
	"context"
	"log/slog"

	"github.com/NorthAIProject/north-client/internal/ai/prompts"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
)

// contextHeading separates the persona and rules from the facts.
//
// The coach prompt refers to this block by name and tells the model it is the
// entire extent of its knowledge, so the heading is part of the contract rather
// than formatting.
const contextHeading = "\n\n## CONTEXT\n\n"

// PromptBuilder assembles system prompts.
//
// It is the only place that decides what the model is told. A handler that
// wanted to "just add a line to the prompt" would have to come here, which is
// the point: prompts drift when everyone can edit them.
type PromptBuilder struct{}

func NewPromptBuilder() *PromptBuilder { return &PromptBuilder{} }

// Coach returns the system prompt for a conversational reply: the persona and
// grounding rules, followed by everything North knows about this person.
func (p *PromptBuilder) Coach(cc *Context) (string, error) {
	base, err := prompts.Raw(prompts.CoachSystem)
	if err != nil {
		return "", err
	}
	return base + contextHeading + cc.Render(), nil
}

// Reflection is the system prompt for a structured reflection session.
func (p *PromptBuilder) Reflection(cc *Context, questionsAsked int) (string, error) {
	base, err := prompts.Render(prompts.ReflectionSession, map[string]any{
		"QuestionsAsked": questionsAsked,
	})
	if err != nil {
		return "", err
	}
	return base + contextHeading + cc.Render(), nil
}

// Titler returns the prompt that names a conversation from its opening message.
func (p *PromptBuilder) Titler(firstMessage string) (string, error) {
	return prompts.Render(prompts.ConversationTitle, map[string]any{"Message": firstMessage})
}

// logSourceFailure records a context source that could not contribute.
//
// Deliberately a warning rather than an error: the reply still happens, just
// with less to go on. It is worth seeing in the log because a source failing
// silently for a week would look like the coach quietly getting worse.
func logSourceFailure(ctx context.Context, name string, err error) {
	middleware.FromContext(ctx).Warn("context source failed",
		slog.String("source", name),
		slog.Any("error", err),
	)
}
