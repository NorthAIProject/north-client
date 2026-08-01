package conversations

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"

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

func (r *Repository) Create(ctx context.Context, userID uuid.UUID, title string) (Conversation, error) {
	row, err := r.q.CreateConversation(ctx, conversationsdb.CreateConversationParams{
		UserID: userID,
		Title:  nilIfEmpty(title),
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

	row, err := r.q.AppendMessage(ctx, conversationsdb.AppendMessageParams{
		ConversationID: msg.ConversationID,
		Role:           string(msg.Role),
		Content:        msg.Content,
		Parts:          parts,
		Usage:          usage,
		Model:          nilIfEmpty(msg.Model),
		Provider:       nilIfEmpty(msg.Provider),
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

func conversationFromDB(row conversationsdb.Conversation) Conversation {
	c := Conversation{
		ID:        row.ID,
		UserID:    row.UserID,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
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

	return m
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
