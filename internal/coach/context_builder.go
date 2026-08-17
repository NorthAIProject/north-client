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
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/conversations"
	"github.com/NorthAIProject/north-client/internal/shared/metrics"
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

	// ConversationSummary is what was said earlier in this same thread, beyond
	// the tail. Without it a long conversation simply loses its beginning: the
	// tail is a fixed number of turns, and everything older used to be dropped
	// with nothing standing in for it.
	ConversationSummary string

	// EarlierTopics are the user's recent messages from other conversations, so
	// a brand new thread still has continuity.
	EarlierTopics []conversations.Message

	// Sources that will be added as their slices are built. Named here so the
	// prompt renderer already handles their absence, which is the normal state
	// for a new account.
	Goals    []string
	CheckIns []string

	// Calendar is the next seven days from an external server the person
	// connected, already reduced to summary strings. Empty when nothing is
	// connected, which is the normal state.
	Calendar     []string
	WorkoutPlan  string
	FormAnalyses []string

	// Memories and KnowledgeHits are retrieved rather than listed: which of
	// them appear depends on what the user just said. They carry Evidence
	// instead of plain strings so the coach can cite them, and so that after
	// the fact we can say which stored facts a reply was actually built from.
	Memories      []Evidence
	KnowledgeHits []Evidence

	// FitnessSummary is biometrics/BMR/TDEE/macro targets (calculator) and
	// today's activity burn (activity). Populated by two sources on purpose:
	// both are "numbers about the body," and splitting them into separate
	// fields would make Render() enumerate near-duplicate headings.
	FitnessSummary []string

	// Nutrition is today's logged intake versus the current macro goal
	// (meals: FoodLogService + TrackMealProgressService), plus the user's
	// standing diet preferences.
	Nutrition []string

	// Preferences is standing settings the coach should default to when it
	// makes suggestions: units system, fitness objective, macro-split
	// preference (internal/preferences).
	Preferences []string

	// Reflections is recent mood trend and journal entries (internal/mind),
	// giving the coach visibility beyond the structured check-in fields.
	Reflections []string

	// DailySignals is the day-to-day lifestyle background: sleep and
	// hydration. Populated by two sources for the same reason FitnessSummary
	// is — these are the ambient numbers a coach reads before interpreting
	// anything else, and one heading each would make the prompt a list of
	// near-empty sections.
	//
	// They matter here more than their size suggests: bad sleep is the
	// likeliest explanation for a week that went wrong in every other domain,
	// and the coach can only make that connection if it can see both.
	DailySignals []string

	// Habits is recurring intentions with their streaks and adherence
	// (internal/habits). Separate from DailySignals because it carries an
	// expectation: a missed habit is information, a missed log is not.
	Habits []string

	// Reports is the latest ready weekly review, when one exists.
	Reports []string

	// Decisions is recorded calls from the decision journal
	// (internal/decisions). Separate from Reflections: a journal entry is
	// how someone felt, a decision is what they chose.
	Decisions []string
}

// Evidence is one retrieved fact carrying enough provenance to cite it.
//
// Everything the coach knew used to arrive as a bare string, which meant a
// reply could not be traced back to what produced it — not by the model, not by
// the interface, and not by whoever is reading a log at 2am working out why the
// coach said something odd. A fact and its origin travel together now.
type Evidence struct {
	// Ref is the stable handle the model cites and the reply records, in the
	// form "memory:<uuid>" or "chunk:<id>". Prefixed by kind because the two
	// kinds resolve against different tables, and a bare id in a stored reply
	// would be unresolvable a month later.
	Ref string

	// Text is the fact itself, as stored.
	Text string

	// Label says where it came from in words a person would use — "profile
	// fact", "note: Training log › Deload weeks".
	Label string

	// Snippet is the matched excerpt with the matching terms bracketed, or
	// empty when the fact was included for a reason other than matching (a
	// pinned memory, say). Shown to the user; never sent to the model, which
	// gets Text and would only have to ignore the brackets.
	Snippet string
}

// ContextSource contributes one section of the context.
//
// New sources are added by writing one of these and registering it, rather than
// by editing the builder. Goals, check-ins, fitness, and knowledge search all
// arrive this way.
type ContextSource interface {
	// Name identifies the source in logs and timing.
	Name() string

	// Collect fills its part of the context.
	//
	// It is given a Context of its own, not the one being assembled: sources
	// run concurrently, so a source cannot see what any other source found.
	// Only User and LocalTime are populated on the way in.
	//
	// This used to promise the opposite — that a source could read what earlier
	// sources had contributed. No implementation ever did, and keeping the
	// promise would have meant running eighteen independent reads in series.
	//
	// A source that genuinely needs another's output should depend on that
	// service directly rather than on the order the builder happens to use.
	Collect(ctx context.Context, req ContextRequest, into *Context) error
}

// ContextRequest is what a source is told about the request being served.
type ContextRequest struct {
	User           users.User
	ConversationID uuid.UUID

	// Query is what the user just said, and it is the only thing that lets a
	// source rank rather than merely list.
	//
	// Without it every source could do exactly one thing — return its newest
	// rows — so a person with two hundred stored facts got the twenty most
	// recent whether or not any of them bore on the question. Sources that
	// have nothing to rank ignore this field.
	//
	// Empty on background jobs and wherever no user turn prompted the build. A
	// source must treat that as "contribute nothing", not as "fall back to
	// everything": a retrieval source with no query has no reason to prefer
	// any particular fact, and guessing fills the context window with noise.
	Query string
}

// ContextBuilder assembles a Context from its registered sources.
type ContextBuilder struct {
	conversations *conversations.Service
	sources       []ContextSource

	// recentTurns bounds how much of the current conversation is included.
	recentTurns int

	// earlierTopics bounds the cross-conversation continuity.
	earlierTopics int

	// metrics counts sources that failed. Nil is a working builder that counts
	// nothing.
	metrics *metrics.Registry
}

// WithMetrics attaches counters. Returns the builder so it can be wired in one
// expression at startup; nil leaves counting off.
func (b *ContextBuilder) WithMetrics(m *metrics.Registry) *ContextBuilder {
	b.metrics = m
	return b
}

// defaultRecentTurns is how many turns of the current thread go to the model
// verbatim. Everything older is represented by the rolling summary instead.
const defaultRecentTurns = 20

// sourceTimeout bounds one context source.
//
// Generous for a local query and tight for a remote one, which is the point:
// the calendar source dials somebody else's server, and a person waiting for a
// reply should not pay for that server being slow. A source that misses this
// deadline is treated exactly like one that failed — logged, counted, and left
// out of the prompt.
const sourceTimeout = 3 * time.Second

func NewContextBuilder(convos *conversations.Service, sources ...ContextSource) *ContextBuilder {
	return &ContextBuilder{
		conversations: convos,
		sources:       sources,
		recentTurns:   defaultRecentTurns,
		earlierTopics: 8,
	}
}

// RecentTurns is the size of the verbatim tail.
//
// Exposed so the summariser folds in exactly the turns this builder stops
// sending. Two independent constants would drift, and the failure mode of
// drifting is losing history without noticing.
func (b *ContextBuilder) RecentTurns() int { return b.recentTurns }

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

		// The compaction of everything older than that tail. Optional in the
		// way a context source is: a thread short enough never to have been
		// summarised has none, and failing to read it must not cost the reply
		// the turns it can still see.
		if convo, err := b.conversations.Get(ctx, req.ConversationID, req.User.ID); err != nil {
			logSourceFailure(ctx, "conversation_summary", err)
			b.metrics.ContextSourceFailed("conversation_summary")
		} else if convo.HasContextSummary() {
			out.ConversationSummary = convo.ContextSummary
		}
	}

	if earlier, err := b.conversations.RecentUserMessages(ctx, req.User.ID, b.earlierTopics); err == nil {
		out.EarlierTopics = excludeConversation(earlier, req.ConversationID)
	}

	b.collect(ctx, req, out)

	return out, nil
}

// collect runs every source concurrently and folds the results into out.
//
// Concurrent because these are eighteen independent reads, most of them a
// database round trip and one of them a call to somebody else's MCP server.
// Run in series they add up: the slowest source used to set the floor for how
// long a person waits before the coach starts answering, and every feature
// added made that floor higher.
//
// Each source is given its own empty Context rather than a shared one, and the
// results are merged afterwards in registration order. That avoids a mutex
// without changing what the model sees: two sources write FitnessSummary and
// three write DailySignals, so a shared struct would be a genuine race, while
// merging in order reproduces exactly the sequence the serial loop produced.
func (b *ContextBuilder) collect(ctx context.Context, req ContextRequest, out *Context) {
	parts := make([]*Context, len(b.sources))

	var wg sync.WaitGroup
	for i, source := range b.sources {
		wg.Add(1)
		go func() {
			defer wg.Done()

			// A deadline per source, so one that hangs costs its own budget
			// rather than the whole reply. Derived from the request context, so
			// cancelling the request still stops all of them at once.
			sourceCtx, cancel := context.WithTimeout(ctx, sourceTimeout)
			defer cancel()

			// Its own Context to fill. Nothing reads from the context under
			// construction — checked across all eighteen implementations — so
			// nothing is lost by not sharing it.
			part := &Context{User: req.User, LocalTime: out.LocalTime}
			if err := source.Collect(sourceCtx, req, part); err != nil {
				logSourceFailure(ctx, source.Name(), err)

				// Counted as well as logged. These fail soft, so the reply still
				// looks right and the log line is the only sign — which means it
				// is the sign nobody sees until somebody goes looking.
				b.metrics.ContextSourceFailed(source.Name())
				return
			}
			parts[i] = part
		}()
	}
	wg.Wait()

	// In registration order, not completion order. A prompt that reordered
	// itself according to which query finished first would make one reply
	// impossible to compare against the next.
	for _, part := range parts {
		if part != nil {
			out.merge(part)
		}
	}
}

// merge folds one source's contribution into the context being built.
//
// Every field a source may write is listed here. A new field on Context that is
// not added to this function will silently never reach the model, which is why
// this sits directly beneath the struct's own documentation rather than in a
// file of its own.
func (c *Context) merge(part *Context) {
	c.Goals = append(c.Goals, part.Goals...)
	c.CheckIns = append(c.CheckIns, part.CheckIns...)
	c.Calendar = append(c.Calendar, part.Calendar...)
	c.FormAnalyses = append(c.FormAnalyses, part.FormAnalyses...)
	c.Memories = append(c.Memories, part.Memories...)
	c.KnowledgeHits = append(c.KnowledgeHits, part.KnowledgeHits...)
	c.FitnessSummary = append(c.FitnessSummary, part.FitnessSummary...)
	c.Nutrition = append(c.Nutrition, part.Nutrition...)
	c.Preferences = append(c.Preferences, part.Preferences...)
	c.Reflections = append(c.Reflections, part.Reflections...)
	c.DailySignals = append(c.DailySignals, part.DailySignals...)
	c.Habits = append(c.Habits, part.Habits...)
	c.Reports = append(c.Reports, part.Reports...)
	c.Decisions = append(c.Decisions, part.Decisions...)

	// The one scalar a source sets. Last non-empty wins, which matches the
	// serial loop: only workouts writes it.
	if part.WorkoutPlan != "" {
		c.WorkoutPlan = part.WorkoutPlan
	}
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
	evidenceSection(&b, "Known about them", c.Memories, "none recorded yet")

	b.WriteString("\nCurrent training plan: ")
	if plan := strings.TrimSpace(c.WorkoutPlan); plan != "" {
		b.WriteString("\n" + plan + "\n")
	} else {
		b.WriteString("none yet\n")
	}

	section(&b, "Recent form analyses", c.FormAnalyses, "none yet")

	section(&b, "Fitness & nutrition targets", c.FitnessSummary, "not calculated yet")
	section(&b, "Today's nutrition", c.Nutrition, "nothing logged yet")
	section(&b, "Sleep & hydration", c.DailySignals, "nothing logged yet")
	section(&b, "Habits", c.Habits, "none set up yet")
	section(&b, "Preferences", c.Preferences, "not set yet")
	section(&b, "Reflections", c.Reflections, "none yet")
	section(&b, "Decisions", c.Decisions, "none recorded yet")
	section(&b, "Latest weekly review", c.Reports, "none yet")
	section(&b, "Next 7 days (their calendar)", c.Calendar, "no calendar connected")

	// Before the other-conversation continuity block: this is the same thread
	// the tail comes from, so it reads as one story rather than as an aside.
	if summary := strings.TrimSpace(c.ConversationSummary); summary != "" {
		b.WriteString("\nEarlier in this conversation:\n")
		b.WriteString(summary + "\n")
	}

	if len(c.EarlierTopics) > 0 {
		b.WriteString("\nWhat they have been talking about in other conversations:\n")
		for _, m := range c.EarlierTopics {
			fmt.Fprintf(&b, "- [%s] %s\n", m.CreatedAt.In(c.User.Location()).Format("2 Jan"), truncate(m.Content, 200))
		}
	}

	if len(c.KnowledgeHits) > 0 {
		evidenceSection(&b, "Relevant notes from their knowledge base", c.KnowledgeHits, "")
	}

	return strings.TrimSpace(b.String())
}

// evidenceSection renders a retrieved section with each fact's ref attached.
//
// The ref is written in double brackets because that is what the system prompt
// asks the model to echo when it uses a fact, and a form that already appears
// beside the fact is one the model copies rather than invents.
func evidenceSection(b *strings.Builder, heading string, items []Evidence, whenEmpty string) {
	if len(items) == 0 {
		section(b, heading, nil, whenEmpty)
		return
	}

	b.WriteString("\n" + heading + ":\n")
	for _, it := range items {
		fmt.Fprintf(b, "- [[%s]] %s\n", it.Ref, it.Text)
	}
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
