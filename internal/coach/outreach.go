package coach

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/prompts"
	"github.com/NorthAIProject/north-client/internal/conversations"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/spend"
	"github.com/NorthAIProject/north-client/internal/users"
)

// RunOutreach writes the coach's one unprompted message of the day and posts
// it into the person's latest chat, captioned as a coach check-in.
//
// situation is what the coach noticed and what was done or offered, already
// worded by internal/outreach; the model only turns it into the coach's voice,
// with the person's context behind it. Same persona, context and provider
// chain as RunStandingTask, and like it, no tools: the outreach service has
// already acted, and the person answers with buttons rather than words.
//
// An error leaves nothing posted, so the caller can post its own fallback.
func (s *Service) RunOutreach(ctx context.Context, user users.User, situation string) (conversations.Message, error) {
	situation = strings.TrimSpace(situation)
	if situation == "" {
		return conversations.Message{}, apperr.Wrap(apperr.ErrValidation, "an outreach needs a situation")
	}

	target, err := s.conversations.ProactiveTarget(ctx, user.ID, uuid.Nil)
	if err != nil {
		return conversations.Message{}, err
	}
	coachCtx, err := s.contextB.Build(ctx, ContextRequest{User: user, ConversationID: target.ID, Query: situation})
	if err != nil {
		return conversations.Message{}, err
	}
	system, err := s.promptB.Coach(coachCtx)
	if err != nil {
		return conversations.Message{}, err
	}
	ask, err := prompts.Render(prompts.CoachOutreach, map[string]string{"Situation": situation})
	if err != nil {
		return conversations.Message{}, err
	}

	// The ask is the last user turn and is never stored, as with a standing
	// task: the person did not write it.
	messages := conversations.ToAIMessages(coachCtx.RecentMessages)
	messages = append(messages, ai.UserText(ask))

	genCtx, cancel := context.WithTimeout(ctx, generationTimeout)
	defer cancel()
	started := time.Now()
	resp, client, err := s.generate(genCtx, user, spend.SurfaceCoachOutreach, ai.Request{
		Model:     s.model,
		System:    system,
		Messages:  messages,
		MaxTokens: outreachMaxTokens,
	})
	provider := ""
	if client != nil {
		provider = client.Name()
	}
	var usage ai.Usage
	if resp != nil {
		usage = resp.Usage
	}
	s.analytics.captureGeneration(ctx, generation{
		sessionID: target.ID.String(), traceID: uuid.New().String(), distinctID: user.ID.String(),
		provider: provider, model: s.model, usage: usage, latency: time.Since(started), err: err,
	})
	if err != nil {
		return conversations.Message{}, apperr.Wrap(err, "coach: write outreach")
	}

	text, refs := StripRefs(strings.TrimSpace(resp.Text), coachCtx.OfferedRefs())
	if text == "" {
		return conversations.Message{}, apperr.Wrap(apperr.ErrUnavailable, "coach: empty outreach")
	}
	return s.conversations.AppendModelMessage(ctx, target.ID, text, &usage, s.model, provider, refs,
		conversations.Proactive(conversations.SourceCoachCheckIn))
}

// outreachMaxTokens bounds a two-sentence message with room for a model that
// thinks before it answers.
const outreachMaxTokens = 1024
