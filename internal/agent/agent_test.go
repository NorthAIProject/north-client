package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

func capability(name string, invoke func(context.Context, uuid.UUID, json.RawMessage) (string, error)) Capability {
	return Capability{
		Tool:   ai.Tool{Name: name, Description: "a test capability", Parameters: ai.Object("none", map[string]*ai.Schema{})},
		Invoke: invoke,
	}
}

func ok(result string) func(context.Context, uuid.UUID, json.RawMessage) (string, error) {
	return func(context.Context, uuid.UUID, json.RawMessage) (string, error) { return result, nil }
}

func TestInvokeRunsTheNamedCapability(t *testing.T) {
	t.Parallel()

	r := NewRegistry()
	r.Register(capability("list_goals", ok("- Squat 140kg")))

	result := r.Invoke(context.Background(), uuid.New(), ai.ToolCall{ID: "c1", Name: "list_goals"})

	if result.IsError {
		t.Fatalf("unexpected error result: %s", result.Content)
	}
	if result.ID != "c1" || result.Name != "list_goals" {
		t.Errorf("result = %+v, want the call's id and name echoed back", result)
	}
	if result.Content != "- Squat 140kg" {
		t.Errorf("Content = %q", result.Content)
	}
}

// The user id is the caller's, from an authenticated session. A capability
// must never be able to read one out of the model's arguments.
func TestInvokePassesTheCallersUserID(t *testing.T) {
	t.Parallel()

	wanted := uuid.New()
	var got uuid.UUID

	r := NewRegistry()
	r.Register(capability("list_goals", func(_ context.Context, userID uuid.UUID, _ json.RawMessage) (string, error) {
		got = userID
		return "fine", nil
	}))

	r.Invoke(context.Background(), wanted, ai.ToolCall{
		Name:      "list_goals",
		Arguments: json.RawMessage(`{"user_id":"00000000-0000-0000-0000-000000000001"}`),
	})

	if got != wanted {
		t.Errorf("capability received %v, want the caller's %v", got, wanted)
	}
}

// A model told "there is no such tool" can correct itself. One handed silence
// assumes the call worked and describes a result that never existed.
func TestAnUnknownToolIsReportedToTheModel(t *testing.T) {
	t.Parallel()

	r := NewRegistry()
	r.Register(capability("list_goals", ok("fine")))

	result := r.Invoke(context.Background(), uuid.New(), ai.ToolCall{Name: "delete_everything"})

	if !result.IsError {
		t.Fatal("an unknown tool must come back as an error result")
	}
	if !strings.Contains(result.Content, "list_goals") {
		t.Errorf("the message should list what is available, got %q", result.Content)
	}
}

// Validation and not-found messages are written for people and are the useful
// half of a failed call, so they reach the model. Anything else can carry
// connection strings and query fragments into a conversation the user reads.
func TestOnlyUserFacingErrorsReachTheModel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		err     error
		wantAll string
	}{
		{
			name:    "validation errors are forwarded",
			err:     apperr.Wrap(apperr.ErrValidation, "record your biometrics first"),
			wantAll: "record your biometrics first",
		},
		{
			name:    "not-found errors are forwarded",
			err:     apperr.ErrNotFound,
			wantAll: apperr.ErrNotFound.Error(),
		},
		{
			name:    "anything else is replaced",
			err:     errors.New(`pq: password authentication failed for user "north"`),
			wantAll: "That did not work",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := NewRegistry()
			r.Register(capability("boom", func(context.Context, uuid.UUID, json.RawMessage) (string, error) {
				return "", tt.err
			}))

			result := r.Invoke(context.Background(), uuid.New(), ai.ToolCall{Name: "boom"})

			if !result.IsError {
				t.Fatal("a failed call must be marked as an error")
			}
			if !strings.Contains(result.Content, tt.wantAll) {
				t.Errorf("Content = %q, want it to contain %q", result.Content, tt.wantAll)
			}
			if strings.Contains(result.Content, "password authentication") {
				t.Errorf("an internal error leaked into the conversation: %q", result.Content)
			}
		})
	}
}

// "" reads to a model as "nothing to say", which it papers over by inventing
// something. Saying "No results." is what stops that.
func TestAnEmptyResultIsSaidOutLoud(t *testing.T) {
	t.Parallel()

	r := NewRegistry()
	r.Register(capability("search", ok("")))

	result := r.Invoke(context.Background(), uuid.New(), ai.ToolCall{Name: "search"})
	if result.Content != "No results." {
		t.Errorf("Content = %q, want an explicit empty answer", result.Content)
	}
	if result.IsError {
		t.Error("finding nothing is not a failure")
	}
}

// The calls in one turn frequently depend on each other: a model that logs a
// meal and then asks for today's total should see the meal it just logged.
func TestInvokeAllRunsCallsInOrder(t *testing.T) {
	t.Parallel()

	var order []string

	r := NewRegistry()
	r.Register(
		capability("first", func(context.Context, uuid.UUID, json.RawMessage) (string, error) {
			order = append(order, "first")
			return "1", nil
		}),
		capability("second", func(context.Context, uuid.UUID, json.RawMessage) (string, error) {
			order = append(order, "second")
			return "2", nil
		}),
	)

	results := r.InvokeAll(context.Background(), uuid.New(), []ai.ToolCall{
		{ID: "a", Name: "first"},
		{ID: "b", Name: "second"},
	})

	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	if strings.Join(order, ",") != "first,second" {
		t.Errorf("ran in order %v, want first then second", order)
	}
}

func TestToolsAreSortedForAStablePrompt(t *testing.T) {
	t.Parallel()

	r := NewRegistry()
	r.Register(capability("zebra", ok("")), capability("alpha", ok("")), capability("middle", ok("")))

	tools := r.Tools()
	if len(tools) != 3 {
		t.Fatalf("got %d tools, want 3", len(tools))
	}
	// An order that changed between requests would defeat prompt caching for
	// no benefit.
	if tools[0].Name != "alpha" || tools[1].Name != "middle" || tools[2].Name != "zebra" {
		t.Errorf("tools are not sorted: %v", r.Names())
	}
}

// Two capabilities answering to one name is a wiring mistake that would
// otherwise surface as one of them silently never running.
func TestRegisteringADuplicateNamePanics(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Fatal("registering a duplicate name should panic at startup")
		}
	}()

	r := NewRegistry()
	r.Register(capability("same", ok("")))
	r.Register(capability("same", ok("")))
}

func TestDecodeReportsAValidationErrorTheModelCanAct(t *testing.T) {
	t.Parallel()

	type args struct {
		Query string `json:"query"`
	}

	if _, err := Decode[args](json.RawMessage(`{"query":`)); !apperr.Is(err, apperr.ErrValidation) {
		t.Errorf("got %v, want a validation error", err)
	}

	// No arguments at all is normal for a tool that takes none.
	if _, err := Decode[args](nil); err != nil {
		t.Errorf("empty arguments should decode cleanly, got %v", err)
	}
}
