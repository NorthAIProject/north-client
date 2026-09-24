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

// silentReply is what the model answers when a standing task finds nothing
// worth saying. Checked loosely (case, trailing punctuation): a model that
// decorates the token is still telling us to stay quiet.
const silentReply = "NOTHING_TO_REPORT"

// RunStandingTask runs one standing task through the coach and posts the
// answer as a proactive message captioned "Standing task".
//
// The same persona, context and model as a reply, so a watch sounds like the
// coach rather than like a notification. One non-streaming generation and no
// tools: nobody is there to approve a write, and the context already carries
// the recent record a watch reads.
//
// preferred is the thread the watch was set up in; ProactiveTarget falls back
// to the latest chat, or a new one, when that thread is gone or waiting on an
// approval card. spoke is false when the model had nothing to say, and then
// nothing is posted.
func (s *Service) RunStandingTask(ctx context.Context, user users.User, preferred uuid.UUID, instruction string) (conversations.Message, bool, error) {
	instruction = strings.TrimSpace(instruction)
	if instruction == "" {
		return conversations.Message{}, false, apperr.Wrap(apperr.ErrValidation, "a standing task needs an instruction")
	}

	target, err := s.conversations.ProactiveTarget(ctx, user.ID, preferred)
	if err != nil {
		return conversations.Message{}, false, err
	}

	coachCtx, err := s.contextB.Build(ctx, ContextRequest{
		User:           user,
		ConversationID: target.ID,
		Query:          instruction,
	})
	if err != nil {
		return conversations.Message{}, false, err
	}

	system, err := s.promptB.Coach(coachCtx)
	if err != nil {
		return conversations.Message{}, false, err
	}

	task, err := prompts.Render(prompts.StandingTask, map[string]string{
		"Instruction": instruction,
		"Silent":      silentReply,
	})
	if err != nil {
		return conversations.Message{}, false, err
	}

	// The task goes in as the last user turn and is never stored: the person
	// did not say it, and the thread should not show them a message they
	// never wrote.
	messages := conversations.ToAIMessages(coachCtx.RecentMessages)
	messages = append(messages, ai.UserText(task))

	genCtx, cancel := context.WithTimeout(ctx, generationTimeout)
	defer cancel()

	started := time.Now()
	resp, client, err := s.generate(genCtx, user, spend.SurfaceStandingTask, capReplyTokens(ai.Request{
		Model:    s.model,
		System:   system,
		Messages: messages,
	}))

	provider := ""
	if client != nil {
		provider = client.Name()
	}
	var usage ai.Usage
	if resp != nil {
		usage = resp.Usage
	}
	s.analytics.captureGeneration(ctx, generation{
		sessionID:  target.ID.String(),
		traceID:    uuid.New().String(),
		distinctID: user.ID.String(),
		provider:   provider,
		model:      s.model,
		usage:      usage,
		latency:    time.Since(started),
		err:        err,
	})
	if err != nil {
		return conversations.Message{}, false, apperr.Wrap(err, "coach: run standing task")
	}

	text, refs := StripRefs(strings.TrimSpace(resp.Text), coachCtx.OfferedRefs())
	if isSilent(text) {
		return conversations.Message{}, false, nil
	}

	msg, err := s.conversations.AppendModelMessage(ctx, target.ID, text, &usage, s.model, provider, refs,
		conversations.Proactive(conversations.SourceStandingTask))
	if err != nil {
		return conversations.Message{}, false, err
	}
	return msg, true, nil
}

func isSilent(text string) bool {
	t := strings.ToUpper(strings.Trim(strings.TrimSpace(text), ".!`*\"' "))
	return t == "" || t == silentReply
}
