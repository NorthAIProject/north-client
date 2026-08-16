package messaging_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/fake"
	"github.com/NorthAIProject/north-client/internal/conversations"
	"github.com/NorthAIProject/north-client/internal/messaging"
)

const chat = "884422"

// The ticket's first acceptance criterion: a fake adapter round-trips to the
// coach and back.
func TestALinkedChatReachesTheCoach(t *testing.T) {
	h := newHarness(t, fake.Text("Two workouts this week. That is one more than last."), harnessOptions{})
	h.link(t, chat)

	out := h.send(t, chat, "how am I doing?")

	if !strings.Contains(out.Text, "Two workouts this week") {
		t.Fatalf("the coach's answer did not come back: %q", out.Text)
	}
	if len(out.Options) != 0 {
		t.Fatalf("an ordinary answer should carry no buttons, got %v", out.Options)
	}
}

// An unlinked chat must not be able to spend anybody's model budget. This is
// the gap ask_coach still has, and the reason the check is in front of the
// coach rather than beside it.
func TestAnUnlinkedChatNeverReachesTheCoach(t *testing.T) {
	client := fake.Text("this should never be generated")
	h := newHarness(t, client, harnessOptions{})

	out := h.send(t, chat, "how am I doing?")

	if len(client.Calls()) != 0 {
		t.Fatalf("an unlinked chat reached the model %d times", len(client.Calls()))
	}
	if !strings.Contains(out.Text, "Settings") {
		t.Fatalf("the reply should say where to get a code, got %q", out.Text)
	}
}

func TestACodeLinksTheChatItWasSentFrom(t *testing.T) {
	h := newHarness(t, fake.Text("hello"), harnessOptions{})
	ctx := context.Background()

	code, err := h.messaging.IssueCode(ctx, h.user.ID, messaging.PlatformTelegram)
	if err != nil {
		t.Fatalf("issue code: %v", err)
	}

	out := h.send(t, chat, code)
	if !strings.Contains(out.Text, h.user.Email) {
		t.Fatalf("linking should confirm which account, got %q", out.Text)
	}

	links, err := h.messaging.Links(ctx, h.user.ID)
	if err != nil {
		t.Fatalf("list links: %v", err)
	}
	if len(links) != 1 || links[0].ExternalID != chat {
		t.Fatalf("expected one link to %s, got %+v", chat, links)
	}
}

// Case and spacing are forgiven because the code is retyped by hand; the
// characters themselves are not, because the alphabet already excludes the
// ones people get wrong.
func TestACodeIsCaseAndSpaceInsensitive(t *testing.T) {
	h := newHarness(t, fake.Text("hello"), harnessOptions{})

	code, err := h.messaging.IssueCode(context.Background(), h.user.ID, messaging.PlatformTelegram)
	if err != nil {
		t.Fatalf("issue code: %v", err)
	}

	out := h.send(t, chat, "  "+strings.ToLower(code)+" ")
	if !strings.Contains(out.Text, h.user.Email) {
		t.Fatalf("a lower-cased code should still link, got %q", out.Text)
	}
}

func TestACodeCannotBeSpentTwice(t *testing.T) {
	h := newHarness(t, fake.Text("hello"), harnessOptions{})

	code, err := h.messaging.IssueCode(context.Background(), h.user.ID, messaging.PlatformTelegram)
	if err != nil {
		t.Fatalf("issue code: %v", err)
	}

	h.send(t, chat, code)

	// From a different chat, so the second attempt is the interesting case
	// rather than one already-linked chat repeating itself.
	out := h.send(t, "999111", code)
	if strings.Contains(out.Text, h.user.Email) {
		t.Fatalf("a spent code linked a second chat: %q", out.Text)
	}
}

// Issuing a code invalidates the last, so somebody who asks twice cannot be
// confused about which of two codes is live.
func TestIssuingACodeInvalidatesTheEarlierOne(t *testing.T) {
	h := newHarness(t, fake.Text("hello"), harnessOptions{})
	ctx := context.Background()

	first, err := h.messaging.IssueCode(ctx, h.user.ID, messaging.PlatformTelegram)
	if err != nil {
		t.Fatalf("issue first code: %v", err)
	}
	if _, err = h.messaging.IssueCode(ctx, h.user.ID, messaging.PlatformTelegram); err != nil {
		t.Fatalf("issue second code: %v", err)
	}

	out := h.send(t, chat, first)
	if strings.Contains(out.Text, h.user.Email) {
		t.Fatalf("the superseded code still linked: %q", out.Text)
	}
}

// Both mouths, one thread. This is the whole point of the ticket: asking on
// the phone and asking in the browser continue each other.
func TestPlatformMessagesLandInTheWebThread(t *testing.T) {
	h := newHarness(t, fake.Text("noted"), harnessOptions{})
	ctx := context.Background()
	h.link(t, chat)

	// A thread the web app started, exactly as the chat page would.
	web, err := h.coach.StartConversation(ctx, h.user.ID)
	if err != nil {
		t.Fatalf("start conversation: %v", err)
	}

	h.send(t, chat, "first")
	h.send(t, chat, "second")

	list, err := h.convos.List(ctx, h.user.ID, 10)
	if err != nil {
		t.Fatalf("list conversations: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected one thread shared by both surfaces, got %d", len(list))
	}
	if list[0].ID != web.ID {
		t.Fatalf("messages started a new thread instead of continuing the web one")
	}

	history, err := h.convos.History(ctx, web.ID)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if !containsUserMessage(history, "first") || !containsUserMessage(history, "second") {
		t.Fatalf("both messages should be in the web thread, got %d turns", len(history))
	}
}

// A reflection ends, and a message arriving after it closed would be refused.
// So a platform message starts its own chat thread rather than joining one.
func TestAReflectionIsNotJoinedByAPlatformMessage(t *testing.T) {
	h := newHarness(t, fake.Text("noted"), harnessOptions{})
	ctx := context.Background()
	h.link(t, chat)

	reflection, err := h.coach.StartReflection(ctx, h.user.ID)
	if err != nil {
		t.Fatalf("start reflection: %v", err)
	}

	h.send(t, chat, "hello")

	list, err := h.convos.List(ctx, h.user.ID, 10)
	if err != nil {
		t.Fatalf("list conversations: %v", err)
	}
	for _, c := range list {
		if c.ID == reflection.ID {
			continue
		}
		if c.Kind == conversations.KindChat {
			return
		}
	}
	t.Fatal("the message did not get a chat thread of its own")
}

// A redelivery must not coach the same sentence twice — and, once tools are
// involved, must not run a confirmed write twice.
func TestARedeliveredUpdateIsIgnored(t *testing.T) {
	client := fake.Text("answered once")
	h := newHarness(t, client, harnessOptions{})
	h.link(t, chat)

	const updateID = 5000
	first := h.sendUpdate(t, chat, "how am I doing?", updateID)
	if first.Silent {
		t.Fatal("the first delivery was ignored")
	}

	before := len(client.Calls())
	second := h.sendUpdate(t, chat, "how am I doing?", updateID)

	if !second.Silent {
		t.Fatalf("a redelivery should send nothing, got %q", second.Text)
	}
	if len(client.Calls()) != before {
		t.Fatalf("a redelivery reached the model again: %d then %d", before, len(client.Calls()))
	}
}

func TestAQuotaRefusalNeverReachesTheModel(t *testing.T) {
	client := fake.Text("this should never be generated")
	quotas := &stubQuotas{allowed: false, retryAfter: 20 * time.Minute}
	h := newHarness(t, client, harnessOptions{quotas: quotas})
	h.link(t, chat)

	before := len(client.Calls())
	out := h.send(t, chat, "how am I doing?")

	if len(client.Calls()) != before {
		t.Fatal("a refused turn still reached the model")
	}
	if !strings.Contains(out.Text, "20 minutes") {
		t.Fatalf("the refusal should say when the budget returns, got %q", out.Text)
	}
}

// Linking is not a coach turn, so it must not cost one.
func TestRedeemingACodeSpendsNoQuota(t *testing.T) {
	quotas := &stubQuotas{allowed: true}
	h := newHarness(t, fake.Text("hello"), harnessOptions{quotas: quotas})

	code, err := h.messaging.IssueCode(context.Background(), h.user.ID, messaging.PlatformTelegram)
	if err != nil {
		t.Fatalf("issue code: %v", err)
	}
	h.send(t, chat, code)

	if quotas.consumed != 0 {
		t.Fatalf("linking spent %d from the budget", quotas.consumed)
	}
}

func TestAnAccountThatHasNotOnboardedIsSentToTheWebApp(t *testing.T) {
	client := fake.Text("this should never be generated")
	h := newHarness(t, client, harnessOptions{})
	h.link(t, chat)

	// Undo what the harness did, so this is the state a brand-new account is in.
	if _, err := h.pool.Exec(context.Background(),
		"UPDATE users SET onboarded_at = NULL WHERE id = $1", h.user.ID); err != nil {
		t.Fatalf("clear onboarding: %v", err)
	}

	before := len(client.Calls())
	out := h.send(t, chat, "how am I doing?")

	if len(client.Calls()) != before {
		t.Fatal("an un-onboarded account reached the model")
	}
	if !strings.Contains(out.Text, "web app") {
		t.Fatalf("the reply should point at the web app, got %q", out.Text)
	}
}

func containsUserMessage(history []conversations.Message, text string) bool {
	for _, m := range history {
		if m.Role == ai.RoleUser && strings.TrimSpace(m.Content) == text {
			return true
		}
	}
	return false
}
