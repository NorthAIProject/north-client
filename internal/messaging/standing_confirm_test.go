package messaging_test

import (
	"testing"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/fake"
	"github.com/NorthAIProject/north-client/internal/messaging"
	"github.com/NorthAIProject/north-client/internal/watches"
)

// Approving a standing task is answered with the contract's sentence and no
// generation. Telegram resumes after every approval, so it has to be handed that
// sentence instead of asking the model again or reporting a failure.
func TestApprovingAStandingTaskAnswersWithItsConfirmation(t *testing.T) {
	const sentence = "Got it — I'll watch your sleep and ping you when it drops under seven hours."

	client := fake.New(
		fake.Calling(fake.ToolCall(watches.ToolName, `{"title":"Sleep"}`)),
		fake.Response{Text: "this reply must never be generated"},
	)
	tools := &stubTools{
		tools: []ai.Tool{{
			Name:        watches.ToolName,
			Description: "set up a standing task",
			Parameters:  &ai.Schema{Type: "object"},
		}},
		results:  map[string]string{watches.ToolName: sentence},
		readOnly: map[string]bool{watches.ToolName: false},
	}
	h := newHarness(t, client, harnessOptions{tools: tools})
	h.link(t, chat)

	h.send(t, chat, "tell me every morning if I slept under seven hours")
	before := len(client.Calls())

	out := h.send(t, chat, messaging.AnswerApprove)

	if out.Text != sentence {
		t.Errorf("answer = %q, want the confirmation", out.Text)
	}
	if n := len(client.Calls()) - before; n != 0 {
		t.Errorf("the model was called %d more times", n)
	}
}
