package coach

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/conversations"
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

	model     string
	fastModel string
}

type Options struct {
	Registry       *ai.Registry
	Conversations  *conversations.Service
	ContextBuilder *ContextBuilder
	PromptBuilder  *PromptBuilder

	// Model answers conversations. FastModel handles cheap side work such as
	// naming a thread, which does not need the expensive model.
	Model     string
	FastModel string
}

func NewService(opts Options) *Service {
	return &Service{
		registry:      opts.Registry,
		conversations: opts.Conversations,
		contextB:      opts.ContextBuilder,
		promptB:       opts.PromptBuilder,
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

	if _, err := s.conversations.AppendUserMessage(ctx, conversationID, text, nil); err != nil {
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

	client, err := s.registry.Default()
	if err != nil {
		return nil, err
	}

	// Detached from the request so a disconnect does not abort generation. The
	// logger is preserved, so these lines stay attached to the request that
	// started them.
	genCtx, cancelGen := context.WithTimeout(context.WithoutCancel(ctx), generationTimeout)

	stream, err := client.Chat(genCtx, ai.Request{
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
		s.titleConversation(saveCtx, target.conversation.ID, target.firstMessage)
	}
}

// titleConversation names a thread from its opening message.
//
// Failure is logged and ignored: an untitled conversation shows a placeholder,
// which is a far better outcome than failing the message that created it.
func (s *Service) titleConversation(ctx context.Context, conversationID uuid.UUID, firstMessage string) {
	log := middleware.FromContext(ctx)

	prompt, err := s.promptB.Titler(firstMessage)
	if err != nil {
		log.Warn("could not build the title prompt", slog.Any("error", err))
		return
	}

	client, err := s.registry.Default()
	if err != nil {
		return
	}

	// The fast model: naming a thread is not worth the expensive one.
	resp, err := client.Generate(ctx, ai.Request{
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
