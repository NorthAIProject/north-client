package coach

import (
	"context"
	"log/slog"

	"github.com/NorthAIProject/north-client/internal/ai/prompts"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	"github.com/NorthAIProject/north-client/internal/users"
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
// grounding rules, the tone this person chose, then everything North knows
// about them.
func (p *PromptBuilder) Coach(cc *Context) (string, error) {
	base, err := prompts.Raw(prompts.CoachSystem)
	if err != nil {
		return "", err
	}
	tone, err := toneSection(cc)
	if err != nil {
		return "", err
	}
	return base + tone + contextHeading + cc.Render(), nil
}

// Reflection is the system prompt for a structured reflection session.
func (p *PromptBuilder) Reflection(cc *Context, questionsAsked int) (string, error) {
	base, err := prompts.Render(prompts.ReflectionSession, map[string]any{
		"QuestionsAsked": questionsAsked,
	})
	if err != nil {
		return "", err
	}
	tone, err := toneSection(cc)
	if err != nil {
		return "", err
	}
	return base + tone + contextHeading + cc.Render(), nil
}

// toneSection renders the chosen tone as an instruction.
//
// It sits above the CONTEXT block rather than inside it because a tone is an
// instruction to the model, not a fact about the person — the free-text
// "How they want to be coached" line stays in the context, where it reads as
// the nuance under this.
func toneSection(cc *Context) (string, error) {
	tone := users.ToneDefault
	if cc != nil && cc.User.CoachingTone.Valid() {
		tone = cc.User.CoachingTone
	}

	rendered, err := prompts.Render(prompts.CoachTone, map[string]any{"Tone": string(tone)})
	if err != nil {
		return "", err
	}
	return "\n\n" + rendered, nil
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
