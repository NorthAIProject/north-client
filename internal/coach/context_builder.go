// Package coach is North's core: it assembles what the AI knows about a person
// and turns their message into a reply.
//
// Everything conversational flows through here. The web handler, and later the
// Telegram and MCP adapters, all call CoachService — none of them build a
// prompt or talk to a provider themselves.
package coach

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/conversations"
	"github.com/NorthAIProject/north-client/internal/users"
)

// Context is everything the coach is allowed to know about a person for one
// request.
//
// The system prompt tells the model that this block is the entire extent of its
// knowledge, so whatever is missing here is something the coach will correctly
// admit it does not know. That makes this struct the product's memory, and
// adding a source to it is how North gets smarter.
type Context struct {
	User users.User

	// LocalTime is the user's wall clock. A coach that says "good morning" at
	// the user's midnight is obviously not paying attention.
	LocalTime time.Time

	// RecentMessages is the tail of the current conversation.
	RecentMessages []conversations.Message

	// EarlierTopics are the user's recent messages from other conversations, so
	// a brand new thread still has continuity.
	EarlierTopics []conversations.Message

	// Sources that will be added as their slices are built. Named here so the
	// prompt renderer already handles their absence, which is the normal state
	// for a new account.
	Goals         []string
	CheckIns      []string
	Memories      []string
	WorkoutPlan   string
	FormAnalyses  []string
	KnowledgeHits []string
}

// ContextSource contributes one section of the context.
//
// New sources are added by writing one of these and registering it, rather than
// by editing the builder. Goals, check-ins, fitness, and knowledge search all
// arrive this way.
type ContextSource interface {
	// Name identifies the source in logs and timing.
	Name() string

	// Collect fills its part of the context. It is given the context under
	// construction so a source may read what earlier sources found.
	Collect(ctx context.Context, req ContextRequest, into *Context) error
}

// ContextRequest is what a source is told about the request being served.
type ContextRequest struct {
	User           users.User
	ConversationID uuid.UUID
}

// ContextBuilder assembles a Context from its registered sources.
type ContextBuilder struct {
	conversations *conversations.Service
	sources       []ContextSource

	// recentTurns bounds how much of the current conversation is included.
	recentTurns int

	// earlierTopics bounds the cross-conversation continuity.
	earlierTopics int
}

func NewContextBuilder(convos *conversations.Service, sources ...ContextSource) *ContextBuilder {
	return &ContextBuilder{
		conversations: convos,
		sources:       sources,
		recentTurns:   20,
		earlierTopics: 8,
	}
}

// Build gathers context for one request.
//
// A failing source degrades the reply rather than failing it: a coach that
// cannot reach the goals table should still answer, having correctly said it
// cannot see the user's goals. Returning an error here would turn a partial
// outage into a total one.
func (b *ContextBuilder) Build(ctx context.Context, req ContextRequest) (*Context, error) {
	out := &Context{
		User:      req.User,
		LocalTime: time.Now().In(req.User.Location()),
	}

	if req.ConversationID != uuid.Nil {
		recent, err := b.conversations.Recent(ctx, req.ConversationID, b.recentTurns)
		if err != nil {
			return nil, err // the conversation itself is not optional
		}
		out.RecentMessages = recent
	}

	if earlier, err := b.conversations.RecentUserMessages(ctx, req.User.ID, b.earlierTopics); err == nil {
		out.EarlierTopics = excludeConversation(earlier, req.ConversationID)
	}

	for _, source := range b.sources {
		if err := source.Collect(ctx, req, out); err != nil {
			logSourceFailure(ctx, source.Name(), err)
		}
	}

	return out, nil
}

// excludeConversation drops messages already covered by RecentMessages, so the
// same text is not sent to the model twice.
func excludeConversation(messages []conversations.Message, conversationID uuid.UUID) []conversations.Message {
	if conversationID == uuid.Nil {
		return messages
	}

	out := make([]conversations.Message, 0, len(messages))
	for _, m := range messages {
		if m.ConversationID != conversationID {
			out = append(out, m)
		}
	}
	return out
}

// Render turns the context into the block that goes into the system prompt.
//
// Written as prose with headings rather than JSON: models read this format more
// reliably, and it stays readable in a log when someone is working out why the
// coach said what it said.
//
// Absent sections are labelled rather than omitted. "Goals: none recorded yet"
// tells the model there are none; silence invites it to assume.
func (c *Context) Render() string {
	var b strings.Builder

	b.WriteString("Name: " + c.User.DisplayName + "\n")
	b.WriteString("Local time: " + c.LocalTime.Format("Monday 2 January 2006, 15:04") + "\n")
	b.WriteString("Timezone: " + c.User.Timezone + "\n")

	if style := strings.TrimSpace(c.User.CoachingStyle); style != "" {
		b.WriteString("How they want to be coached: " + style + "\n")
	}

	section(&b, "Goals", c.Goals, "none recorded yet")
	section(&b, "Recent check-ins", c.CheckIns, "none recorded yet")
	section(&b, "Known about them", c.Memories, "none recorded yet")

	b.WriteString("\nCurrent training plan: ")
	if plan := strings.TrimSpace(c.WorkoutPlan); plan != "" {
		b.WriteString("\n" + plan + "\n")
	} else {
		b.WriteString("none yet\n")
	}

	section(&b, "Recent form analyses", c.FormAnalyses, "none yet")

	if len(c.EarlierTopics) > 0 {
		b.WriteString("\nWhat they have been talking about in other conversations:\n")
		for _, m := range c.EarlierTopics {
			b.WriteString(fmt.Sprintf("- [%s] %s\n", m.CreatedAt.In(c.User.Location()).Format("2 Jan"), truncate(m.Content, 200)))
		}
	}

	if len(c.KnowledgeHits) > 0 {
		section(&b, "Relevant notes from their knowledge base", c.KnowledgeHits, "")
	}

	return strings.TrimSpace(b.String())
}

func section(b *strings.Builder, heading string, items []string, whenEmpty string) {
	b.WriteString("\n" + heading + ":")
	if len(items) == 0 {
		if whenEmpty != "" {
			b.WriteString(" " + whenEmpty + "\n")
		} else {
			b.WriteString(" none\n")
		}
		return
	}

	b.WriteString("\n")
	for _, item := range items {
		b.WriteString("- " + item + "\n")
	}
}

func truncate(s string, max int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
