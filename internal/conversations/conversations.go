// Package conversations owns the record of what was said.
//
// It stores and retrieves; it does not decide what the coach replies. That is
// internal/coach. Keeping the two apart is what lets Telegram and MCP reuse the
// same history without inheriting the web request cycle.
package conversations

import (
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
)

// Conversation is one continuous thread with the coach.
type Conversation struct {
	ID     uuid.UUID
	UserID uuid.UUID

	// Title is generated from the opening message. Empty until then.
	Title string

	CreatedAt time.Time
	UpdatedAt time.Time
}

// DisplayTitle is what the sidebar shows, including for a thread whose title
// has not been generated yet.
func (c Conversation) DisplayTitle() string {
	if t := strings.TrimSpace(c.Title); t != "" {
		return t
	}
	return "New conversation"
}

// Message is one turn.
type Message struct {
	ID             uuid.UUID
	ConversationID uuid.UUID
	Role           ai.Role
	Content        string

	// Attachments referenced by this message. Text lives in Content so it stays
	// directly searchable.
	Parts []Attachment

	Usage    *ai.Usage
	Model    string
	Provider string

	CreatedAt time.Time
}

// Attachment is a file referenced by a message.
type Attachment struct {
	// MediaID points at North's own record in internal/media, which is the
	// durable copy. Provider file URIs expire within days.
	MediaID  uuid.UUID `json:"media_id"`
	Kind     string    `json:"kind"`
	MIMEType string    `json:"mime_type"`
	Name     string    `json:"name"`
}

func (m Message) IsUser() bool  { return m.Role == ai.RoleUser }
func (m Message) IsModel() bool { return m.Role == ai.RoleModel }

// ToAIMessages converts stored history into the form the AI layer expects.
func ToAIMessages(messages []Message) []ai.Message {
	out := make([]ai.Message, 0, len(messages))
	for _, m := range messages {
		if strings.TrimSpace(m.Content) == "" {
			// An empty turn carries nothing and some providers reject it.
			continue
		}
		out = append(out, ai.Message{
			Role:  m.Role,
			Parts: []ai.Part{ai.TextPart(m.Content)},
		})
	}
	return out
}
