package chat

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/conversations"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/users"
)

func TestComposerAcceptsAPhoto(t *testing.T) {
	var buf bytes.Buffer
	err := Page(
		users.User{DisplayName: "Fernando"},
		conversations.Conversation{ID: uuid.MustParse("11111111-1111-1111-1111-111111111111")},
		nil,
		nil,
		CoachStats{},
		nil,
		false,
		"",
	).Render(context.Background(), &buf)
	if err != nil {
		t.Fatal(err)
	}
	body := buf.String()
	for _, want := range []string{
		`hx-encoding="multipart/form-data"`,
		`name="attachment"`,
		`accept="image/jpeg,image/png,image/webp,image/gif"`,
		`aria-label="Attach a photo"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("composer missing %q", want)
		}
	}
}

func TestBubbleRendersAPhoto(t *testing.T) {
	var buf bytes.Buffer
	err := Bubble(conversations.Message{
		Role: ai.RoleUser,
		Parts: []conversations.Attachment{{
			MediaID: uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
			Kind:    "image",
			Name:    "squat.jpg",
		}},
	}).Render(context.Background(), &buf)
	if err != nil {
		t.Fatal(err)
	}
	body := buf.String()
	if !strings.Contains(body, "/app/media/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa") {
		t.Error("user bubble should show the stored photo")
	}
}

func TestPageKeepsTheComposerReachableOnAPhone(t *testing.T) {
	var buf bytes.Buffer
	err := Page(
		users.User{DisplayName: "Fernando"},
		conversations.Conversation{ID: uuid.MustParse("11111111-1111-1111-1111-111111111111")},
		nil,
		nil,
		CoachStats{},
		nil,
		false,
		"",
	).Render(context.Background(), &buf)
	if err != nil {
		t.Fatal(err)
	}
	body := buf.String()
	for _, want := range []string{
		`id="chat-root"`,
		"visualViewport",
		"env(safe-area-inset-bottom)",
		// Checked separately: TwMerge reorders a component's classes, so
		// the pair is never adjacent on a templUI button.
		"min-h-11",
		"min-w-11",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("chat page missing %q", want)
		}
	}
}

func TestCopyIsVisibleOnTouch(t *testing.T) {
	var buf bytes.Buffer
	err := Bubble(conversations.Message{
		Role:    ai.RoleModel,
		Content: "A reply.",
	}).Render(context.Background(), &buf)
	if err != nil {
		t.Fatal(err)
	}
	body := buf.String()
	if !strings.Contains(body, "sm:opacity-0 sm:group-hover:opacity-100") {
		t.Error("copy control is hover-only; thumbs never hover")
	}
}

func TestApprovalButtonsAreThumbSized(t *testing.T) {
	var buf bytes.Buffer
	err := ApprovalCard(uuid.MustParse("11111111-1111-1111-1111-111111111111"), []PendingTool{{
		MessageID: uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		Name:      "log_check_in",
		Summary:   "log a check-in",
	}}).Render(context.Background(), &buf)
	if err != nil {
		t.Fatal(err)
	}
	body := buf.String()
	if !strings.Contains(body, "min-h-11") {
		t.Error("approval buttons smaller than 44px")
	}
}

func TestEmptyOffersStarterChips(t *testing.T) {
	var buf bytes.Buffer
	if err := Empty(users.User{DisplayName: "Ada"}, nil, CoachStats{}).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	body := buf.String()
	if !strings.Contains(body, "I want to get stronger") {
		t.Fatal("empty chat has no first-message chips")
	}
	if !strings.Contains(body, `name="draft"`) {
		t.Fatal("chips do not submit a draft")
	}
}

func TestStreamingBubbleKeepsACaret(t *testing.T) {
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")

	var pending bytes.Buffer
	if err := PendingExchange(id, "How did training go?", uuid.Nil).Render(context.Background(), &pending); err != nil {
		t.Fatal(err)
	}
	var resume bytes.Buffer
	if err := ResumeExchange(id).Render(context.Background(), &resume); err != nil {
		t.Fatal(err)
	}

	for name, body := range map[string]string{
		"pending": pending.String(),
		"resume":  resume.String(),
	} {
		if !strings.Contains(body, `data-streaming-caret`) {
			t.Errorf("%s stream has no caret", name)
		}
		if !strings.Contains(body, "motion-safe:animate-caret") {
			t.Errorf("%s caret is not the CLI blink", name)
		}
		// htmx 4 swaps unnamed SSE frames into the target the *connecting*
		// element resolves, so the wiring is split across two elements and
		// every piece has to be present for a token to reach the page.
		if !strings.Contains(body, `data-stream-sink`) {
			t.Errorf("%s lost the token sink", name)
		}
		if !strings.Contains(body, `hx-target="find [data-stream-sink]"`) {
			t.Errorf("%s does not point the stream at the sink", name)
		}
		if !strings.Contains(body, `hx-swap="beforeend"`) {
			t.Errorf("%s does not append tokens", name)
		}
		if !strings.Contains(body, `hx-sse:close="done"`) {
			t.Errorf("%s would reconnect forever after the reply ends", name)
		}
		// The refresh has to name the connecting element: "done" is dispatched
		// there, and without from: this hidden div never hears it.
		if !strings.Contains(body, `done from:#coach-stream-`+id.String()) {
			t.Errorf("%s does not refresh when the stream closes", name)
		}
		// sse-swap is removed in htmx 4; it would be silently ignored.
		if strings.Contains(body, "sse-swap") {
			t.Errorf("%s still carries sse-swap, which htmx 4 ignores", name)
		}
	}
}

func TestComposerPrefillsADraft(t *testing.T) {
	var buf bytes.Buffer
	err := Page(
		users.User{DisplayName: "Ada"},
		conversations.Conversation{ID: uuid.MustParse("11111111-1111-1111-1111-111111111111")},
		nil,
		nil,
		CoachStats{},
		nil,
		false,
		"Help me build a habit I'll actually keep.",
	).Render(context.Background(), &buf)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "Help me build a habit I&#39;ll actually keep.") &&
		!strings.Contains(buf.String(), "Help me build a habit I'll actually keep.") {
		t.Fatal("composer did not prefill the draft")
	}
}

// The mascot in the chat header is the one that reacts to the coach, and it
// only survives the sse-close swap because it is in the header rather than in
// the transcript. A refactor that moves it into the reply would still render,
// and would silently stop nodding.
func TestChatHeaderCarriesTheReactiveMascot(t *testing.T) {
	var buf bytes.Buffer
	err := Page(
		users.User{DisplayName: "Fernando"},
		conversations.Conversation{ID: uuid.MustParse("11111111-1111-1111-1111-111111111111")},
		nil,
		nil,
		CoachStats{},
		nil,
		false,
		"",
	).Render(context.Background(), &buf)
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()

	// The app shell renders a header of its own first, so anchor on the chat
	// header's own markup rather than on the first </header> in the document.
	_, afterOpen, found := strings.Cut(out, `<header class="muse-header`)
	if !found {
		t.Fatal("no chat header rendered")
	}
	header, _, found := strings.Cut(afterOpen, "</header>")
	if !found {
		t.Fatal("chat header is not closed")
	}
	if !strings.Contains(header, `id="chat-mascot"`) {
		t.Errorf("reactive mascot is not inside the header:\n%s", header)
	}
	if got := strings.Count(out, "/assets/js/shared/mascot/alpine.js"); got != 1 {
		t.Errorf("mascot script rendered %d times, want 1", got)
	}
}

// chatHeaderHTML renders a thread page and returns just the Muse header.
func chatHeaderHTML(t *testing.T, c conversations.Conversation) string {
	t.Helper()

	var buf bytes.Buffer
	if err := Page(users.User{DisplayName: "Fernando"}, c, nil, nil, CoachStats{}, nil, false, "").
		Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	_, afterOpen, found := strings.Cut(buf.String(), `<header class="muse-header`)
	if !found {
		t.Fatal("no Muse header rendered")
	}
	header, _, found := strings.Cut(afterOpen, "</header>")
	if !found {
		t.Fatal("Muse header is not closed")
	}
	return header
}

// The Muse header is the one place the coach is named: avatar, name, and a
// status line the stream can drive. Stage B only has to write `status`.
func TestMuseHeaderCarriesAvatarNameAndStatus(t *testing.T) {
	header := chatHeaderHTML(t, conversations.Conversation{ID: uuid.MustParse("11111111-1111-1111-1111-111111111111")})

	for _, want := range []string{
		`class="muse-avatar"`,
		`id="chat-mascot"`,
		// SizeLg: the 110px avatar, not the old 32px header chip.
		"size-40",
		`class="muse-scrim"`,
		`<span class="muse-pill-name">`,
		"Khepri",
		`x-text="status"`,
		">Ready</span>",
		"New chat",
		`aria-label="Show conversations"`,
		`aria-label="Delete conversation"`,
	} {
		if !strings.Contains(header, want) {
			t.Errorf("Muse header missing %q:\n%s", want, header)
		}
	}
	if strings.Contains(header, "size-8") {
		t.Error("header mascot is still the small chip")
	}
}

// x-text="status" needs a status in scope, or Alpine blanks the pill and
// throws. The root scope seeds it with the translated "Ready".
func TestChatRootSeedsTheStatus(t *testing.T) {
	var buf bytes.Buffer
	if err := Page(users.User{}, conversations.Conversation{ID: uuid.New()}, nil, nil, CoachStats{}, nil, false, "").
		Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "status: &#34;Ready&#34;") {
		t.Error("#chat-root x-data does not seed status")
	}
}

// Stage B: the bridge in alpine.js drives the header from the #chat-root
// scope. That needs the scope to hand itself over, the translated copy to be
// on the element, the ring to read phase, and the composer to be marked as
// the thing whose focus means "listening".
func TestChatPageWiresTheStatusBridge(t *testing.T) {
	var buf bytes.Buffer
	if err := Page(users.User{}, conversations.Conversation{ID: uuid.New()}, nil, nil, CoachStats{}, nil, false, "").
		Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	page := buf.String()

	for _, want := range []string{
		"NorthMascot.bindChat(this, this.$el)",
		"phase: &#39;idle&#39;",
		`data-status-ready="Ready"`,
		`data-status-listening="is listening"`,
		`data-status-thinking="is thinking"`,
		`data-status-writing="is writing"`,
		`data-status-snag="hit a snag"`,
		`class="muse-ring"`,
		`:data-phase="phase"`,
		`data-phase="idle"`,
		"data-muse-listen",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("chat page missing %q", want)
		}
	}
}

// The copy is the reader's language, not English baked into a script.
func TestStatusCopyFollowsTheLocale(t *testing.T) {
	var buf bytes.Buffer
	ctx := i18n.WithLocale(context.Background(), "pt-PT")
	if err := Page(users.User{}, conversations.Conversation{ID: uuid.New()}, nil, nil, CoachStats{}, nil, false, "").
		Render(ctx, &buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `data-status-thinking="está a pensar"`) {
		t.Error("pt-PT page does not carry the pt-PT status copy")
	}
}

// The coach is named once, in the header. Replies carry no avatar and no
// "Khepri" caption, stored or streaming.
func TestBubbleHasNoInlineAvatar(t *testing.T) {
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	renders := map[string]templ.Component{
		"stored":  Bubble(conversations.Message{Role: ai.RoleModel, Content: "A reply."}),
		"user":    Bubble(conversations.Message{Role: ai.RoleUser, Content: "A question."}),
		"pending": PendingExchange(id, "How did training go?", uuid.Nil),
		"resume":  ResumeExchange(id),
	}
	for name, c := range renders {
		var buf bytes.Buffer
		if err := c.Render(context.Background(), &buf); err != nil {
			t.Fatal(err)
		}
		body := buf.String()
		if strings.Contains(body, "north-mascot") || strings.Contains(body, "khepri-mascot.png") {
			t.Errorf("%s bubble renders an inline avatar", name)
		}
		if strings.Contains(body, ">Khepri<") {
			t.Errorf("%s bubble still carries the Khepri caption", name)
		}
		if !strings.Contains(body, "rounded-[24px]") {
			t.Errorf("%s bubble is not the Muse radius", name)
		}
	}

	var agent bytes.Buffer
	if err := Bubble(conversations.Message{Role: ai.RoleModel, Content: "A reply."}).Render(context.Background(), &agent); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(agent.String(), "max-w-[94%] ") || !strings.Contains(agent.String(), "bg-muse-agent") {
		t.Error("agent bubble is not the 94% muse agent bubble")
	}
	var user bytes.Buffer
	if err := Bubble(conversations.Message{Role: ai.RoleUser, Content: "Hi"}).Render(context.Background(), &user); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(user.String(), "max-w-[85%]") || !strings.Contains(user.String(), "bg-muse-user") {
		t.Error("user bubble is not the 85% muse user bubble")
	}
}

func renderFeedback(t *testing.T, m conversations.Message) string {
	t.Helper()

	var buf bytes.Buffer
	if err := MessageFeedback(m).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestUnratedReplyAsksTheQuestion(t *testing.T) {
	messageID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	conversationID := uuid.MustParse("11111111-1111-1111-1111-111111111111")

	body := renderFeedback(t, conversations.Message{
		ID:             messageID,
		ConversationID: conversationID,
		Role:           ai.RoleModel,
	})

	for _, want := range []string{
		"Did this help?",
		`id="feedback-22222222-2222-2222-2222-222222222222"`,
		`hx-post="/app/chat/11111111-1111-1111-1111-111111111111/messages/22222222-2222-2222-2222-222222222222/helpful"`,
		`hx-swap="outerHTML"`,
		`value="helpful"`,
		`value="unhelpful"`,
		// The action attribute is what makes this work with JavaScript off, so
		// it is part of the contract rather than decoration.
		`action="/app/chat/11111111-1111-1111-1111-111111111111/messages/22222222-2222-2222-2222-222222222222/helpful"`,
		`name="csrf_token"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in:\n%s", want, body)
		}
	}
}

func TestRatedReplyShowsTheAnswerAndOffersUndo(t *testing.T) {
	yes := true
	no := false
	id := uuid.MustParse("22222222-2222-2222-2222-222222222222")

	helpful := renderFeedback(t, conversations.Message{ID: id, Role: ai.RoleModel, Helpful: &yes})
	if !strings.Contains(helpful, "Marked helpful") {
		t.Errorf("a helpful rating does not say so:\n%s", helpful)
	}
	if strings.Contains(helpful, "Did this help?") {
		t.Error("an answered reply still asks the question")
	}
	// Undo has to be reachable, or a mistaken tap is permanent in the one
	// labelled column the product has.
	if !strings.Contains(helpful, `value="clear"`) || !strings.Contains(helpful, "Undo") {
		t.Errorf("no way to undo the answer:\n%s", helpful)
	}

	unhelpful := renderFeedback(t, conversations.Message{ID: id, Role: ai.RoleModel, Helpful: &no})
	if !strings.Contains(unhelpful, "Marked not helpful") {
		t.Errorf("an unhelpful rating does not say so:\n%s", unhelpful)
	}
}

// The person's own turns get no rating control: rating your own message is
// meaningless, and the server refuses it anyway.
func TestUserBubbleHasNoRatingControl(t *testing.T) {
	var buf bytes.Buffer
	err := Bubble(conversations.Message{
		ID:      uuid.MustParse("33333333-3333-3333-3333-333333333333"),
		Role:    ai.RoleUser,
		Content: "How should I train this week?",
	}).Render(context.Background(), &buf)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "Did this help?") {
		t.Error("a user message is offering a rating control")
	}
}
