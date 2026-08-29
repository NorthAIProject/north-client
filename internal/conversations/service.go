package conversations

import (
	"context"
	"strings"
	"time"

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
	return s.StartKind(ctx, userID, KindChat)
}

// StartKind opens a thread of the given kind. Empty kind is chat.
func (s *Service) StartKind(ctx context.Context, userID uuid.UUID, kind string) (Conversation, error) {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		kind = KindChat
	}
	if kind != KindChat && kind != KindReflection {
		return Conversation{}, apperr.Wrap(apperr.ErrValidation, "unknown conversation kind")
	}
	return s.repo.Create(ctx, userID, "", kind)
}

func (s *Service) SetSummary(ctx context.Context, id uuid.UUID, summary string) error {
	return s.repo.SetSummary(ctx, id, strings.TrimSpace(summary))
}

// SetContextSummary stores the rolling compaction of a long thread.
//
// An empty summary is refused rather than written: it would advance the
// watermark past turns nothing describes, which is how history disappears.
func (s *Service) SetContextSummary(ctx context.Context, id uuid.UUID, summary string, through time.Time) error {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return apperr.Wrap(apperr.ErrValidation, "refusing to store an empty context summary")
	}
	if through.IsZero() {
		return apperr.Wrap(apperr.ErrValidation, "context summary needs a watermark")
	}
	return s.repo.SetContextSummary(ctx, id, summary, through)
}

// ToSummarize returns the turns a summarising pass should fold in: everything
// after the current watermark, up to and including through.
func (s *Service) ToSummarize(ctx context.Context, id uuid.UUID, after, through time.Time, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = defaultHistoryLimit
	}
	return s.repo.MessagesBetween(ctx, id, after, through, limit)
}

// AwaitingSummary lists threads whose history has outgrown the context window.
func (s *Service) AwaitingSummary(ctx context.Context, keepRecent, limit int) ([]Pending, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	return s.repo.AwaitingSummary(ctx, keepRecent, limit)
}

// Owner returns who a thread belongs to. For background work that has an id
// and no user to scope by.
func (s *Service) Owner(ctx context.Context, id uuid.UUID) (uuid.UUID, error) {
	return s.repo.Owner(ctx, id)
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

// UserMessageCount is how many turns the person has written, across threads.
func (s *Service) UserMessageCount(ctx context.Context, userID uuid.UUID) (int, error) {
	// A first-week account has a handful of turns. Loading a short tail and
	// counting it is enough to tell "seed only" from "they came back".
	msgs, err := s.repo.RecentUserMessages(ctx, userID, 20)
	if err != nil {
		return 0, err
	}
	return len(msgs), nil
}

// ValidateMessage checks a user message before anything is stored or sent.
func ValidateMessage(text string) error {
	return ValidateTurn(text, false)
}

// ValidateTurn is ValidateMessage for a turn that may carry a file instead of
// words. A photo with no caption is a complete question; an empty box is not.
func ValidateTurn(text string, hasAttachment bool) error {
	trimmed := strings.TrimSpace(text)

	var errs apperr.FieldErrors
	switch {
	case trimmed == "" && !hasAttachment:
		errs = errs.Add("message", "Type something first.")
	case len(trimmed) > maxMessageLength:
		errs = errs.Add("message", "That message is too long. Try breaking it up.")
	}
	return errs.OrNil()
}

// AppendUserMessage stores what the user said.
func (s *Service) AppendUserMessage(ctx context.Context, conversationID uuid.UUID, text string, attachments []Attachment) (Message, error) {
	if err := ValidateTurn(text, len(attachments) > 0); err != nil {
		return Message{}, err
	}

	return s.repo.Append(ctx, NewMessage{
		ConversationID: conversationID,
		Role:           ai.RoleUser,
		Content:        strings.TrimSpace(text),
		Parts:          attachments,
	})
}

// AppendModelMessage stores the coach's reply along with what it cost and what
// it was built from.
func (s *Service) AppendModelMessage(ctx context.Context, conversationID uuid.UUID, text string, usage *ai.Usage, model, provider string, evidenceRefs []string) (Message, error) {
	return s.repo.Append(ctx, NewMessage{
		ConversationID: conversationID,
		Role:           ai.RoleModel,
		Content:        text,
		Usage:          usage,
		Model:          model,
		Provider:       provider,
		EvidenceRefs:   evidenceRefs,
	})
}

// AppendToolCalls stores the tools a model asked for on this turn.
//
// A model turn, because that is whose turn it is: ai.ToolCallMessage builds the
// same shape for the request. Every provider rejects a result whose call it has
// not been shown, so this is what makes a resumed conversation replayable.
func (s *Service) AppendToolCalls(ctx context.Context, conversationID uuid.UUID, calls []ai.ToolCall) (Message, error) {
	return s.repo.Append(ctx, NewMessage{
		ConversationID: conversationID,
		Role:           ai.RoleModel,
		ToolCalls:      calls,
	})
}

// AppendToolResults stores what the tools answered.
//
// A user turn, matching ai.ToolResultMessage: from the model's point of view
// the results arrive from outside, the same way a person's message does.
func (s *Service) AppendToolResults(ctx context.Context, conversationID uuid.UUID, results []ai.ToolResult) (Message, error) {
	return s.repo.Append(ctx, NewMessage{
		ConversationID: conversationID,
		Role:           ai.RoleUser,
		ToolResults:    results,
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

func (s *Service) CountMessages(ctx context.Context, conversationID uuid.UUID) (int, error) {
	return s.repo.CountMessages(ctx, conversationID)
}

// AwaitingExtraction lists threads that went quiet before the memory extractor
// read them.
//
// Not user-scoped, unlike everything else here, because its only caller is a
// background sweep over every account. Nothing reachable from a request should
// call it.
func (s *Service) AwaitingExtraction(ctx context.Context, idleBefore time.Time, minMessages, limit int) ([]Pending, error) {
	return s.repo.AwaitingExtraction(ctx, idleBefore, minMessages, limit)
}

// MarkExtracted records that extraction ran over a thread, found or not.
// SetMessageHelpful records whether a coach reply helped.
//
// A miss is a not-found rather than a distinct error. The three ways it can miss
// — no such message, somebody else's thread, or the person's own turn — are all
// answered the same way on purpose: telling them apart would let a caller probe
// which message ids exist in accounts they cannot see.
func (s *Service) SetMessageHelpful(ctx context.Context, messageID, userID uuid.UUID, helpful *bool) (Message, error) {
	msg, ok, err := s.repo.SetMessageHelpful(ctx, messageID, userID, helpful)
	if err != nil {
		return Message{}, err
	}
	if !ok {
		return Message{}, apperr.ErrNotFound
	}
	return msg, nil
}

func (s *Service) MarkExtracted(ctx context.Context, id uuid.UUID) error {
	return s.repo.MarkExtracted(ctx, id)
}

// NeedsTitle reports whether a conversation is still unnamed.
func (s *Service) NeedsTitle(ctx context.Context, c Conversation) bool {
	return strings.TrimSpace(c.Title) == ""
}
