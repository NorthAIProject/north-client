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

	// chains decides which providers serve a given user, in what order.
	chains ai.ChainSet

	// own yields a user's bring-your-own provider. Nil disables the path
	// entirely, which is what every process without a sealer gets.
	own ClientSource

	model     string
	fastModel string
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

	// Own yields a user's own provider, tried ahead of Chains. Nil leaves
	// every user on North's providers, which is what a deployment with no
	// encryption key gets.
	Own ClientSource

	// Model answers conversations. FastModel handles cheap side work such as
	// naming a thread, which does not need the expensive model. Both may be
	// empty, which lets whichever provider answers use its own default — the
	// only sane behaviour when the chain spans several vendors.
	Model     string
	FastModel string
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
		own:           opts.Own,
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
	if err := conversations.ValidateMessage(text); err != nil {
		return nil, err
	}

	conversation, err := s.conversations.Get(ctx, conversationID, user.ID)
	if err != nil {
		return nil, err
	}

	if _, err = s.conversations.AppendUserMessage(ctx, conversationID, text, nil); err != nil {
		return nil, err
	}

	// Built after the user's message is stored, so the model sees the turn it
	// is answering as part of the history rather than as a special case.
	coachCtx, err := s.contextB.Build(ctx, ContextRequest{
		User:           user,
		ConversationID: conversationID,
		Query:          text,
	})
	if err != nil {
		return nil, err
	}

	system, err := s.promptB.Coach(coachCtx)
	if err != nil {
		return nil, err
	}

	// Detached from the request so a disconnect does not abort generation. The
	// logger is preserved, so these lines stay attached to the request that
	// started them.
	genCtx, cancelGen := context.WithTimeout(context.WithoutCancel(ctx), generationTimeout)

	req := ai.Request{
		Model:    s.model,
		System:   system,
		Messages: conversations.ToAIMessages(coachCtx.RecentMessages),
		Tools:    s.toolDeclarations(),
	}

	stream, client, err := s.startChat(ctx, genCtx, user, req)
	if err != nil {
		cancelGen()
		return nil, apperr.Wrap(err, "coach: start reply")
	}

	out := make(chan ai.StreamChunk)

	go s.pump(ctx, genCtx, cancelGen, stream, out, pumpTarget{
		conversation: conversation,
		user:         user,
		provider:     client.Name(),
		firstMessage: text,
		client:       client,
		request:      req,
		offeredRefs:  coachCtx.OfferedRefs(),
	})

	return out, nil
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
// stream.
func (s *Service) generate(ctx context.Context, user users.User, req ai.Request) (*ai.Response, error) {
	var resp *ai.Response

	_, err := s.eachProvider(ctx, user, func(c ai.Client) error {
		r, err := c.Generate(ctx, req)
		resp = r
		return err
	})
	if err != nil {
		return nil, err
	}

	return resp, nil
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
	)

	// One pass per round-trip to the model. A pass that ends in tool calls
	// runs them, appends what they returned, and asks again; a pass that ends
	// in prose is the answer.
	request := target.request

	for round := 0; ; round++ {
		var calls []ai.ToolCall

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

		results := s.tools.InvokeAll(genCtx, target.user.ID, calls)
		for _, result := range results {
			log.Info("coach ran a tool",
				slog.String("tool", result.Name),
				slog.Bool("failed", result.IsError),
				slog.String("conversation_id", target.conversation.ID.String()))
		}

		// Both turns, in this order: every provider rejects a result whose
		// call it has not been shown.
		request.Messages = append(request.Messages,
			ai.ToolCallMessage(calls),
			ai.ToolResultMessage(results),
		)

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

	if s.conversations.NeedsTitle(saveCtx, target.conversation) {
		s.titleConversation(saveCtx, target.user, target.conversation.ID, target.firstMessage)
	}

	s.enqueueMemoryExtraction(saveCtx, target)
}

// enqueueMemoryExtraction proposes durable facts off the chat hot path.
//
// Failures are logged only: a missed extraction is preferable to a failed reply.
func (s *Service) enqueueMemoryExtraction(ctx context.Context, target pumpTarget) {
	if s.queue == nil {
		return
	}
	log := middleware.FromContext(ctx)

	// Wait until the thread has real back-and-forth before filing notes.
	recent, err := s.conversations.Recent(ctx, target.conversation.ID, 6)
	if err != nil || len(recent) < 4 {
		return
	}

	if _, err := s.queue.Enqueue(ctx, jobs.KindExtractMemories, jobs.ExtractMemoriesPayload{
		UserID:         target.user.ID,
		ConversationID: target.conversation.ID,
	}); err != nil {
		log.Warn("could not enqueue memory extraction", slog.Any("error", err),
			slog.String("conversation_id", target.conversation.ID.String()))
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
	resp, err := s.generate(ctx, user, ai.Request{
		Model:     s.fastModel,
		Messages:  []ai.Message{ai.UserText(prompt)},
		MaxTokens: 40,
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
