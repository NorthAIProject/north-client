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

// generationTimeout bounds a detached generation. Long, because a large model
// answering a considered question is slow; bounded, because a stuck provider
// must not leak a goroutine forever.
const generationTimeout = 5 * time.Minute

// persistTimeout bounds the write that saves a completed reply.
const persistTimeout = 15 * time.Second

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

	// chains decides which providers serve a given user, in what order.
	chains ai.ChainSet

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
	coachCtx, err := s.contextB.Build(ctx, ContextRequest{User: user, ConversationID: conversationID})
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

	stream, client, err := s.startChat(ctx, genCtx, user, ai.Request{
		Model:    s.model,
		System:   system,
		Messages: conversations.ToAIMessages(coachCtx.RecentMessages),
	})
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
	if len(clients) == 0 {
		return nil, apperr.Wrap(apperr.ErrUnavailable, "coach: no AI provider is configured")
	}

	var lastErr error
	for i, client := range clients {
		err := attempt(client)
		if err == nil {
			return client, nil
		}
		lastErr = err

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

type pumpTarget struct {
	conversation conversations.Conversation
	user         users.User
	provider     string
	firstMessage string
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
	)

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
		if chunk.Text != "" {
			reply.WriteString(chunk.Text)
		}

		if listening && !trySend(callerCtx, out, chunk) {
			// The browser went away. Keep draining: the answer is still worth
			// finishing and saving.
			listening = false
			log.Info("client disconnected mid-reply; continuing to save it",
				slog.String("conversation_id", target.conversation.ID.String()))
		}
	}

	text := strings.TrimSpace(reply.String())

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
		saveCtx, target.conversation.ID, text, usage, s.model, target.provider,
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
