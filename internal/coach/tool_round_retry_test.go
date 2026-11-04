package coach_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/fake"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// Managed clients make one attempt and the chain is their retry, but the round
// after a tool call has no chain. The tools have already run, so a brief 429 must
// not end the turn.
func TestAToolRoundSurvivesOneUnavailableAnswer(t *testing.T) {
	calls := 0
	client := &fake.Client{Handler: func(context.Context, ai.Request) (fake.Response, error) {
		calls++
		switch calls {
		case 1:
			return fake.Calling(fake.ToolCall("search_exercises", `{"muscle":"hamstrings"}`)), nil
		case 2:
			return fake.Response{}, fmt.Errorf("%w: rate limited", apperr.ErrUnavailable)
		default:
			return fake.Response{Text: "Try the deadlift."}, nil
		}
	}}

	reply, err := runToolTurn(t, client)
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if !strings.Contains(reply, "deadlift") {
		t.Errorf("reply = %q, want the answer from the second attempt", reply)
	}
	if calls != 3 {
		t.Errorf("provider asked %d times, want 3", calls)
	}
}

// One retry, not a loop, and only for a failure a retry could cure.
func TestAToolRoundStopsAfterASecondUnavailableAnswer(t *testing.T) {
	calls := 0
	client := &fake.Client{Handler: func(context.Context, ai.Request) (fake.Response, error) {
		calls++
		if calls == 1 {
			return fake.Calling(fake.ToolCall("search_exercises", `{"muscle":"hamstrings"}`)), nil
		}
		return fake.Response{}, fmt.Errorf("%w: rate limited", apperr.ErrUnavailable)
	}}

	if _, err := runToolTurn(t, client); err == nil {
		t.Error("drain succeeded, want the provider's error")
	}
	if calls != 3 {
		t.Errorf("provider asked %d times, want 3", calls)
	}
}

func TestAToolRoundDoesNotRetryAnEmptyBalance(t *testing.T) {
	calls := 0
	client := &fake.Client{Handler: func(context.Context, ai.Request) (fake.Response, error) {
		calls++
		if calls == 1 {
			return fake.Calling(fake.ToolCall("search_exercises", `{"muscle":"hamstrings"}`)), nil
		}
		return fake.Response{}, fmt.Errorf("%w: credit balance too low", apperr.ErrPaymentRequired)
	}}

	if _, err := runToolTurn(t, client); err == nil {
		t.Error("drain succeeded, want the provider's error")
	}
	if calls != 2 {
		t.Errorf("provider asked %d times, want 2", calls)
	}
}

func runToolTurn(t *testing.T, client *fake.Client) (string, error) {
	t.Helper()

	tools := &stubTools{
		tools:    []ai.Tool{searchTool},
		results:  map[string]string{"search_exercises": "- barbell-deadlift"},
		readOnly: map[string]bool{"search_exercises": true},
	}
	h := attributedHarness(t, &recordingMeter{}, client, tools)
	conversationID := newConversation(t, h)

	stream, err := h.coach.SendMessage(context.Background(), h.user, conversationID, "hamstrings?")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	return drain(stream)
}
