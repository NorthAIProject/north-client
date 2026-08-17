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

const (
	KindChat       = "chat"
	KindReflection = "reflection"
)

// Conversation is one continuous thread with the coach.
type Conversation struct {
	ID     uuid.UUID
	UserID uuid.UUID

	// Title is generated from the opening message. Empty until then.
	Title string

	// Kind is chat (default) or reflection. Reflection is the same coach
	// with a different prompt and a required closing summary.
	Kind string

	// Summary is the closing write-up of a reflection. Empty on chat
	// threads and on an in-progress reflection.
	Summary string

	// ContextSummary is the rolling compaction of turns that no longer fit in
	// the context window. Distinct from Summary: that one is a reflection's
	// closing write-up and is what Ended() reads. This one exists on any long
	// thread, says nothing about whether it is finished, and is never shown to
	// the person — the UI still lists every message.
	ContextSummary string

	// ContextSummaryThrough is the newest turn ContextSummary covers. Nil when
	// nothing has been summarised yet.
	ContextSummaryThrough *time.Time

	CreatedAt time.Time
	UpdatedAt time.Time
}

// HasContextSummary reports whether there is compacted history to put in front
// of the model.
func (c Conversation) HasContextSummary() bool {
	return strings.TrimSpace(c.ContextSummary) != ""
}

// IsReflection reports whether this thread is a structured reflection.
func (c Conversation) IsReflection() bool { return c.Kind == KindReflection }

// Ended reports whether a reflection has written its summary.
func (c Conversation) Ended() bool {
	return c.IsReflection() && strings.TrimSpace(c.Summary) != ""
}

// Pending is a thread the memory extractor has work to do on.
//
// Deliberately not a Conversation: the sweep needs an id and an owner and
// nothing else, and loading whole rows to throw away every field but two would
// make a background pass over the table more expensive than the work it
// schedules.
type Pending struct {
	ID     uuid.UUID
	UserID uuid.UUID
}

// DisplayTitle is what the sidebar shows, including for a thread whose title
// has not been generated yet.
func (c Conversation) DisplayTitle() string {
	if t := strings.TrimSpace(c.Title); t != "" {
		return t
	}
	if c.IsReflection() {
		return "Reflection"
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

	// EvidenceRefs are the stored facts the coach drew on for this reply, in
	// "memory:<uuid>" / "chunk:<id>" form. Model and Provider record which LLM
	// wrote the words; this records what it was working from.
	EvidenceRefs []string

	// ToolCalls are the tools the model asked for on this turn, and ToolResults
	// are what they answered. Only one of the two is ever set: a call is the
	// model's turn, a result is carried back on the user's.
	//
	// Stored rather than kept in memory because a turn can pause — a write
	// waits for the person to approve it — and the request has to be rebuilt
	// from the database when they do.
	ToolCalls   []ai.ToolCall
	ToolResults []ai.ToolResult

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

// Evidence ref kinds, as stored in EvidenceRefs.
//
// Declared by the package that owns the column rather than by the one that
// writes it: internal/coach builds these refs, but it also renders web/chat,
// and a template that needed to read a ref would have to import the service
// that produced it. The vocabulary of a stored column belongs with the column.
const (
	EvidenceKindMemory = "memory"
	EvidenceKindChunk  = "chunk"

	// EvidenceKindExercise records a catalogue exercise the coach looked up
	// while writing the reply. Unlike the other two it is not something the
	// model cited: it is something it fetched, which is a stronger claim and
	// the reason the chat can draw the muscles worked without asking the model
	// to name them again in a format a template could parse.
	EvidenceKindExercise = "exercise"
)

// IsToolTurn reports whether this turn exists for the model rather than the
// reader: a tool call or the result handed back to it. Both carry no text.
func (m Message) IsToolTurn() bool { return len(m.ToolCalls) > 0 || len(m.ToolResults) > 0 }

func (m Message) IsUser() bool  { return m.Role == ai.RoleUser }
func (m Message) IsModel() bool { return m.Role == ai.RoleModel }

// ChunkIDs is the document passages this reply drew on.
//
// Memory refs are dropped: a stored fact has no line range and no page to open,
// so there is nothing a reader could check. A ref that does not parse is
// dropped too — this is a text column, and a malformed value in it should cost
// a reply one source rather than fail the page.
func (m Message) ChunkIDs() []string {
	out := make([]string, 0, len(m.EvidenceRefs))
	for _, ref := range m.EvidenceRefs {
		kind, id, found := strings.Cut(ref, ":")
		if found && id != "" && kind == EvidenceKindChunk {
			out = append(out, id)
		}
	}
	return out
}

// ExerciseSlugs is the catalogue exercises the coach read while writing this
// reply, in the order it read them.
//
// Slugs rather than ids because that is what get_exercise takes and what the
// catalogue is addressed by everywhere else.
func (m Message) ExerciseSlugs() []string {
	out := make([]string, 0, len(m.EvidenceRefs))
	for _, ref := range m.EvidenceRefs {
		kind, slug, found := strings.Cut(ref, ":")
		if found && slug != "" && kind == EvidenceKindExercise {
			out = append(out, slug)
		}
	}
	return out
}

// ToAIMessages converts stored history into the form the AI layer expects.
func ToAIMessages(messages []Message) []ai.Message {
	out := make([]ai.Message, 0, len(messages))
	for _, m := range messages {
		// A tool turn carries no text, so the emptiness rule below would drop
		// exactly the turns a resumed request needs. Passed through first.
		switch {
		case len(m.ToolCalls) > 0:
			out = append(out, ai.ToolCallMessage(m.ToolCalls))
			continue
		case len(m.ToolResults) > 0:
			out = append(out, ai.ToolResultMessage(m.ToolResults))
			continue
		}

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
