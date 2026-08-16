package messaging_test

import (
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/fake"
	"github.com/NorthAIProject/north-client/internal/messaging"
)

// writeTools is a registry whose one capability writes, so the coach suspends
// rather than running it.
func writeTools() *stubTools {
	return &stubTools{
		tools: []ai.Tool{{
			Name:        "create_check_in",
			Description: "log how a day went",
			Parameters:  &ai.Schema{Type: "object"},
		}},
		results:  map[string]string{"create_check_in": `{"ok":true}`},
		readOnly: map[string]bool{"create_check_in": false},
	}
}

// A pending write reaches a platform as a question with two answers.
//
// The coach's pump never forwards tool calls to its caller, so out here a
// suspended write looks exactly like a short reply. Without the check after the
// stream closes this test would see silence — which is what ask_coach does.
func TestAPendingWriteComesBackAsAQuestion(t *testing.T) {
	client := fake.New(
		fake.Calling(fake.ToolCall("create_check_in", `{"mood":4,"note":"good day"}`)),
		fake.Response{Text: "Logged it."},
	)
	tools := writeTools()
	h := newHarness(t, client, harnessOptions{tools: tools})
	h.link(t, chat)

	out := h.send(t, chat, "log a check-in saying today went well")

	if len(tools.calls) != 0 {
		t.Fatalf("the write ran without being confirmed: %v", tools.calls)
	}
	if len(out.Options) != 2 {
		t.Fatalf("expected two answers on the question, got %v", out.Options)
	}
	if out.Options[0].Value != messaging.AnswerApprove || out.Options[1].Value != messaging.AnswerDecline {
		t.Fatalf("unexpected answer values: %+v", out.Options)
	}

	// The arguments are in the question on purpose: "log a check-in" is a very
	// different request from "log a check-in saying the week went badly".
	if !strings.Contains(out.Text, "good day") {
		t.Fatalf("the question should describe what would be written, got %q", out.Text)
	}
}

func TestApprovingRunsTheWriteAndAnswers(t *testing.T) {
	client := fake.New(
		fake.Calling(fake.ToolCall("create_check_in", `{"mood":4}`)),
		fake.Response{Text: "Logged it."},
	)
	tools := writeTools()
	h := newHarness(t, client, harnessOptions{tools: tools})
	h.link(t, chat)

	h.send(t, chat, "log a check-in")
	out := h.send(t, chat, messaging.AnswerApprove)

	if len(tools.calls) != 1 {
		t.Fatalf("expected the write to run once, ran %d times", len(tools.calls))
	}
	if !strings.Contains(out.Text, "Logged it") {
		t.Fatalf("the coach should answer after the write, got %q", out.Text)
	}
	if len(out.Options) != 0 {
		t.Fatalf("a resolved turn should carry no buttons, got %v", out.Options)
	}
}

// A typed word is as good as a tapped button. People do not scroll up.
func TestATypedYesApproves(t *testing.T) {
	client := fake.New(
		fake.Calling(fake.ToolCall("create_check_in", `{"mood":4}`)),
		fake.Response{Text: "Logged it."},
	)
	tools := writeTools()
	h := newHarness(t, client, harnessOptions{tools: tools})
	h.link(t, chat)

	h.send(t, chat, "log a check-in")
	h.send(t, chat, "yes please do")

	if len(tools.calls) != 1 {
		t.Fatalf("a typed yes did not approve the write, ran %d times", len(tools.calls))
	}
}

func TestDecliningRunsNothing(t *testing.T) {
	client := fake.New(
		fake.Calling(fake.ToolCall("create_check_in", `{"mood":4}`)),
		fake.Response{Text: "No problem, nothing logged."},
	)
	tools := writeTools()
	h := newHarness(t, client, harnessOptions{tools: tools})
	h.link(t, chat)

	h.send(t, chat, "log a check-in")
	out := h.send(t, chat, "no")

	if len(tools.calls) != 0 {
		t.Fatalf("declining still ran the write: %v", tools.calls)
	}
	if !strings.Contains(out.Text, "nothing logged") {
		t.Fatalf("a refusal is owed an acknowledgement, got %q", out.Text)
	}
}

// An ambiguous reply must not silently abandon the write the person was asked
// about — nor start a new turn as if the question had never been asked.
func TestAnUnclearAnswerReAsks(t *testing.T) {
	client := fake.New(
		fake.Calling(fake.ToolCall("create_check_in", `{"mood":4}`)),
		fake.Response{Text: "Logged it."},
	)
	tools := writeTools()
	h := newHarness(t, client, harnessOptions{tools: tools})
	h.link(t, chat)

	h.send(t, chat, "log a check-in")
	out := h.send(t, chat, "what would that do exactly?")

	if len(tools.calls) != 0 {
		t.Fatalf("an unclear answer ran the write: %v", tools.calls)
	}
	if len(out.Options) != 2 {
		t.Fatalf("the question should be asked again, got %v", out.Options)
	}
	if !strings.Contains(strings.ToLower(out.Text), "yes or a no") {
		t.Fatalf("the re-ask should say what is needed, got %q", out.Text)
	}
}

// Answering a confirmation is not a second question, so it costs nothing —
// matching the web app's resume route.
func TestAnsweringAConfirmationSpendsNoQuota(t *testing.T) {
	client := fake.New(
		fake.Calling(fake.ToolCall("create_check_in", `{"mood":4}`)),
		fake.Response{Text: "Logged it."},
	)
	quotas := &stubQuotas{allowed: true}
	h := newHarness(t, client, harnessOptions{tools: writeTools(), quotas: quotas})
	h.link(t, chat)

	h.send(t, chat, "log a check-in")
	spentOnTheQuestion := quotas.consumed

	h.send(t, chat, messaging.AnswerApprove)

	if quotas.consumed != spentOnTheQuestion {
		t.Fatalf("answering cost %d extra", quotas.consumed-spentOnTheQuestion)
	}
}
