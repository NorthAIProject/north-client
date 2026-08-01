package conversations

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// maxMessageLength bounds a single user message. Generous enough to paste a
// training log, small enough that one message cannot fill the context window.
const maxMessageLength = 20_000

// defaultHistoryLimit is how many turns are loaded for display.
const defaultHistoryLimit = 200

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) Start(ctx context.Context, userID uuid.UUID) (Conversation, error) {
	return s.repo.Create(ctx, userID, "")
}

// Get returns a conversation the user owns, or ErrNotFound.
func (s *Service) Get(ctx context.Context, id, userID uuid.UUID) (Conversation, error) {
	return s.repo.Get(ctx, id, userID)
}

func (s *Service) List(ctx context.Context, userID uuid.UUID, limit int) ([]Conversation, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	return s.repo.List(ctx, userID, limit, 0)
}

func (s *Service) Delete(ctx context.Context, id, userID uuid.UUID) error {
	return s.repo.Delete(ctx, id, userID)
}

// History returns a conversation's messages in reading order.
func (s *Service) History(ctx context.Context, conversationID uuid.UUID) ([]Message, error) {
	return s.repo.Messages(ctx, conversationID, defaultHistoryLimit)
}

// Recent returns the tail of a conversation, for context building.
func (s *Service) Recent(ctx context.Context, conversationID uuid.UUID, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = 20
	}
	return s.repo.Recent(ctx, conversationID, limit)
}

func (s *Service) RecentUserMessages(ctx context.Context, userID uuid.UUID, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = 10
	}
	return s.repo.RecentUserMessages(ctx, userID, limit)
}

// ValidateMessage checks a user message before anything is stored or sent.
func ValidateMessage(text string) error {
	trimmed := strings.TrimSpace(text)

	var errs apperr.FieldErrors
	switch {
	case trimmed == "":
		errs = errs.Add("message", "Type something first.")
	case len(trimmed) > maxMessageLength:
		errs = errs.Add("message", "That message is too long. Try breaking it up.")
	}
	return errs.OrNil()
}

// AppendUserMessage stores what the user said.
func (s *Service) AppendUserMessage(ctx context.Context, conversationID uuid.UUID, text string, attachments []Attachment) (Message, error) {
	if err := ValidateMessage(text); err != nil {
		return Message{}, err
	}

	return s.repo.Append(ctx, NewMessage{
		ConversationID: conversationID,
		Role:           ai.RoleUser,
		Content:        strings.TrimSpace(text),
		Parts:          attachments,
	})
}

// AppendModelMessage stores the coach's reply along with what it cost.
func (s *Service) AppendModelMessage(ctx context.Context, conversationID uuid.UUID, text string, usage *ai.Usage, model, provider string) (Message, error) {
	return s.repo.Append(ctx, NewMessage{
		ConversationID: conversationID,
		Role:           ai.RoleModel,
		Content:        text,
		Usage:          usage,
		Model:          model,
		Provider:       provider,
	})
}

// SetTitle names a conversation.
func (s *Service) SetTitle(ctx context.Context, id uuid.UUID, title string) error {
	title = strings.TrimSpace(title)
	title = strings.Trim(title, `"'`)

	// A model asked for a short title occasionally returns a sentence. Truncate
	// rather than reject: a long title is a cosmetic problem, and failing the
	// message because of one would not be.
	if len(title) > 80 {
		title = strings.TrimSpace(title[:80])
	}
	if title == "" {
		return nil
	}

	return s.repo.SetTitle(ctx, id, title)
}

// NeedsTitle reports whether a conversation is still unnamed.
func (s *Service) NeedsTitle(ctx context.Context, c Conversation) bool {
	return strings.TrimSpace(c.Title) == ""
}
