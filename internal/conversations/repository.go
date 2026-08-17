package conversations

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/ai"
	conversationsdb "github.com/NorthAIProject/north-client/internal/conversations/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

type Repository struct {
	q *conversationsdb.Queries
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{q: conversationsdb.New(pool)}
}

func (r *Repository) Create(ctx context.Context, userID uuid.UUID, title, kind string) (Conversation, error) {
	row, err := r.q.CreateConversation(ctx, conversationsdb.CreateConversationParams{
		UserID: userID,
		Title:  nilIfEmpty(title),
		Kind:   kind,
	})
	if err != nil {
		return Conversation{}, apperr.Wrap(err, "create conversation")
	}
	return conversationFromDB(row), nil
}

// Get returns a conversation the user owns.
//
// Ownership is part of the query rather than a check the caller performs, so
// there is no path that returns another user's thread.
func (r *Repository) Get(ctx context.Context, id, userID uuid.UUID) (Conversation, error) {
	row, err := r.q.GetConversation(ctx, conversationsdb.GetConversationParams{ID: id, UserID: userID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Conversation{}, apperr.ErrNotFound
		}
		return Conversation{}, apperr.Wrap(err, "get conversation")
	}
	return conversationFromDB(row), nil
}

func (r *Repository) List(ctx context.Context, userID uuid.UUID, limit, offset int) ([]Conversation, error) {
	rows, err := r.q.ListConversations(ctx, conversationsdb.ListConversationsParams{
		UserID: userID,
		Limit:  int32(limit),
		Offset: int32(offset),
	})
	if err != nil {
		return nil, apperr.Wrap(err, "list conversations")
	}

	out := make([]Conversation, 0, len(rows))
	for _, row := range rows {
		out = append(out, conversationFromDB(row))
	}
	return out, nil
}

func (r *Repository) SetSummary(ctx context.Context, id uuid.UUID, summary string) error {
	return apperr.Wrap(r.q.SetConversationSummary(ctx, conversationsdb.SetConversationSummaryParams{
		ID:      id,
		Summary: summary,
	}), "set conversation summary")
}

// SetContextSummary stores the rolling compaction and how far it reaches.
func (r *Repository) SetContextSummary(ctx context.Context, id uuid.UUID, summary string, through time.Time) error {
	return apperr.Wrap(r.q.SetConversationContextSummary(ctx, conversationsdb.SetConversationContextSummaryParams{
		ID:                    id,
		ContextSummary:        summary,
		ContextSummaryThrough: &through,
	}), "set conversation context summary")
}

// MessagesBetween returns the turns written after a watermark and no later than
// through, oldest first. A zero after means from the beginning of the thread.
func (r *Repository) MessagesBetween(ctx context.Context, id uuid.UUID, after, through time.Time, limit int) ([]Message, error) {
	rows, err := r.q.MessagesBetween(ctx, conversationsdb.MessagesBetweenParams{
		ConversationID: id,
		After:          after,
		Through:        through,
		ResultLimit:    int32(limit),
	})
	if err != nil {
		return nil, apperr.Wrap(err, "list messages between")
	}
	return messagesFromDB(rows), nil
}

// AwaitingSummary lists threads with more than keepRecent turns beyond their
// summary watermark — the ones losing history off the end of the window.
func (r *Repository) AwaitingSummary(ctx context.Context, keepRecent, limit int) ([]Pending, error) {
	rows, err := r.q.ConversationsAwaitingSummary(ctx, conversationsdb.ConversationsAwaitingSummaryParams{
		KeepRecent:  int64(keepRecent),
		ResultLimit: int32(limit),
	})
	if err != nil {
		return nil, apperr.Wrap(err, "list conversations awaiting summary")
	}

	out := make([]Pending, len(rows))
	for i, row := range rows {
		out[i] = Pending{ID: row.ID, UserID: row.UserID}
	}
	return out, nil
}

// Owner returns the account a thread belongs to.
func (r *Repository) Owner(ctx context.Context, id uuid.UUID) (uuid.UUID, error) {
	owner, err := r.q.ConversationOwner(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, apperr.ErrNotFound
		}
		return uuid.Nil, apperr.Wrap(err, "conversation owner")
	}
	return owner, nil
}

func (r *Repository) SetTitle(ctx context.Context, id uuid.UUID, title string) error {
	err := r.q.SetConversationTitle(ctx, conversationsdb.SetConversationTitleParams{
		ID:    id,
		Title: nilIfEmpty(title),
	})
	return apperr.Wrap(err, "set conversation title")
}

func (r *Repository) Touch(ctx context.Context, id uuid.UUID) error {
	return apperr.Wrap(r.q.TouchConversation(ctx, id), "touch conversation")
}

func (r *Repository) Delete(ctx context.Context, id, userID uuid.UUID) error {
	err := r.q.DeleteConversation(ctx, conversationsdb.DeleteConversationParams{ID: id, UserID: userID})
	return apperr.Wrap(err, "delete conversation")
}

// NewMessage is a turn to append.
type NewMessage struct {
	ConversationID uuid.UUID
	Role           ai.Role
	Content        string
	Parts          []Attachment
	Usage          *ai.Usage
	Model          string
	Provider       string

	// EvidenceRefs are the stored facts this reply was built from. Empty for a
	// user message, and empty for a reply that cited nothing.
	EvidenceRefs []string

	// ToolCalls and ToolResults are set on tool turns; ordinary turns leave
	// both nil and the columns stay null.
	ToolCalls   []ai.ToolCall
	ToolResults []ai.ToolResult
}

func (r *Repository) Append(ctx context.Context, msg NewMessage) (Message, error) {
	parts, err := json.Marshal(orEmpty(msg.Parts))
	if err != nil {
		return Message{}, apperr.Wrap(err, "encode message parts")
	}

	var usage []byte
	if msg.Usage != nil {
		if usage, err = json.Marshal(msg.Usage); err != nil {
			return Message{}, apperr.Wrap(err, "encode token usage")
		}
	}

	// Left nil when there are none, so an ordinary message writes null rather
	// than an empty array — "nothing to do with tools", not "called none".
	var toolCalls, toolResults []byte
	if len(msg.ToolCalls) > 0 {
		if toolCalls, err = json.Marshal(msg.ToolCalls); err != nil {
			return Message{}, apperr.Wrap(err, "encode tool calls")
		}
	}
	if len(msg.ToolResults) > 0 {
		if toolResults, err = json.Marshal(msg.ToolResults); err != nil {
			return Message{}, apperr.Wrap(err, "encode tool results")
		}
	}

	row, err := r.q.AppendMessage(ctx, conversationsdb.AppendMessageParams{
		ConversationID: msg.ConversationID,
		Role:           string(msg.Role),
		Content:        msg.Content,
		Parts:          parts,
		ToolCalls:      toolCalls,
		ToolResults:    toolResults,
		Usage:          usage,
		Model:          nilIfEmpty(msg.Model),
		Provider:       nilIfEmpty(msg.Provider),
		EvidenceRefs:   orEmptyStrings(msg.EvidenceRefs),
	})
	if err != nil {
		return Message{}, apperr.Wrap(err, "append message")
	}

	// Keeps the conversation list ordered by activity rather than creation.
	if err := r.Touch(ctx, msg.ConversationID); err != nil {
		return Message{}, err
	}

	return messageFromDB(row), nil
}

// Messages returns a conversation's history in reading order.
func (r *Repository) Messages(ctx context.Context, conversationID uuid.UUID, limit int) ([]Message, error) {
	rows, err := r.q.ListMessages(ctx, conversationsdb.ListMessagesParams{
		ConversationID: conversationID,
		Limit:          int32(limit),
	})
	if err != nil {
		return nil, apperr.Wrap(err, "list messages")
	}
	return messagesFromDB(rows), nil
}

// Recent returns the most recent turns, oldest first.
//
// The query selects newest-first so the limit keeps the end of the conversation;
// the result is reversed here into reading order. Selecting oldest-first with a
// limit would return the beginning, which is the opposite of what context needs.
func (r *Repository) Recent(ctx context.Context, conversationID uuid.UUID, limit int) ([]Message, error) {
	rows, err := r.q.RecentMessages(ctx, conversationsdb.RecentMessagesParams{
		ConversationID: conversationID,
		Limit:          int32(limit),
	})
	if err != nil {
		return nil, apperr.Wrap(err, "recent messages")
	}

	slices.Reverse(rows)
	return messagesFromDB(rows), nil
}

// RecentUserMessages returns what the user has said lately across every
// conversation, so a brand new thread still has continuity.
func (r *Repository) RecentUserMessages(ctx context.Context, userID uuid.UUID, limit int) ([]Message, error) {
	rows, err := r.q.RecentUserMessages(ctx, conversationsdb.RecentUserMessagesParams{
		UserID: userID,
		Limit:  int32(limit),
	})
	if err != nil {
		return nil, apperr.Wrap(err, "recent user messages")
	}

	slices.Reverse(rows)
	return messagesFromDB(rows), nil
}

func (r *Repository) CountMessages(ctx context.Context, conversationID uuid.UUID) (int, error) {
	n, err := r.q.CountMessages(ctx, conversationID)
	return int(n), apperr.Wrap(err, "count messages")
}

// AwaitingExtraction lists quiet threads the extractor has not read yet.
func (r *Repository) AwaitingExtraction(ctx context.Context, idleBefore time.Time, minMessages, limit int) ([]Pending, error) {
	rows, err := r.q.ConversationsAwaitingExtraction(ctx, conversationsdb.ConversationsAwaitingExtractionParams{
		IdleBefore:  idleBefore,
		MinMessages: int64(minMessages),
		ResultLimit: int32(limit),
	})
	if err != nil {
		return nil, apperr.Wrap(err, "list conversations awaiting extraction")
	}

	out := make([]Pending, len(rows))
	for i, row := range rows {
		out[i] = Pending{ID: row.ID, UserID: row.UserID}
	}
	return out, nil
}

// MarkExtracted records that extraction ran over this thread.
func (r *Repository) MarkExtracted(ctx context.Context, id uuid.UUID) error {
	return apperr.Wrap(r.q.MarkConversationExtracted(ctx, id), "mark conversation extracted")
}

func conversationFromDB(row conversationsdb.Conversation) Conversation {
	c := Conversation{
		ID:                    row.ID,
		UserID:                row.UserID,
		Kind:                  row.Kind,
		Summary:               row.Summary,
		ContextSummary:        row.ContextSummary,
		ContextSummaryThrough: row.ContextSummaryThrough,
		CreatedAt:             row.CreatedAt,
		UpdatedAt:             row.UpdatedAt,
	}
	if c.Kind == "" {
		c.Kind = KindChat
	}
	if row.Title != nil {
		c.Title = *row.Title
	}
	return c
}

func messagesFromDB(rows []conversationsdb.Message) []Message {
	out := make([]Message, 0, len(rows))
	for _, row := range rows {
		out = append(out, messageFromDB(row))
	}
	return out
}

func messageFromDB(row conversationsdb.Message) Message {
	m := Message{
		ID:             row.ID,
		ConversationID: row.ConversationID,
		Role:           ai.Role(row.Role),
		Content:        row.Content,
		CreatedAt:      row.CreatedAt,
	}

	// Malformed JSON in these columns should not make a conversation
	// unreadable: the text is the message, the metadata is decoration.
	if len(row.Parts) > 0 {
		_ = json.Unmarshal(row.Parts, &m.Parts)
	}
	if len(row.Usage) > 0 {
		var usage ai.Usage
		if json.Unmarshal(row.Usage, &usage) == nil {
			m.Usage = &usage
		}
	}
	if row.Model != nil {
		m.Model = *row.Model
	}
	if row.Provider != nil {
		m.Provider = *row.Provider
	}
	m.EvidenceRefs = row.EvidenceRefs

	if len(row.ToolCalls) > 0 {
		_ = json.Unmarshal(row.ToolCalls, &m.ToolCalls)
	}
	if len(row.ToolResults) > 0 {
		_ = json.Unmarshal(row.ToolResults, &m.ToolResults)
	}

	return m
}

// orEmptyStrings keeps a nil slice out of the insert.
//
// The column is NOT NULL DEFAULT '{}', and pgx encodes a nil []string as SQL
// NULL rather than as an empty array, which the constraint rejects.
func orEmptyStrings(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

func nilIfEmpty(s string) *string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return &s
}

// orEmpty keeps a nil slice out of the jsonb column, which would encode as
// "null" and then fail to decode into a slice on the way back.
func orEmpty(parts []Attachment) []Attachment {
	if parts == nil {
		return []Attachment{}
	}
	return parts
}
