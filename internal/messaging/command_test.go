package messaging_test

import (
	"context"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/ai/fake"
	"github.com/NorthAIProject/north-client/internal/messaging"
)

// A command is not a question, so it must never reach a paid provider and must
// never cost anybody a coach message.
func TestCommandsNeverReachTheModelOrSpendQuota(t *testing.T) {
	for _, command := range []string{"/start", "/help"} {
		client := fake.Text("this should never be generated")
		quotas := &stubQuotas{allowed: true}
		h := newHarness(t, client, harnessOptions{quotas: quotas})
		h.link(t, chat)

		out := h.send(t, chat, command)

		if len(client.Calls()) != 0 {
			t.Fatalf("%s reached the model", command)
		}
		if quotas.consumed != 0 {
			t.Fatalf("%s spent %d from the budget", command, quotas.consumed)
		}
		if strings.TrimSpace(out.Text) == "" {
			t.Fatalf("%s said nothing", command)
		}
	}
}

// Pressing START is the first thing anybody does, and a linked person should be
// greeted rather than have a slash command interpreted by a model.
func TestStartGreetsALinkedPerson(t *testing.T) {
	h := newHarness(t, fake.Text("unused"), harnessOptions{})
	h.link(t, chat)

	out := h.send(t, chat, "/start")

	if !strings.Contains(out.Text, h.user.Email) {
		t.Fatalf("the greeting should name the account, got %q", out.Text)
	}
}

func TestStartFromAnUnlinkedChatAsksForACode(t *testing.T) {
	h := newHarness(t, fake.Text("unused"), harnessOptions{})

	out := h.send(t, chat, "/start")

	if !strings.Contains(out.Text, "Settings") {
		t.Fatalf("an unlinked /start should say where to get a code, got %q", out.Text)
	}
}

// The deep link t.me/<bot>?start=CODE arrives as "/start CODE", and that path
// existed before commands did. It must keep working.
func TestStartCarryingACodeStillLinks(t *testing.T) {
	h := newHarness(t, fake.Text("unused"), harnessOptions{})

	code, err := h.messaging.IssueCode(context.Background(), h.user.ID, messaging.PlatformTelegram)
	if err != nil {
		t.Fatalf("issue code: %v", err)
	}

	out := h.send(t, chat, "/start "+code)

	if !strings.Contains(out.Text, h.user.Email) {
		t.Fatalf("a deep-linked code did not link: %q", out.Text)
	}
}

// The two things somebody cannot discover by trying: that writes are confirmed
// first, and how to get out. Asserted on meaning rather than wording — the help
// deliberately says "wait for a yes" rather than "confirmation".
func TestHelpExplainsConfirmationAndDisconnecting(t *testing.T) {
	h := newHarness(t, fake.Text("unused"), harnessOptions{})
	h.link(t, chat)

	out := h.send(t, chat, "/help")
	lower := strings.ToLower(out.Text)

	if !strings.Contains(lower, "yes") || !strings.Contains(lower, "write") {
		t.Fatalf("help does not explain that writes are confirmed first: %q", out.Text)
	}
	if !strings.Contains(lower, commandUnlinkText) {
		t.Fatalf("help does not say how to disconnect: %q", out.Text)
	}
}

const commandUnlinkText = "/unlink"

func TestUnlinkDisconnectsTheChat(t *testing.T) {
	h := newHarness(t, fake.Text("unused"), harnessOptions{})
	ctx := context.Background()
	h.link(t, chat)

	h.send(t, chat, "/unlink")

	links, err := h.messaging.Links(ctx, h.user.ID)
	if err != nil {
		t.Fatalf("list links: %v", err)
	}
	if len(links) != 0 {
		t.Fatalf("the chat is still linked: %+v", links)
	}

	// And the next message is treated as a stranger's.
	out := h.send(t, chat, "how am I doing?")
	if !strings.Contains(out.Text, "Settings") {
		t.Fatalf("an unlinked chat should be asked for a code, got %q", out.Text)
	}
}

func TestUnlinkFromAnUnlinkedChatSaysSo(t *testing.T) {
	h := newHarness(t, fake.Text("unused"), harnessOptions{})

	out := h.send(t, chat, "/unlink")
	if strings.TrimSpace(out.Text) == "" {
		t.Fatal("/unlink on an unlinked chat said nothing")
	}
}

// An unknown slash command is far more likely to be a person typing than a
// mistake, so it goes to the coach rather than being refused.
func TestAnUnknownCommandReachesTheCoach(t *testing.T) {
	client := fake.Text("I do not have a command for that, but here is what I think.")
	h := newHarness(t, client, harnessOptions{})
	h.link(t, chat)

	out := h.send(t, chat, "/summarise my week")

	if len(client.Calls()) == 0 {
		t.Fatal("an unknown command never reached the coach")
	}
	if !strings.Contains(out.Text, "here is what I think") {
		t.Fatalf("got %q", out.Text)
	}
}

// A command must not resolve a write that is waiting for a yes or a no.
func TestACommandDoesNotAnswerAPendingConfirmation(t *testing.T) {
	client := fake.New(
		fake.Calling(fake.ToolCall("create_check_in", `{"mood":4}`)),
		fake.Response{Text: "Logged it."},
	)
	tools := writeTools()
	h := newHarness(t, client, harnessOptions{tools: tools})
	h.link(t, chat)

	h.send(t, chat, "log a check-in")
	h.send(t, chat, "/help")

	if len(tools.calls) != 0 {
		t.Fatalf("/help ran the pending write: %v", tools.calls)
	}

	// The confirmation is still there to answer.
	h.send(t, chat, messaging.AnswerApprove)
	if len(tools.calls) != 1 {
		t.Fatalf("the write was lost, ran %d times", len(tools.calls))
	}
}
