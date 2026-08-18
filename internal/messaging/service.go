package messaging

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/coach"
	"github.com/NorthAIProject/north-client/internal/conversations"
	"github.com/NorthAIProject/north-client/internal/media"
	"github.com/NorthAIProject/north-client/internal/quota"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/ratelimit"
	"github.com/NorthAIProject/north-client/internal/users"
)

// Coach is the slice of the coach this package needs.
//
// An interface for the reason onboarding.Coach gives: taking *coach.Service
// directly would drag the whole context builder into this package's tests to
// answer one sentence. It is also the honest statement of the dependency —
// messaging asks the coach five things and builds no prompt.
type Coach interface {
	StartConversation(ctx context.Context, userID uuid.UUID) (conversations.Conversation, error)
	SendMessage(ctx context.Context, user users.User, conversationID uuid.UUID, text string) (<-chan ai.StreamChunk, error)
	SendIncoming(ctx context.Context, user users.User, conversationID uuid.UUID, in coach.Incoming) (<-chan ai.StreamChunk, error)
	PendingApproval(ctx context.Context, user users.User, conversationID uuid.UUID) (coach.PendingCall, bool, error)
	ResolvePending(ctx context.Context, user users.User, conversationID, messageID uuid.UUID, approve bool) error
	Resume(ctx context.Context, user users.User, conversationID uuid.UUID) (<-chan ai.StreamChunk, error)
}

// Threads is how a platform message finds the thread it belongs in.
type Threads interface {
	List(ctx context.Context, userID uuid.UUID, limit int) ([]conversations.Conversation, error)
}

// Users resolves a linked account to the user the coach needs.
type Users interface {
	ByID(ctx context.Context, id uuid.UUID) (users.User, error)
}

// Quotas meters platform turns against the same budget the web chat spends.
//
// An interface only so a test can put a budget at its limit without a clock.
type Quotas interface {
	Consume(ctx context.Context, userID uuid.UUID, action quota.Action) (quota.Decision, error)
}

// threadSearchDepth is how far back to look for a chat thread to continue.
//
// Ten, because the newest conversations may be reflections, which a platform
// message must not join — they end. Beyond ten the thread is old enough that
// starting a fresh one is the better answer anyway.
const threadSearchDepth = 10

// redeemAttemptsPerMinute bounds guesses at a link code from one chat.
//
// Six is generous for a person retyping a code they can see and hopeless for
// anyone working through a 40-bit space.
const redeemAttemptsPerMinute = 6

// Images stores a photo so the coach can see it this turn.
type Images interface {
	UploadImage(ctx context.Context, userID uuid.UUID, filename string, size int64, body io.Reader) (media.Media, error)
}

type Service struct {
	coach     Coach
	threads   Threads
	users     Users
	links     *Repository
	quotas    Quotas
	images    Images
	transport Transport
	log       *slog.Logger

	redeemLimit *ratelimit.Limiters

	// now is overridable so a test can expire a code without sleeping.
	now func() time.Time
}

type Options struct {
	Coach   Coach
	Threads Threads
	Users   Users
	Links   *Repository

	// Quotas meters platform turns. Nil leaves them unmetered, which is only
	// correct in a test: a live adapter without this is a way to spend a paid
	// provider's budget from outside the web app's limits.
	Quotas Quotas

	// Images stores a photo from a platform. Nil refuses the file and asks
	// the person to use the web app.
	Images Images

	// Transport delivers unsolicited messages (the morning briefing). Nil
	// makes Notify a no-op, which is what every process without a bot token
	// should do.
	Transport Transport

	Log *slog.Logger

	// Now defaults to time.Now.
	Now func() time.Time
}

func NewService(opts Options) *Service {
	s := &Service{
		coach:       opts.Coach,
		threads:     opts.Threads,
		users:       opts.Users,
		links:       opts.Links,
		quotas:      opts.Quotas,
		images:      opts.Images,
		transport:   opts.Transport,
		log:         opts.Log,
		redeemLimit: ratelimit.New(redeemAttemptsPerMinute),
		now:         opts.Now,
	}
	if s.log == nil {
		s.log = slog.Default()
	}
	if s.now == nil {
		s.now = time.Now
	}
	return s
}

// Handle turns one inbound platform message into one reply.
//
// This is the whole adapter. It resolves who is speaking, meters the turn,
// finds the thread, and hands the text to the coach — it does not build a
// prompt, choose a provider, or know what a tool is. Everything platform
// specific lives on the other side of InboundMessage and Transport.
func (s *Service) Handle(ctx context.Context, in InboundMessage) (OutboundMessage, error) {
	in.Text = strings.TrimSpace(in.Text)

	link, err := s.links.ClaimUpdate(ctx, in.Platform, in.ExternalID, in.UpdateID)
	switch {
	case err == nil:
		// Linked, and this delivery is new.
	case errors.Is(err, apperr.ErrNotFound):
		// Either not linked or already answered. Only now is it worth a second
		// query to find out which.
		if _, getErr := s.links.Get(ctx, in.Platform, in.ExternalID); getErr == nil {
			s.log.Info("messaging ignored a redelivered update",
				"platform", in.Platform, "update_id", in.UpdateID)
			return OutboundMessage{Silent: true}, nil
		} else if !errors.Is(getErr, apperr.ErrNotFound) {
			return OutboundMessage{}, getErr
		}
		return s.handleUnlinked(ctx, in)
	default:
		return OutboundMessage{}, err
	}

	user, err := s.users.ByID(ctx, link.UserID)
	if err != nil {
		return OutboundMessage{}, apperr.Wrap(err, "messaging: load linked user")
	}

	// An account that has not finished onboarding has no coaching style, no
	// goals and no first conversation, so the coach would answer from nothing.
	// Better to say where to go than to answer badly.
	if user.NeedsOnboarding() {
		return OutboundMessage{Text: "Finish setting up your account in the DuxAI web app first, then message me again."}, nil
	}

	// Before the thread is resolved and before anything is metered: a command
	// is not a coach turn. Pressing START would otherwise spend a message
	// asking a model to interpret "/start", and asking for help mid-
	// confirmation would look like an answer to it.
	if out, handled, err := s.runCommand(ctx, user, in); handled {
		return out, err
	}

	return s.coachTurn(ctx, user, in)
}

// handleUnlinked treats the message as a link code and nothing else.
//
// Deliberately the only thing an unlinked chat can do. Reaching the coach
// first and asking questions later would let anyone who finds the bot spend
// somebody's model budget, and there is no account yet to charge it to.
func (s *Service) handleUnlinked(ctx context.Context, in InboundMessage) (OutboundMessage, error) {
	userID, err := s.redeem(ctx, in)
	switch {
	case err == nil:
		user, loadErr := s.users.ByID(ctx, userID)
		if loadErr != nil {
			return OutboundMessage{}, apperr.Wrap(loadErr, "messaging: load linked user")
		}
		s.log.Info("messaging linked a chat", "platform", in.Platform, "user_id", userID)
		return OutboundMessage{Text: fmt.Sprintf(
			"Linked to %s. Message me whenever — I have the same memory and goals as the web app.",
			user.Email)}, nil

	case errors.Is(err, apperr.ErrConflict):
		return OutboundMessage{Text: "This chat is already linked to another DuxAI account."}, nil

	case errors.Is(err, apperr.ErrForbidden):
		return OutboundMessage{Text: "Too many attempts. Wait a minute and try your code again."}, nil

	case errors.Is(err, apperr.ErrNotFound):
		return OutboundMessage{Text: "I do not know you yet. Open DuxAI, go to Settings → Agent connections, and send me the code it shows."}, nil

	default:
		return OutboundMessage{}, err
	}
}

// coachTurn is the linked path: meter, find the thread, answer.
func (s *Service) coachTurn(ctx context.Context, user users.User, in InboundMessage) (OutboundMessage, error) {
	conversation, err := s.resolveThread(ctx, user)
	if err != nil {
		return OutboundMessage{}, err
	}

	// A waiting write is answered before anything else. Starting a new turn
	// while one is pending would leave the write suspended forever, and the
	// person believing they had cancelled it.
	pending, waiting, err := s.coach.PendingApproval(ctx, user, conversation.ID)
	if err != nil {
		return OutboundMessage{}, err
	}
	if waiting {
		return s.answerPending(ctx, user, conversation, pending, in.Text)
	}

	refusal, allowed, err := s.meter(ctx, user)
	if err != nil {
		return OutboundMessage{}, err
	}
	if !allowed {
		return refusal, nil
	}

	incoming, refuse, err := s.incomingFrom(ctx, user.ID, in)
	if err != nil {
		return OutboundMessage{}, err
	}
	if refuse != "" {
		return OutboundMessage{Text: refuse}, nil
	}

	stream, err := s.coach.SendIncoming(ctx, user, conversation.ID, incoming)
	if err != nil {
		return OutboundMessage{}, err
	}
	return s.reply(ctx, user, conversation, stream)
}

func (s *Service) incomingFrom(ctx context.Context, userID uuid.UUID, in InboundMessage) (coach.Incoming, string, error) {
	out := coach.Incoming{Text: in.Text, Source: coach.SourceTelegram}
	if in.Attachment == nil || len(in.Attachment.Bytes) == 0 {
		return out, "", nil
	}
	if s.images == nil {
		return coach.Incoming{}, "I can see you sent a photo, but I cannot store it right now. Try the web app.", nil
	}

	name := in.Attachment.Name
	if name == "" {
		name = "photo.jpg"
	}
	stored, err := s.images.UploadImage(ctx, userID, name, int64(len(in.Attachment.Bytes)), bytes.NewReader(in.Attachment.Bytes))
	if err != nil {
		var fieldErrs apperr.FieldErrors
		if apperr.As(err, &fieldErrs) {
			msg := fieldErrs.Messages()["attachment"]
			if msg == "" {
				msg = "That photo could not be stored."
			}
			return coach.Incoming{}, msg, nil
		}
		return coach.Incoming{}, "I could not store that photo. Try again?", nil
	}

	out.Attachments = []conversations.Attachment{{
		MediaID:  stored.ID,
		Kind:     stored.Kind,
		MIMEType: stored.MIMEType,
		Name:     stored.OriginalName,
	}}
	return out, "", nil
}

// Notify sends an unsolicited message to every linked chat for this account.
//
// Used by the morning briefing. A missing transport or no linked chat is
// success: there is nobody to tell, not a failed generation.
func (s *Service) Notify(ctx context.Context, userID uuid.UUID, text string) error {
	if s.transport == nil || s.links == nil || strings.TrimSpace(text) == "" {
		return nil
	}

	links, err := s.links.ListByUser(ctx, userID)
	if err != nil {
		return err
	}

	for _, link := range links {
		if link.Platform != s.transport.Platform() {
			continue
		}
		if err := s.transport.Send(ctx, link.ExternalID, OutboundMessage{Text: text}); err != nil {
			s.log.Warn("messaging notify failed",
				"error", err,
				"user_id", userID,
				"platform", link.Platform)
		}
	}
	return nil
}

// answerPending interprets the message as yes or no to a waiting write.
func (s *Service) answerPending(ctx context.Context, user users.User, conversation conversations.Conversation, pending coach.PendingCall, text string) (OutboundMessage, error) {
	approve, understood := parseAnswer(text)
	if !understood {
		// Re-asking rather than treating it as a new question: an ambiguous
		// reply must not silently abandon a write the person was asked about.
		return confirmationMessage(pending, "I still need a yes or a no first.\n\n"), nil
	}

	if err := s.coach.ResolvePending(ctx, user, conversation.ID, pending.MessageID, approve); err != nil {
		return OutboundMessage{}, err
	}

	// No quota spent here, matching the web resume route: the person asked one
	// question, and answering a confirmation is not a second one.
	stream, err := s.coach.Resume(ctx, user, conversation.ID)
	if err != nil {
		return OutboundMessage{}, err
	}
	return s.reply(ctx, user, conversation, stream)
}

// reply drains a coach stream and turns it into one platform message.
func (s *Service) reply(ctx context.Context, user users.User, conversation conversations.Conversation, stream <-chan ai.StreamChunk) (OutboundMessage, error) {
	text, streamErr := collect(stream)

	// Checked after the stream closes, because a suspended write looks exactly
	// like a short reply from out here: the coach's pump never forwards tool
	// calls to its caller. Without this the person would get silence — which
	// is the bug ask_coach still has.
	pending, waiting, err := s.coach.PendingApproval(ctx, user, conversation.ID)
	if err != nil {
		return OutboundMessage{}, err
	}
	if waiting {
		return confirmationMessage(pending, text), nil
	}

	if strings.TrimSpace(text) == "" {
		if streamErr != nil {
			return OutboundMessage{}, streamErr
		}
		return OutboundMessage{Text: "I could not think of a reply to that. Try asking again?"}, nil
	}

	if streamErr != nil {
		// Partial text is worth more than an error: the person can read what
		// arrived, and the whole reply is in the web thread either way.
		s.log.Warn("messaging reply ended early", "error", streamErr, "conversation_id", conversation.ID)
		text += "\n\n(That answer got cut short. The full thread is in the web app.)"
	}
	return OutboundMessage{Text: text}, nil
}

// meter spends one coach message against the same budget the web chat uses.
func (s *Service) meter(ctx context.Context, user users.User) (OutboundMessage, bool, error) {
	if s.quotas == nil {
		return OutboundMessage{}, true, nil
	}

	decision, err := s.quotas.Consume(ctx, user.ID, quota.CoachMessage)
	if err != nil {
		// Consume fails open by design; an error here is a counter problem,
		// not a person's problem.
		s.log.Warn("messaging could not check quota", "error", err, "user_id", user.ID)
		return OutboundMessage{}, true, nil
	}
	if decision.Allowed {
		return OutboundMessage{}, true, nil
	}

	s.log.Warn("messaging turn refused by quota", "user_id", user.ID)
	return OutboundMessage{Text: quotaMessage(decision)}, false, nil
}

// resolveThread finds the conversation a platform message belongs in.
//
// The newest live chat thread, which is the same one the web app is showing.
// That is what "one brain, many mouths" has to mean in practice: asking on the
// phone and asking in the browser continue each other rather than starting two
// histories that each know half the story.
//
// Reflections are skipped because they end, and a message arriving after one
// closed would be refused.
func (s *Service) resolveThread(ctx context.Context, user users.User) (conversations.Conversation, error) {
	recent, err := s.threads.List(ctx, user.ID, threadSearchDepth)
	if err != nil {
		return conversations.Conversation{}, apperr.Wrap(err, "messaging: list conversations")
	}
	for _, c := range recent {
		if c.Kind == conversations.KindChat && !c.Ended() {
			return c, nil
		}
	}
	return s.coach.StartConversation(ctx, user.ID)
}

// collect drains a coach stream into one message.
//
// A messaging platform has no notion of a token arriving, so the whole reply is
// assembled before any of it is sent. A mid-stream failure keeps the text that
// arrived before it: a partial answer is worth more than an error.
func collect(stream <-chan ai.StreamChunk) (string, error) {
	var b strings.Builder
	var streamErr error
	for chunk := range stream {
		if chunk.Err != nil {
			streamErr = chunk.Err
			continue
		}
		b.WriteString(chunk.Text)
	}
	return strings.TrimSpace(b.String()), streamErr
}

// confirmationMessage renders a waiting write as a question with two answers.
//
// The call is described in words rather than named, for the reason the web
// card does it: "log a check-in" is a very different request from "log a
// check-in saying the week went badly".
func confirmationMessage(pending coach.PendingCall, prefix string) OutboundMessage {
	var b strings.Builder
	b.WriteString(prefix)
	if prefix != "" && !strings.HasSuffix(prefix, "\n") {
		b.WriteString("\n\n")
	}
	b.WriteString("Before I do this, can you confirm?\n")
	for _, call := range pending.Calls {
		b.WriteString("\n• ")
		b.WriteString(describeCall(call))
	}

	return OutboundMessage{
		Text: b.String(),
		Options: []Option{
			{Label: "Yes, do it", Value: AnswerApprove},
			{Label: "No", Value: AnswerDecline},
		},
	}
}

func describeCall(call ai.ToolCall) string {
	name := strings.ReplaceAll(call.Name, "_", " ")
	args := strings.TrimSpace(string(call.Arguments))
	if args == "" || args == "{}" {
		return name
	}
	return name + " " + args
}

// parseAnswer reads a yes or a no out of a chat reply.
//
// People do not answer with one word. "yes please do", "no thanks" and "go
// ahead" are all ordinary, so this looks for a decisive word anywhere in the
// sentence rather than matching the whole of it.
//
// Refusals are checked before approvals, and that ordering is the safety
// property rather than a detail: "please don't" contains both an approving word
// and a refusing one, and reading it as approval would run a write nobody
// agreed to. Every ambiguity here resolves towards not writing — the worst case
// is being asked again.
func parseAnswer(text string) (approve, understood bool) {
	// Exact first: a button value is unambiguous and should not be subject to
	// any of the guessing below.
	switch strings.TrimSpace(text) {
	case AnswerApprove:
		return true, true
	case AnswerDecline:
		return false, true
	}

	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && r != '\''
	})

	for _, w := range words {
		if refusals[w] {
			return false, true
		}
	}
	for _, w := range words {
		if approvals[w] {
			return true, true
		}
	}
	return false, false
}

// refusals and approvals are the words people actually type.
//
// Deliberately short. A long list is a long list of ways to misread somebody,
// and anything not here is answered with the question again rather than a
// guess.
var (
	refusals = map[string]bool{
		"no": true, "n": true, "nope": true, "nah": true, "not": true,
		"don't": true, "dont": true, "stop": true, "cancel": true,
		"never": true, "decline": true, "wait": true,
	}

	approvals = map[string]bool{
		"yes": true, "y": true, "yep": true, "yeah": true, "yup": true,
		"ok": true, "okay": true, "sure": true, "confirm": true,
		"approve": true, "go": true, "ahead": true,
	}
)

// quotaMessage names a wait rather than a limit, matching the web chat's
// refusal: the number a person needs is when they can carry on.
func quotaMessage(decision quota.Decision) string {
	switch {
	case decision.RetryAfter < time.Minute:
		return "You have reached your coach message limit. Try again in less than a minute."
	case decision.RetryAfter < time.Hour:
		return fmt.Sprintf("You have reached your coach message limit. Try again in %d minutes.", int(decision.RetryAfter.Minutes()))
	default:
		return "You have reached your coach message limit. Try again in about an hour."
	}
}
