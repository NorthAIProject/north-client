package messaging

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/analytics"
	"github.com/NorthAIProject/north-client/internal/coach"
	"github.com/NorthAIProject/north-client/internal/conversations"
	"github.com/NorthAIProject/north-client/internal/media"
	"github.com/NorthAIProject/north-client/internal/quota"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
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
	LatestExerciseRefs(ctx context.Context, user users.User, conversationID uuid.UUID) ([]string, error)
}

// Threads is how a platform message finds the thread it belongs in.
// Art resolves a catalogue slug to the slug its illustration is filed under.
//
// One method, because that is all the reply path needs and the mapping lives
// in a column: the catalogue and the artwork use different vocabularies, so a
// path cannot be built from the slug the coach looked up.
type Art interface {
	IllustrationFor(ctx context.Context, slug string) (string, bool)
}

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
	Consume(ctx context.Context, userID uuid.UUID, tier string, action quota.Action) (quota.Decision, error)
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
	voice     Voice
	files     Files
	transport Transport
	log       *slog.Logger
	funnel    *analytics.Funnel
	art       Art
	stats     Stats
	siteURL   string

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

	// Voice turns a voice note into words before the coach sees it. Nil
	// refuses voice notes in words, which is what a deployment with no
	// transcription endpoint should do.
	Voice Voice

	// Files fetches a recording's bytes when the adapter has left them on the
	// platform. Nil is correct for an adapter that downloads eagerly.
	Files Files

	// Transport delivers unsolicited messages (the morning briefing). Nil
	// makes Notify a no-op, which is what every process without a bot token
	// should do.
	Transport Transport

	Log *slog.Logger

	// Funnel records a linked chat as a connected source. Nil is a no-op.
	Funnel *analytics.Funnel

	// Stats answers /stats. Nil points the reader at the web app instead,
	// which is the right answer for a build with no insights service rather
	// than handing a slash command to the coach as prose.
	Stats Stats

	// Art and SiteURL together turn an exercise the coach looked up into a
	// picture the platform can show. Either one empty means replies stay text
	// only, which is the correct degradation rather than a broken link.
	Art     Art
	SiteURL string

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
		voice:       opts.Voice,
		files:       opts.Files,
		transport:   opts.Transport,
		log:         opts.Log,
		funnel:      opts.Funnel,
		art:         opts.Art,
		stats:       opts.Stats,
		siteURL:     opts.SiteURL,
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

	link, err := s.links.ClaimUpdate(ctx, in.Platform, in.ExternalID, in.UpdateID, in.AccountID)
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

	// Telegram has no HTTP middleware to resolve a language, so this is where
	// the account's own setting joins the request. Everything below — the
	// approval prompt, the quota refusals, the link and unlink replies — reads
	// it from here.
	//
	// Set after the user is loaded and before any reply is composed, which is
	// the same order auth.LoadUser establishes for the web app.
	ctx = i18n.WithLocale(ctx, string(user.Locale))

	// An account that has not finished onboarding has no coaching style, no
	// goals and no first conversation, so the coach would answer from nothing.
	// Better to say where to go than to answer badly.
	if user.NeedsOnboarding() {
		return OutboundMessage{Text: i18n.T(ctx, "tg.onboard")}, nil
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
		s.funnel.SourceConnected(ctx, userID, analytics.SourceTelegram)
		// The account has a language from signup, and this is the first thing
		// the bot ever says to it — so it is worth answering in that language
		// rather than in whatever the previous line happened to leave on ctx.
		return OutboundMessage{
			Text: i18n.Tf(i18n.WithLocale(ctx, string(user.Locale)), "tg.linked", user.Email),
		}, nil

	case errors.Is(err, apperr.ErrConflict):
		return OutboundMessage{Text: i18n.T(ctx, "tg.takenlink")}, nil

	case errors.Is(err, apperr.ErrForbidden):
		return OutboundMessage{Text: i18n.T(ctx, "tg.toomany")}, nil

	case errors.Is(err, apperr.ErrNotFound):
		return OutboundMessage{Text: "I do not know you yet. Open Khepri, go to Settings → Agent connections, and send me the code it shows."}, nil

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

	// A voice note becomes its words here, before anything is metered as a
	// coach message, so what follows is an ordinary typed turn.
	said, heard, err := s.transcribeVoice(ctx, user, &in)
	if err != nil {
		return OutboundMessage{}, err
	}
	if !heard {
		return said, nil
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
				msg = i18n.T(ctx, "tg.photofailed")
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

// Notify sends an unsolicited line of text to every linked chat for this
// account.
//
// Used by the morning briefing and the nudges, both of which have only words
// to send. A missing transport or no linked chat is success: there is nobody
// to tell, not a failed generation.
func (s *Service) Notify(ctx context.Context, userID uuid.UUID, text string) error {
	return s.NotifyMessage(ctx, userID, OutboundMessage{Text: text})
}

// NotifyMessage is Notify for a message that is more than a line of text.
//
// The insights digest carries a rendered card, and the string-only signature
// above would have dropped it on the floor with nothing to show for it. Kept
// as two functions rather than one because two of the three callers genuinely
// have only a string, and making them construct a message to pass it would be
// ceremony for its own sake.
func (s *Service) NotifyMessage(ctx context.Context, userID uuid.UUID, msg OutboundMessage) error {
	if s.transport == nil || s.links == nil {
		return nil
	}
	// Nothing to say is not a failure. A message with neither words nor a
	// picture is what a caller sends when a window turned out to be empty.
	if strings.TrimSpace(msg.Text) == "" && len(msg.Photo) == 0 {
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
		if err := s.transport.Send(ctx, link.ExternalID, msg); err != nil {
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
		return confirmationMessage(ctx, pending, i18n.T(ctx, "tg.confirm.again")+"\n\n"), nil
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
		return confirmationMessage(ctx, pending, text), nil
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

	out := OutboundMessage{Text: text}
	if url, credit, ok := s.illustration(ctx, user, conversation); ok {
		out.Animation = url
		out.AnimationCredit = credit
	}
	return out, nil
}

// illustration finds the looping artwork for whatever exercise the coach just
// looked up, if it looked one up and there is any.
//
// Read after the stream rather than from it, for the same reason
// PendingApproval is: the coach's pump does not forward tool calls to its
// caller, so out here the only trace of the lookup is what was persisted.
//
// Every failure is a quiet no-op. A missing picture is a reply without a
// picture; it is never worth losing the words over.
func (s *Service) illustration(ctx context.Context, user users.User, conversation conversations.Conversation) (url, credit string, ok bool) {
	if s.art == nil || s.siteURL == "" {
		return "", "", false
	}

	slugs, err := s.coach.LatestExerciseRefs(ctx, user, conversation.ID)
	if err != nil {
		s.log.Warn("could not read the exercises this reply looked up",
			"error", err, "conversation_id", conversation.ID)
		return "", "", false
	}

	// The first is the one the answer is about. A reply that looked up three
	// movements is a comparison, and picking one of them to illustrate would
	// be arbitrary.
	for _, slug := range slugs {
		illustration, found := s.art.IllustrationFor(ctx, slug)
		if !found {
			continue
		}
		return strings.TrimRight(s.siteURL, "/") + "/assets/exercises/" + illustration + "/loop.gif",
			artworkCredit, true
	}
	return "", "", false
}

// meter spends one coach message against the same budget the web chat uses.
func (s *Service) meter(ctx context.Context, user users.User) (OutboundMessage, bool, error) {
	if s.quotas == nil {
		return OutboundMessage{}, true, nil
	}

	decision, err := s.quotas.Consume(ctx, user.ID, string(user.Tier), quota.CoachMessage)
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
	return OutboundMessage{Text: quotaMessage(ctx, decision)}, false, nil
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
func confirmationMessage(ctx context.Context, pending coach.PendingCall, prefix string) OutboundMessage {
	var b strings.Builder
	b.WriteString(prefix)
	if prefix != "" && !strings.HasSuffix(prefix, "\n") {
		b.WriteString("\n\n")
	}
	b.WriteString(i18n.T(ctx, "tg.confirm") + "\n")
	for _, call := range pending.Calls {
		b.WriteString("\n• ")
		b.WriteString(describeCall(call))
	}

	return OutboundMessage{
		Text: b.String(),
		Options: []Option{
			{Label: i18n.T(ctx, "tg.confirm.yes"), Value: AnswerApprove},
			{Label: i18n.T(ctx, "tg.confirm.no"), Value: AnswerDecline},
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
func quotaMessage(ctx context.Context, decision quota.Decision) string {
	switch {
	case decision.RetryAfter < time.Minute:
		return i18n.T(ctx, "tg.quota.minute")
	case decision.RetryAfter < time.Hour:
		return i18n.Tf(ctx, "tg.quota.n", int(decision.RetryAfter.Minutes()))
	default:
		return i18n.T(ctx, "tg.quota.hour")
	}
}
