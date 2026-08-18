package coach

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/conversations"
	"github.com/NorthAIProject/north-client/internal/jobs"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	"github.com/NorthAIProject/north-client/internal/shared/toolsurface"
	"github.com/NorthAIProject/north-client/internal/users"
)

// ToolRunner is the coach's view of internal/agent's registry: the tools a
// model may call, and a way to run them.
//
// An interface rather than the concrete type because agent depends on the
// feature slices — calculator, goals, meals — and several of those contribute
// a ContextSource back to this package. Importing agent here would close that
// loop.
type ToolRunner interface {
	// Tools is what the model is shown it can call.
	Tools() []ai.Tool

	// InvokeAll runs a turn's calls in order and returns their results. The
	// user id comes from the authenticated session, never from the model.
	InvokeAll(ctx context.Context, userID uuid.UUID, calls []ai.ToolCall) []ai.ToolResult

	// IsReadOnly reports whether a named tool changes nothing.
	//
	// The loop asks before running anything: a tool that writes is suspended
	// and shown to the person instead. An unknown name answers false, so the
	// unrecognised case is the careful one.
	IsReadOnly(name string) bool
}

// ClientSource yields the provider a user brought themselves.
//
// An interface for the same reason ToolRunner is one: the implementation lives
// in a slice this package must not import, and a stub makes the fallback
// behaviour testable without a database or a network.
type ClientSource interface {
	// For returns the user's own client, or (nil, nil) when they have not
	// configured one — which is the normal case and is not an error.
	For(ctx context.Context, userID uuid.UUID) (ai.Client, error)

	// NoteFailure records that the user's provider refused, so the settings
	// page can tell them instead of leaving them to notice. The reason is a
	// summary written by the coach, never the provider's response body, which
	// can echo the key back.
	NoteFailure(ctx context.Context, userID uuid.UUID, reason string)
}

// generationTimeout bounds a detached generation. Long, because a large model
// answering a considered question is slow; bounded, because a stuck provider
// must not leak a goroutine forever.
const generationTimeout = 5 * time.Minute

// persistTimeout bounds the write that saves a completed reply.
const persistTimeout = 15 * time.Second

// toolRounds bounds how many times the coach may run tools and go back to the
// model within one turn.
//
// Bounded because the loop is the model deciding when to stop, and a model
// that keeps asking is an unbounded bill against a paid provider. Five is more
// than any current capability chain needs — the deepest is search, read, then
// answer — and low enough that a loop costs a few calls rather than a budget.
const toolRounds = 5

// Service turns a user's message into a coached reply.
//
// Everything conversational passes through here: the web handler, and later the
// Telegram and MCP adapters, all call SendMessage. None of them builds a prompt
// or touches a provider.
type Service struct {
	registry      *ai.Registry
	conversations *conversations.Service
	contextB      *ContextBuilder
	promptB       *PromptBuilder
	queue         *jobs.Queue
	tools         ToolRunner
	declines      DeclineRecorder

	// chains decides which providers serve a given user, in what order.
	chains ai.ChainSet

	// own yields a user's bring-your-own provider. Nil disables the path
	// entirely, which is what every process without a sealer gets.
	own ClientSource

	// analytics reports LLM calls and tool runs to PostHog. Nil client makes
	// every call a no-op — see Analytics.
	analytics *Analytics

	// attachments loads a stored photo so the current turn can show it to the
	// model. Nil leaves files as text notes only, which is what every test
	// that does not send a photo gets.
	attachments AttachmentLoader

	// inbox records a web-bell note when a reply arrived from another surface.
	// Nil disables it.
	inbox Inbox

	model     string
	fastModel string
}

// Inbox is the nudge table, as seen from the coach.
type Inbox interface {
	Raise(ctx context.Context, user users.User, kind, dedupe, title, body, href string) error
}

// InboxFunc lets a method be used as an Inbox.
type InboxFunc func(ctx context.Context, user users.User, kind, dedupe, title, body, href string) error

func (f InboxFunc) Raise(ctx context.Context, user users.User, kind, dedupe, title, body, href string) error {
	return f(ctx, user, kind, dedupe, title, body, href)
}

// Incoming is one turn the person just sent: words, a file, or both.
type Incoming struct {
	Text        string
	Attachments []conversations.Attachment

	// Source is "telegram" when the turn arrived from a linked chat. The
	// web bell is told about those replies; Telegram is not, because it
	// already has the answer.
	Source string
}

const SourceTelegram = "telegram"

// AttachmentLoader reads a stored file so the current turn can be shown to
// the model. The media service satisfies it; a test stubs it.
type AttachmentLoader interface {
	LoadInline(ctx context.Context, userID, mediaID uuid.UUID) (mime string, data []byte, err error)
}

type Options struct {
	Registry       *ai.Registry
	Conversations  *conversations.Service
	ContextBuilder *ContextBuilder
	PromptBuilder  *PromptBuilder

	// Queue receives post-turn memory extraction jobs. Nil disables extraction
	// (tests and processes that have no worker).
	Queue *jobs.Queue

	// Chains decides which providers serve a given user, in what order. The
	// zero value resolves every tier to an empty chain, so callers that want a
	// coach at all must supply one.
	Chains ai.ChainSet

	// Tools the coach may call mid-answer. Nil leaves the coach answering
	// purely from its context, which is what it did before tools existed.
	Tools ToolRunner

	// Declines keeps the account of writes a person refused. Nil records
	// nothing; the refusal still reaches the model either way.
	Declines DeclineRecorder

	// Own yields a user's own provider, tried ahead of Chains. Nil leaves
	// every user on North's providers, which is what a deployment with no
	// encryption key gets.
	Own ClientSource

	// Analytics reports LLM calls and tool runs to PostHog. Nil leaves the
	// coach exactly as it was before AI Observability existed.
	Analytics *Analytics

	// Attachments loads a stored photo onto the current turn. Nil leaves
	// files as a text note in history, which is correct for every caller
	// that does not send one.
	Attachments AttachmentLoader

	// Inbox records a web note when a reply arrived from Telegram. Nil
	// leaves the bell unchanged.
	Inbox Inbox

	// Model answers conversations. FastModel handles cheap side work such as
	// naming a thread, which does not need the expensive model. Both may be
	// empty, which lets whichever provider answers use its own default — the
	// only sane behaviour when the chain spans several vendors.
	Model     string
	FastModel string
}

func (s *Service) WithInbox(in Inbox) *Service {
	s.inbox = in
	return s
}

func NewService(opts Options) *Service {
	return &Service{
		registry:      opts.Registry,
		conversations: opts.Conversations,
		contextB:      opts.ContextBuilder,
		promptB:       opts.PromptBuilder,
		queue:         opts.Queue,
		chains:        opts.Chains,
		tools:         opts.Tools,
		declines:      opts.Declines,
		own:           opts.Own,
		analytics:     opts.Analytics,
		attachments:   opts.Attachments,
		inbox:         opts.Inbox,
		model:         opts.Model,
		fastModel:     opts.FastModel,
	}
}

// SendMessage stores the user's message, asks the coach, and streams the reply.
//
// The returned channel stops delivering when ctx is cancelled, but generation
// does not stop with it: the model keeps writing on a detached context and the
// finished reply is saved. Closing a tab mid-answer therefore loses the live
// view, not the answer — reopening the conversation shows it in full.
//
// The alternative, tying generation to the request, means a user who switches
// apps on their phone silently loses the reply they were waiting for.
func (s *Service) SendMessage(ctx context.Context, user users.User, conversationID uuid.UUID, text string) (<-chan ai.StreamChunk, error) {
	return s.SendIncoming(ctx, user, conversationID, Incoming{Text: text})
}

// SendIncoming is SendMessage for a turn that may carry a file.
func (s *Service) SendIncoming(ctx context.Context, user users.User, conversationID uuid.UUID, in Incoming) (<-chan ai.StreamChunk, error) {
	if err := conversations.ValidateTurn(in.Text, len(in.Attachments) > 0); err != nil {
		return nil, err
	}

	conversation, err := s.conversations.Get(ctx, conversationID, user.ID)
	if err != nil {
		return nil, err
	}
	if conversation.Ended() {
		return nil, apperr.Wrap(apperr.ErrConflict, "this reflection has ended")
	}

	if _, err = s.conversations.AppendUserMessage(ctx, conversationID, in.Text, in.Attachments); err != nil {
		return nil, err
	}

	query := strings.TrimSpace(in.Text)
	if query == "" && len(in.Attachments) > 0 {
		query = conversations.AttachmentNote(in.Attachments[0])
	}

	// Built after the user's message is stored, so the model sees the turn it
	// is answering as part of the history rather than as a special case.
	coachCtx, err := s.contextB.Build(ctx, ContextRequest{
		User:           user,
		ConversationID: conversationID,
		Query:          query,
	})
	if err != nil {
		return nil, err
	}

	system, err := s.systemPrompt(coachCtx, conversation)
	if err != nil {
		return nil, err
	}

	// Detached from the request so a disconnect does not abort generation. The
	// logger is preserved, so these lines stay attached to the request that
	// started them.
	genCtx, cancelGen := context.WithTimeout(context.WithoutCancel(ctx), generationTimeout)

	messages := conversations.ToAIMessages(coachCtx.RecentMessages)
	messages = s.hydrateCurrentTurn(ctx, user, coachCtx.RecentMessages, messages)

	req := ai.Request{
		Model:    s.model,
		System:   system,
		Messages: messages,
		Tools:    s.toolsFor(conversation),
	}

	stream, client, err := s.startChat(ctx, genCtx, user, req)
	if err != nil {
		cancelGen()
		return nil, apperr.Wrap(err, "coach: start reply")
	}

	out := make(chan ai.StreamChunk)

	// One trace id per turn: every generation and tool span this message
	// produces, however many rounds it takes, shares it — that is what lets
	// PostHog group them into one trace instead of one row per model call.
	go s.pump(ctx, genCtx, cancelGen, stream, out, pumpTarget{
		conversation: conversation,
		user:         user,
		provider:     client.Name(),
		firstMessage: firstMessage(in),
		source:       in.Source,
		client:       client,
		request:      req,
		offeredRefs:  coachCtx.OfferedRefs(),
		traceID:      uuid.New().String(),
	})

	return out, nil
}

func (s *Service) noteTelegramReply(ctx context.Context, target pumpTarget, text string) {
	if s.inbox == nil || target.source != SourceTelegram {
		return
	}
	preview := strings.TrimSpace(text)
	if len(preview) > 140 {
		preview = strings.TrimSpace(preview[:140]) + "…"
	}
	_ = s.inbox.Raise(ctx, target.user,
		"coach_reply",
		target.conversation.ID.String(),
		"See what I said",
		preview,
		"/app/chat/"+target.conversation.ID.String(),
	)
}

func firstMessage(in Incoming) string {
	if text := strings.TrimSpace(in.Text); text != "" {
		return text
	}
	if len(in.Attachments) > 0 {
		return conversations.AttachmentNote(in.Attachments[0])
	}
	return ""
}

// hydrateCurrentTurn puts the latest photo's bytes on the last user message.
//
// Only the current turn: every earlier file is already a text note from
// ToAIMessages. Re-inlining the last month of progress photos would be a
// silent context-window tax.
func (s *Service) hydrateCurrentTurn(ctx context.Context, user users.User, stored []conversations.Message, messages []ai.Message) []ai.Message {
	if s.attachments == nil || len(stored) == 0 || len(messages) == 0 {
		return messages
	}

	last := stored[len(stored)-1]
	if !last.IsUser() || len(last.Parts) == 0 {
		return messages
	}

	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != ai.RoleUser || len(messages[i].ToolResults) > 0 {
			continue
		}
		for _, part := range last.Parts {
			if part.Kind != "image" {
				continue
			}
			mime, data, err := s.attachments.LoadInline(ctx, user.ID, part.MediaID)
			if err != nil || len(data) == 0 {
				continue
			}
			messages[i].Parts = append(messages[i].Parts, ai.Part{
				InlineData: data,
				MIMEType:   mime,
			})
		}
		break
	}
	return messages
}

// startChat asks each provider in the user's chain until one of them begins a
// reply, and returns the client that did so the answer can be attributed.
//
// Failover happens here, before the first byte, and nowhere else. Once a stream
// has produced text that text has already reached the browser, and a second
// provider would contradict what the user is reading mid-sentence — so a
// mid-stream failure is recorded and the reply ends there. See pump.
func (s *Service) startChat(
	ctx context.Context,
	genCtx context.Context,
	user users.User,
	req ai.Request,
) (<-chan ai.StreamChunk, ai.Client, error) {
	var stream <-chan ai.StreamChunk

	client, err := s.eachProvider(ctx, user, func(c ai.Client) error {
		opened, err := c.Chat(genCtx, req)
		stream = opened
		return err
	})
	if err != nil {
		return nil, nil, err
	}

	return stream, client, nil
}

// generate is startChat's one-shot counterpart, for the side work that does not
// stream. It also returns the client that answered, so a caller reporting the
// call to analytics can attribute it to the real provider.
func (s *Service) generate(ctx context.Context, user users.User, req ai.Request) (*ai.Response, ai.Client, error) {
	var resp *ai.Response

	client, err := s.eachProvider(ctx, user, func(c ai.Client) error {
		r, err := c.Generate(ctx, req)
		resp = r
		return err
	})
	if err != nil {
		return nil, nil, err
	}

	return resp, client, nil
}

// eachProvider tries the user's chain in order until attempt succeeds, and
// returns the client that managed it.
func (s *Service) eachProvider(ctx context.Context, user users.User, attempt func(ai.Client) error) (ai.Client, error) {
	log := middleware.FromContext(ctx)

	clients := s.registry.Resolve(s.chains.For(string(user.Tier)))

	// A user's own key goes in front of North's chain rather than replacing
	// it, which is docs/byok-plan.md's "the user's own credential, else the
	// platform provider" expressed with the failover machinery that already
	// exists: a key that is out of credit or rejected produces
	// ErrPaymentRequired or ErrForbidden, ai.Failover says so, and the loop
	// below walks on by itself. No second fallback concept, and nobody loses
	// their coach to a mistyped key.
	//
	// The registry is untouched. It is built once at startup and read without
	// locking, and registering a per-user client into it would be exactly the
	// bug that comment warns about.
	// True when clients[0] is the user's own provider rather than North's.
	ownIsFirst := false
	if s.own != nil {
		own, err := s.own.For(ctx, user.ID)
		switch {
		case err != nil:
			log.Warn("the user's own provider could not be built; serving from North's chain",
				slog.String("user_id", user.ID.String()), slog.Any("error", err))
		case own != nil:
			ownIsFirst = true
			clients = append([]ai.Client{own}, clients...)
		}
	}

	if len(clients) == 0 {
		return nil, apperr.Wrap(apperr.ErrUnavailable, "coach: no AI provider is configured")
	}

	var lastErr error
	for i, client := range clients {
		err := attempt(client)
		if err == nil {
			// The user's own key worked, so any complaint recorded against it
			// is stale. Clearing it is the settings page's job on the next
			// save; nothing to do here but stop blaming it.
			return client, nil
		}
		lastErr = err

		// The user's key is the only one they can do anything about, so its
		// refusal is recorded where they will see it. Everything else is
		// North's problem and stays in the log.
		if i == 0 && ownIsFirst {
			s.own.NoteFailure(ctx, user.ID, ownFailureReason(err))
		}

		// A request the provider refused on its own account is worth asking
		// someone else. A malformed one is not: it would fail identically
		// everywhere, and walking the chain only delays the same error.
		if !ai.Failover(err) || i == len(clients)-1 {
			break
		}

		log.Warn("ai provider refused; falling back to the next in the chain",
			slog.String("provider", client.Name()),
			slog.String("next", clients[i+1].Name()),
			slog.Any("error", err),
		)
	}

	return nil, lastErr
}

// ownFailureReason summarises why a user's provider refused, in words meant
// for the person who owns the key.
//
// A summary rather than the provider's own error: a response body can quote
// the credential back, and this string is rendered in a template and stored in
// a column that outlives the request.
func ownFailureReason(err error) string {
	switch {
	case apperr.Is(err, apperr.ErrForbidden):
		return "Your provider rejected the key. It may have been revoked or copied incompletely."
	case apperr.Is(err, apperr.ErrPaymentRequired):
		return "Your provider refused on billing grounds — the account is out of credit."
	case apperr.Is(err, apperr.ErrUnavailable):
		return "Your provider was unreachable or overloaded."
	default:
		return "Your provider could not answer this request."
	}
}

// toolDeclarations is what the model is shown it can call, or nil when the
// coach has no registry wired.
func (s *Service) toolDeclarations() []ai.Tool {
	if s.tools == nil {
		return nil
	}
	return s.tools.Tools()
}

type pumpTarget struct {
	conversation conversations.Conversation
	user         users.User
	provider     string
	firstMessage string
	source       string

	// client and request are kept so the pump can go back to the model after
	// running tools, carrying the same system prompt and history plus what the
	// tools returned.
	client  ai.Client
	request ai.Request

	// offeredRefs is every evidence ref the context block showed the model this
	// turn. Citations outside it are discarded rather than recorded: a model
	// asked to cite will occasionally produce a well-formed ref for a fact it
	// was never given, and storing that would make the audit trail wrong in the
	// one direction that matters.
	offeredRefs map[string]bool

	// traceID groups every generation and tool span this turn produces, so
	// they land in PostHog as one trace rather than one per model call.
	traceID string
}

// pump forwards the provider's stream to the caller while accumulating the full
// reply, then saves it.
//
// callerCtx governs delivery; genCtx governs generation. When the caller goes
// away the loop keeps draining so the reply still completes and is persisted —
// it simply stops trying to send.
func (s *Service) pump(
	callerCtx context.Context,
	genCtx context.Context,
	cancelGen context.CancelFunc,
	stream <-chan ai.StreamChunk,
	out chan<- ai.StreamChunk,
	target pumpTarget,
) {
	defer cancelGen()
	defer close(out)

	log := middleware.FromContext(callerCtx)

	var (
		reply     strings.Builder
		usage     *ai.Usage
		streamErr error
		listening = true

		// The reply is accumulated with its citations intact and stripped only
		// at the end, because that is what says which facts were used. What
		// reaches the browser is filtered as it goes: the user should never see
		// the handles, and waiting for the whole reply to strip them would give
		// up streaming entirely.
		visible refStripper

		// Catalogue exercises the model looked up this turn, across every
		// round. Accumulated here rather than read back off the request
		// afterwards because the tool messages are rewritten each round.
		exerciseSlugs []string
	)

	// One pass per round-trip to the model. A pass that ends in tool calls
	// runs them, appends what they returned, and asks again; a pass that ends
	// in prose is the answer.
	request := target.request

	for round := 0; ; round++ {
		var calls []ai.ToolCall
		var roundUsage *ai.Usage
		roundStart := time.Now()

		for chunk := range stream {
			if chunk.Err != nil {
				streamErr = chunk.Err
				if listening {
					trySend(callerCtx, out, chunk)
				}
				break
			}

			if chunk.Usage != nil {
				usage = chunk.Usage
				roundUsage = chunk.Usage
			}
			if len(chunk.ToolCalls) > 0 {
				// Not forwarded to the caller: the browser renders prose, and
				// a tool call is machinery the person did not ask to watch.
				calls = append(calls, chunk.ToolCalls...)
				continue
			}
			if chunk.Text != "" {
				reply.WriteString(chunk.Text)

				// Usage was already taken above, so holding this chunk back
				// loses nothing but the text it did not yet have.
				if chunk.Text = visible.Take(chunk.Text); chunk.Text == "" {
					continue
				}
			}

			if listening && !trySend(callerCtx, out, chunk) {
				// The browser went away. Keep draining: the answer is still
				// worth finishing and saving.
				listening = false
				log.Info("client disconnected mid-reply; continuing to save it",
					slog.String("conversation_id", target.conversation.ID.String()))
			}
		}

		roundUsageValue := usageOrZero(roundUsage)
		roundLatency := time.Since(roundStart)

		s.analytics.captureGeneration(callerCtx, generation{
			sessionID:  target.conversation.ID.String(),
			traceID:    target.traceID,
			distinctID: target.user.ID.String(),
			provider:   target.provider,
			model:      request.Model,
			usage:      roundUsageValue,
			latency:    roundLatency,
			err:        streamErr,
		})

		if streamErr != nil || len(calls) == 0 {
			break
		}

		if round+1 >= toolRounds {
			// The model is still asking. Stopping here rather than continuing
			// is the whole point of the bound; the partial answer is kept.
			log.Warn("coach hit the tool-call limit",
				slog.Int("rounds", toolRounds),
				slog.String("conversation_id", target.conversation.ID.String()))
			break
		}

		exerciseSlugs = appendExerciseLookups(exerciseSlugs, calls)

		// A turn that writes stops here and waits for a person.
		//
		// The calls are stored and the stream ends; approving is a fresh
		// request that rebuilds the conversation and carries on. The loop
		// cannot simply block: it is running inside an SSE response, and
		// holding one open until somebody clicks would tie up a connection and
		// lose everything if the process restarted.
		//
		// The whole turn suspends even when only one of its calls writes.
		// Running the read-only half first would mean a declined turn had
		// already done something, which is not what "declined" reads as.
		if writes := writingCalls(s.tools, calls); len(writes) > 0 {
			if _, err := s.conversations.AppendToolCalls(genCtx, target.conversation.ID, calls); err != nil {
				log.Error("could not store a tool call awaiting approval",
					slog.String("conversation_id", target.conversation.ID.String()),
					slog.Any("error", err))
				break
			}

			log.Info("coach is waiting for approval before writing",
				slog.String("conversation_id", target.conversation.ID.String()),
				slog.Any("tools", toolNames(writes)))

			// Nothing is sent to the caller here. The handler asks
			// PendingApproval once the stream closes and renders the card from
			// that, which keeps the provider-facing ai.StreamChunk free of a
			// field only North's own UI would ever read.
			break
		}

		results := s.tools.InvokeAll(toolsurface.With(genCtx, toolsurface.Coach), target.user.ID, calls)
		for _, result := range results {
			log.Info("coach ran a tool",
				slog.String("tool", result.Name),
				slog.Bool("failed", result.IsError),
				slog.String("conversation_id", target.conversation.ID.String()))

			s.analytics.captureToolSpan(callerCtx,
				target.conversation.ID.String(), target.traceID, target.user.ID.String(), result)
		}

		// Both turns, in this order: every provider rejects a result whose
		// call it has not been shown.
		request.Messages = append(request.Messages,
			ai.ToolCallMessage(calls),
			ai.ToolResultMessage(results),
		)

		// And to the database, in the same order, so a turn that has to pause
		// for approval can be rebuilt after its stream has ended. A failure
		// here is logged rather than fatal: the reply in flight is still worth
		// finishing, and the cost of losing these rows is a turn that cannot be
		// resumed — not a turn that is wrong.
		if _, err := s.conversations.AppendToolCalls(genCtx, target.conversation.ID, calls); err != nil {
			log.Error("could not store the coach's tool calls",
				slog.String("conversation_id", target.conversation.ID.String()),
				slog.Any("error", err))
		} else if _, err := s.conversations.AppendToolResults(genCtx, target.conversation.ID, results); err != nil {
			// Only when the calls landed: a result whose call was never stored
			// would rebuild into exactly the shape providers reject.
			log.Error("could not store the coach's tool results",
				slog.String("conversation_id", target.conversation.ID.String()),
				slog.Any("error", err))
		}

		next, err := target.client.Chat(genCtx, request)
		if err != nil {
			streamErr = apperr.Wrap(err, "coach: continue after tools")
			if listening {
				trySend(callerCtx, out, ai.StreamChunk{Err: streamErr})
			}
			break
		}
		stream = next
	}

	// Anything the filter was still holding when the stream ended was never a
	// citation after all, so it belongs to the reader.
	if tail := visible.Flush(); tail != "" && listening {
		trySend(callerCtx, out, ai.StreamChunk{Text: tail})
	}

	text, evidenceRefs := StripRefs(strings.TrimSpace(reply.String()), target.offeredRefs)

	// Recorded alongside the citations because it is the same kind of claim:
	// what this reply was built from. The chat draws the muscles worked from
	// these rather than asking the model to describe them in a shape a template
	// could parse — the catalogue already knows, and the model already looked.
	for _, slug := range exerciseSlugs {
		evidenceRefs = append(evidenceRefs, ExerciseRef(slug))
	}

	if streamErr != nil {
		log.Error("coach reply failed", slog.Any("error", streamErr),
			slog.String("conversation_id", target.conversation.ID.String()))

		// A partial answer is still worth keeping: the user saw it, and losing
		// it would make the conversation history disagree with what happened.
		if text == "" {
			return
		}
	}
	if text == "" {
		return
	}

	// Detached and fresh: by now the request context is very likely cancelled,
	// and using it would drop the very write this whole design exists to make.
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(genCtx), persistTimeout)
	defer cancel()

	if _, err := s.conversations.AppendModelMessage(
		saveCtx, target.conversation.ID, text, usage, s.model, target.provider, evidenceRefs,
	); err != nil {
		log.Error("could not save the coach's reply", slog.Any("error", err),
			slog.String("conversation_id", target.conversation.ID.String()))
		return
	}

	s.noteTelegramReply(saveCtx, target, text)

	if s.conversations.NeedsTitle(saveCtx, target.conversation) {
		s.titleConversation(saveCtx, target.user, target.conversation.ID, target.firstMessage)
	}

	// Threshold trigger: the turn that pushes a thread past the context window
	// is the one that makes its beginning invisible, so compact it now rather
	// than waiting for the sweep an hour later.
	s.enqueueSummary(saveCtx, target.user.ID, target.conversation.ID)

	ended := s.maybeEndReflection(saveCtx, target.conversation, text)
	s.enqueueMemoryExtraction(saveCtx, target, ended)
}

const reflectionSummaryHeading = "## Reflection summary"

func (s *Service) systemPrompt(cc *Context, conversation conversations.Conversation) (string, error) {
	if conversation.IsReflection() {
		return s.promptB.Reflection(cc, countModelMessages(cc.RecentMessages))
	}
	return s.promptB.Coach(cc)
}

func (s *Service) toolsFor(conversation conversations.Conversation) []ai.Tool {
	if conversation.IsReflection() {
		return nil
	}
	return s.toolDeclarations()
}

func countModelMessages(messages []conversations.Message) int {
	n := 0
	for _, m := range messages {
		if m.IsModel() {
			n++
		}
	}
	return n
}

func extractReflectionSummary(text string) (string, bool) {
	idx := strings.Index(text, reflectionSummaryHeading)
	if idx < 0 {
		return "", false
	}
	return strings.TrimSpace(text[idx+len(reflectionSummaryHeading):]), true
}

func (s *Service) maybeEndReflection(ctx context.Context, conversation conversations.Conversation, reply string) bool {
	if !conversation.IsReflection() || conversation.Ended() {
		return conversation.Ended()
	}
	summary, ok := extractReflectionSummary(reply)
	if !ok {
		return false
	}
	if err := s.conversations.SetSummary(ctx, conversation.ID, summary); err != nil {
		middleware.FromContext(ctx).Warn("could not store reflection summary", slog.Any("error", err))
		return false
	}
	return true
}

// BeginReflection asks the first question of an empty reflection thread.
func (s *Service) BeginReflection(ctx context.Context, user users.User, conversationID uuid.UUID) (<-chan ai.StreamChunk, error) {
	conversation, err := s.conversations.Get(ctx, conversationID, user.ID)
	if err != nil {
		return nil, err
	}
	if !conversation.IsReflection() {
		return nil, apperr.Wrap(apperr.ErrValidation, "not a reflection")
	}
	if conversation.Ended() {
		return nil, apperr.Wrap(apperr.ErrConflict, "this reflection has ended")
	}

	history, err := s.conversations.History(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	if len(history) > 0 {
		return nil, apperr.Wrap(apperr.ErrConflict, "reflection already started")
	}

	coachCtx, err := s.contextB.Build(ctx, ContextRequest{
		User:           user,
		ConversationID: conversationID,
	})
	if err != nil {
		return nil, err
	}

	system, err := s.promptB.Reflection(coachCtx, 0)
	if err != nil {
		return nil, err
	}

	genCtx, cancelGen := context.WithTimeout(context.WithoutCancel(ctx), generationTimeout)

	req := ai.Request{
		Model:  s.model,
		System: system,
		// Sent to the model only. Nothing is stored as a user turn: the
		// person has not spoken yet.
		Messages: []ai.Message{ai.UserText("Begin the reflection.")},
	}

	stream, client, err := s.startChat(ctx, genCtx, user, req)
	if err != nil {
		cancelGen()
		return nil, apperr.Wrap(err, "coach: start reflection")
	}

	out := make(chan ai.StreamChunk)
	go s.pump(ctx, genCtx, cancelGen, stream, out, pumpTarget{
		conversation: conversation,
		user:         user,
		provider:     client.Name(),
		firstMessage: "",
		client:       client,
		request:      req,
		offeredRefs:  coachCtx.OfferedRefs(),
		traceID:      uuid.New().String(),
	})
	return out, nil
}

func (s *Service) StartReflection(ctx context.Context, userID uuid.UUID) (conversations.Conversation, error) {
	return s.conversations.StartKind(ctx, userID, conversations.KindReflection)
}

// enqueueMemoryExtraction proposes durable facts off the chat hot path.
//
// Failures are logged only: a missed extraction is preferable to a failed reply.
func (s *Service) enqueueMemoryExtraction(ctx context.Context, target pumpTarget, force bool) {
	if s.queue == nil {
		return
	}
	log := middleware.FromContext(ctx)

	// Wait until the thread has real back-and-forth before filing notes,
	// unless a reflection just closed — that is the moment to extract.
	if !force {
		recent, err := s.conversations.Recent(ctx, target.conversation.ID, 6)
		if err != nil || len(recent) < 4 {
			return
		}
	}

	if _, err := s.queue.Enqueue(ctx, jobs.KindExtractMemories, jobs.ExtractMemoriesPayload{
		UserID:         target.user.ID,
		ConversationID: target.conversation.ID,
	}); err != nil {
		log.Warn("could not enqueue memory extraction", slog.Any("error", err),
			slog.String("conversation_id", target.conversation.ID.String()))
	}
}

// enqueueSummary asks for a thread to be compacted, once it has more history
// than the context window carries.
//
// Queued rather than run inline: unlike titling, this reads a large slice of
// the thread and writes a long prompt, and the reply pump has already made the
// person wait once.
func (s *Service) enqueueSummary(ctx context.Context, userID, conversationID uuid.UUID) {
	if s.queue == nil {
		return
	}
	log := middleware.FromContext(ctx)

	// One more than the tail: at exactly the tail size nothing has fallen out
	// yet, so there is nothing to summarise.
	count, err := s.conversations.CountMessages(ctx, conversationID)
	if err != nil || count <= s.contextB.RecentTurns() {
		return
	}

	if _, err := s.queue.Enqueue(ctx, jobs.KindSummarizeConversation, jobs.SummarizeConversationPayload{
		UserID:         userID,
		ConversationID: conversationID,
	}); err != nil {
		log.Warn("could not enqueue conversation summary", slog.Any("error", err),
			slog.String("conversation_id", conversationID.String()))
	}
}

// titleConversation names a thread from its opening message.
//
// Failure is logged and ignored: an untitled conversation shows a placeholder,
// which is a far better outcome than failing the message that created it.
func (s *Service) titleConversation(ctx context.Context, user users.User, conversationID uuid.UUID, firstMessage string) {
	log := middleware.FromContext(ctx)

	prompt, err := s.promptB.Titler(firstMessage)
	if err != nil {
		log.Warn("could not build the title prompt", slog.Any("error", err))
		return
	}

	// The same chain as the reply, so a title is not the one thing that breaks
	// when the head provider runs dry. The fast model: naming a thread is not
	// worth the expensive one.
	started := time.Now()
	resp, client, err := s.generate(ctx, user, ai.Request{
		Model:     s.fastModel,
		Messages:  []ai.Message{ai.UserText(prompt)},
		MaxTokens: 40,
	})

	// A trace of its own: naming a thread is not part of the turn that
	// triggered it, but it shares the conversation's session id.
	var respUsage ai.Usage
	provider := ""
	if client != nil {
		provider = client.Name()
	}
	if resp != nil {
		respUsage = resp.Usage
	}
	s.analytics.captureGeneration(ctx, generation{
		sessionID:  conversationID.String(),
		traceID:    uuid.New().String(),
		distinctID: user.ID.String(),
		provider:   provider,
		model:      s.fastModel,
		usage:      respUsage,
		latency:    time.Since(started),
		err:        err,
	})

	if err != nil {
		log.Warn("could not generate a conversation title", slog.Any("error", err))
		return
	}

	if err := s.conversations.SetTitle(ctx, conversationID, resp.Text); err != nil {
		log.Warn("could not save the conversation title", slog.Any("error", err))
	}
}

// StartConversation opens a new thread.
func (s *Service) StartConversation(ctx context.Context, userID uuid.UUID) (conversations.Conversation, error) {
	return s.conversations.Start(ctx, userID)
}

// Conversations exposes the conversation service for handlers that only need
// to read history.
func (s *Service) Conversations() *conversations.Service { return s.conversations }

// trySend delivers a chunk unless the caller has gone, reporting whether it
// arrived.
func trySend(ctx context.Context, out chan<- ai.StreamChunk, chunk ai.StreamChunk) bool {
	select {
	case out <- chunk:
		return true
	case <-ctx.Done():
		return false
	}
}
