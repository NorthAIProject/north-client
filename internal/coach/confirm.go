package coach

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/conversations"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/toolsurface"
	"github.com/NorthAIProject/north-client/internal/users"
	"github.com/NorthAIProject/north-client/internal/watches"
)

// DeclinedCall is a write somebody refused.
//
// Reported from here because a refusal never reaches the capability registry —
// the tool is not invoked at all — so without this the account would show only
// the writes that happened and none of the ones a person stopped.
type DeclinedCall struct {
	UserID    uuid.UUID
	Tool      string
	Arguments json.RawMessage
}

// DeclineRecorder keeps the account of refusals. Optional: a service without
// one still works, it simply records nothing.
type DeclineRecorder interface {
	RecordDeclinedCall(ctx context.Context, c DeclinedCall)
}

// PendingCall is a turn stopped in front of a person: the tools a model asked
// to run, waiting to be allowed or refused.
type PendingCall struct {
	// MessageID is the stored turn carrying the calls. Passed back on approval
	// so a page submitted twice cannot run the tools twice.
	MessageID uuid.UUID

	Calls []ai.ToolCall
}

// declineNotice is what the model is told when a person refuses.
//
// Phrased as an outcome rather than an error, because it is not a failure: the
// person answered, and the answer was no. A model told only "error" tends to
// retry the same call.
const declineNotice = "The user declined this. It was not run, and nothing was changed. " +
	"Acknowledge that and carry on without trying again."

// PendingApproval reports the turn waiting on this conversation, if any.
//
// Pending is derived rather than stored in a status column: a conversation is
// waiting exactly when its last turn is a tool call with no results after it.
// A flag beside that would be a second source of truth able to disagree with
// the messages themselves.
func (s *Service) PendingApproval(ctx context.Context, user users.User, conversationID uuid.UUID) (PendingCall, bool, error) {
	// Get is what enforces ownership: it filters by user id, so another
	// account asking about this conversation is told it does not exist.
	if _, err := s.conversations.Get(ctx, conversationID, user.ID); err != nil {
		return PendingCall{}, false, err
	}

	history, err := s.latestTurn(ctx, conversationID)
	if err != nil {
		return PendingCall{}, false, err
	}

	for i := len(history) - 1; i >= 0; i-- {
		m := history[i]
		switch {
		case len(m.ToolResults) > 0:
			// Results are the answer to the call before them, so the most
			// recent tool turn is already resolved.
			return PendingCall{}, false, nil
		case len(m.ToolCalls) > 0:
			return PendingCall{MessageID: m.ID, Calls: m.ToolCalls}, true, nil
		}
	}
	return PendingCall{}, false, nil
}

// latestTurnWindow is how many messages from the end of a conversation are
// enough to hold its latest turn: the person's message, a call and a result
// for every tool round, and the reply.
const latestTurnWindow = 2*toolRounds + 2

// latestTurn reads the end of a conversation, in reading order.
//
// Not History: that is the first page, oldest first, for display. A linked
// Telegram chat is one thread that runs for weeks, and reading its latest turn
// off the first page found a turn from the beginning of it — so replies lost
// their animation and suspended writes were never offered for approval.
func (s *Service) latestTurn(ctx context.Context, conversationID uuid.UUID) ([]conversations.Message, error) {
	return s.conversations.Recent(ctx, conversationID, latestTurnWindow)
}

// ResolvePending runs or refuses the waiting call and continues the reply.
//
// Approving invokes the tools and hands their results back to the model;
// declining records a refusal instead and never invokes anything. Either way
// the model gets a turn to answer, because a person who says no is owed an
// acknowledgement rather than silence.
func (s *Service) ResolvePending(ctx context.Context, user users.User, conversationID, messageID uuid.UUID, approve bool) error {
	pending, ok, err := s.PendingApproval(ctx, user, conversationID)
	if err != nil {
		return err
	}
	if !ok {
		// Not found rather than a conflict: from the caller's side there is
		// nothing here to act on, and saying more would tell a stranger
		// whether this conversation exists.
		return apperr.Wrap(apperr.ErrNotFound, "nothing is awaiting approval on this conversation")
	}

	// The card names the turn it belongs to, so a page submitted twice — or a
	// stale tab from an earlier turn — resolves nothing. Without this the
	// second submit would approve whatever happened to be pending by then.
	if messageID != uuid.Nil && messageID != pending.MessageID {
		return apperr.Wrap(apperr.ErrConflict, "this approval is for a turn that has already moved on")
	}

	results := make([]ai.ToolResult, 0, len(pending.Calls))
	if approve {
		toolCtx := toolsurface.WithThread(toolsurface.With(ctx, toolsurface.Coach), conversationID)
		results = s.tools.InvokeAll(toolCtx, user.ID, pending.Calls)
	} else {
		for _, call := range pending.Calls {
			results = append(results, ai.ToolResult{
				ID:      call.ID,
				Name:    call.Name,
				Content: declineNotice,
			})

			if s.declines != nil {
				s.declines.RecordDeclinedCall(ctx, DeclinedCall{
					UserID:    user.ID,
					Tool:      call.Name,
					Arguments: call.Arguments,
				})
			}
		}
	}

	// Written before the model is asked anything. If generation fails, the
	// tools have still run and the record has to say so — a write that
	// happened and was not recorded is the one unrecoverable outcome here.
	if _, err = s.conversations.AppendToolResults(ctx, conversationID, results); err != nil {
		return apperr.Wrap(err, "record the resolved tool call")
	}

	// A confirmed standing task is answered here rather than by resuming the
	// model: the contract fixes the sentence ("Got it — I'll watch X and ping
	// you when Y", captioned "Standing task"), and create_watch already
	// returned it. With a text turn after the results the page no longer
	// derives a resume, so nothing else is generated for this turn.
	if approve {
		if confirmations, ok := standingTaskConfirmations(results); ok {
			for _, text := range confirmations {
				if _, err = s.conversations.AppendModelMessage(ctx, conversationID, text, nil, "", "", nil,
					conversations.Proactive(conversations.SourceStandingTask)); err != nil {
					return apperr.Wrap(err, "record the standing task confirmation")
				}
			}
		}
	}
	return nil
}

// standingTaskConfirmations returns the confirmation sentences when every
// result in a turn is a create_watch that succeeded. A mixed or failed turn
// answers false and is resumed as usual, so the model can explain it.
func standingTaskConfirmations(results []ai.ToolResult) ([]string, bool) {
	if len(results) == 0 {
		return nil, false
	}
	out := make([]string, 0, len(results))
	for _, r := range results {
		if r.Name != watches.ToolName || r.IsError || r.Content == "" {
			return nil, false
		}
		out = append(out, r.Content)
	}
	return out, true
}

// toolNames lists what a set of calls would run, for a log line.
func toolNames(calls []ai.ToolCall) []string {
	names := make([]string, 0, len(calls))
	for _, call := range calls {
		names = append(names, call.Name)
	}
	return names
}

// writingCalls returns the calls in a turn that change something.
//
// A tool the runner does not recognise counts as a write. The registry answers
// false for an unknown name for the same reason: the safe wrong answer is to
// ask.
func writingCalls(tools ToolRunner, calls []ai.ToolCall) []ai.ToolCall {
	var writes []ai.ToolCall
	for _, call := range calls {
		if !tools.IsReadOnly(call.Name) {
			writes = append(writes, call)
		}
	}
	return writes
}

// describeCall renders a call the way a person has to read it before allowing
// it. The arguments matter as much as the name: "log a check-in" is a very
// different request from "log a check-in saying the week went badly".
func describeCall(call ai.ToolCall) string {
	if len(call.Arguments) == 0 {
		return call.Name
	}
	return fmt.Sprintf("%s %s", call.Name, string(call.Arguments))
}

// resume carries on a reply whose turn was interrupted by an approval.
//
// The request is rebuilt from stored messages rather than kept in memory,
// because the stream that started this turn ended when the card was shown and
// the process may not even be the same one. That is what the tool_calls and
// tool_results columns are for: ToAIMessages replays them, so the model sees
// the call it made and the result it is owed, which is the pair every provider
// requires.
//
// Nothing is appended as a user turn and no quota is spent: the person asked
// once, and answering a confirmation is not a second question.
func (s *Service) Resume(ctx context.Context, user users.User, conversationID uuid.UUID) (<-chan ai.StreamChunk, error) {
	conversation, err := s.conversations.Get(ctx, conversationID, user.ID)
	if err != nil {
		return nil, err
	}

	coachCtx, err := s.contextB.Build(ctx, ContextRequest{
		User:           user,
		ConversationID: conversationID,
	})
	if err != nil {
		return nil, err
	}

	system, err := s.promptB.Coach(coachCtx)
	if err != nil {
		return nil, err
	}

	genCtx, cancelGen := context.WithTimeout(context.WithoutCancel(ctx), generationTimeout)

	req := ai.Request{
		Model:    s.model,
		System:   system,
		Messages: conversations.ToAIMessages(coachCtx.RecentMessages),
		Tools:    s.toolDeclarations(),
	}

	stream, client, err := s.startChat(ctx, genCtx, user, req)
	if err != nil {
		cancelGen()
		return nil, apperr.Wrap(err, "coach: resume reply")
	}

	out := make(chan ai.StreamChunk)
	go s.pump(ctx, genCtx, cancelGen, stream, out, pumpTarget{
		conversation: conversation,
		user:         user,
		provider:     client.Name(),
		client:       client,
		request:      req,
		offeredRefs:  coachCtx.OfferedRefs(),
		traceID:      uuid.New().String(),
	})

	return out, nil
}
