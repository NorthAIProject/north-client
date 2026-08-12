// Package goal holds the shape of a goal and its progress notes.
//
// A leaf, so the goals service and the templates that render a goal can both
// import it without importing each other. See CLAUDE.md on slice layout.
//
// A goal is the thing that makes coaching coaching rather than conversation.
// Without one the coach can only react to what is in front of it; with one it
// has something to measure a week against.
package goal

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/shared/lifedomain"
)

// Status values a goal can hold.
const (
	StatusActive    = "active"
	StatusAchieved  = "achieved"
	StatusPaused    = "paused"
	StatusAbandoned = "abandoned"
)

// A goal's category is a life domain, and goals is no longer the only slice
// that needs that vocabulary — habits classify themselves the same way. The
// list therefore lives in internal/shared/lifedomain now, and these names are
// kept as aliases so goals' own callers (handler, templates, validation) read
// in goals' own language.
//
// Same values, same order as before: nothing stored needs migrating.
const (
	CategoryFitness  = lifedomain.Fitness
	CategoryHealth   = lifedomain.Health
	CategoryWork     = lifedomain.Work
	CategoryLearning = lifedomain.Learning
	CategoryPersonal = lifedomain.Personal
	CategoryOther    = lifedomain.Other
)

// Categories is the ordered set offered in the UI.
var Categories = lifedomain.Domains

// Statuses that a user can move a goal to.
var Statuses = []string{StatusActive, StatusAchieved, StatusPaused, StatusAbandoned}

// Milestone status values.
const (
	MilestoneOpen      = "open"
	MilestoneCompleted = "completed"
)

// MilestoneStatuses that a user can move a checkpoint to.
var MilestoneStatuses = []string{MilestoneOpen, MilestoneCompleted}

// Goal is something the user is working toward.
type Goal struct {
	ID     uuid.UUID
	UserID uuid.UUID

	Title string

	// Motivation is why it matters to them. A goal without a reason is a task.
	Motivation string

	// Success is how they will know it happened, in their own words.
	Success string

	Category string
	Status   string

	// TargetDate is zero when the goal is open-ended, which is common and fine.
	TargetDate time.Time

	CreatedAt time.Time
	UpdatedAt time.Time
	ClosedAt  *time.Time

	// LatestUpdate is the most recent progress note, when one has been loaded.
	LatestUpdate *Update

	// Milestones is the checklist, loaded on the detail page. Empty on the
	// list; MilestoneTotal / MilestoneDone still carry the counts there.
	Milestones     []Milestone
	MilestoneTotal int
	MilestoneDone  int
}

// Milestone is a checkpoint on the way to a goal.
type Milestone struct {
	ID     uuid.UUID
	GoalID uuid.UUID
	UserID uuid.UUID

	Title    string
	Status   string
	Position int

	// TargetDate is zero when the checkpoint has no date.
	TargetDate time.Time

	CompletedAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (m Milestone) IsComplete() bool { return m.Status == MilestoneCompleted }

func (m Milestone) HasDeadline() bool { return !m.TargetDate.IsZero() }

// Update is a progress note against a goal.
type Update struct {
	ID     uuid.UUID
	GoalID uuid.UUID

	Note string

	// Progress is an optional self-assessment from 0 to 100. Nil when the note
	// is just a note, which is most of the time.
	Progress *int

	CreatedAt time.Time
}

func (g Goal) IsActive() bool    { return g.Status == StatusActive }
func (g Goal) HasDeadline() bool { return !g.TargetDate.IsZero() }

// WithMilestones attaches the checklist and its counts so Progress can see them.
func (g Goal) WithMilestones(ms []Milestone) Goal {
	g.Milestones = ms
	g.MilestoneTotal = len(ms)
	g.MilestoneDone = 0
	for _, m := range ms {
		if m.IsComplete() {
			g.MilestoneDone++
		}
	}
	return g
}

// Progress is the current completion, when one can be derived.
//
// Milestones win over a self-assessed note: a checklist is the more honest
// number. No milestones and no note percentage means unset, not zero.
func (g Goal) Progress() (int, bool) {
	if g.MilestoneTotal > 0 {
		return g.MilestoneDone * 100 / g.MilestoneTotal, true
	}
	if g.LatestUpdate != nil && g.LatestUpdate.Progress != nil {
		return *g.LatestUpdate.Progress, true
	}
	return 0, false
}

// ProgressLabel is what the UI prints: "2 of 5" when there are milestones,
// "40%" when only a note has a percentage, empty when there is nothing to show.
func (g Goal) ProgressLabel() string {
	if g.MilestoneTotal > 0 {
		return fmt.Sprintf("%d of %d", g.MilestoneDone, g.MilestoneTotal)
	}
	if pct, ok := g.Progress(); ok {
		return fmt.Sprintf("%d%%", pct)
	}
	return ""
}

// DaysRemaining is how long is left, negative when the date has passed.
// Meaningless without a deadline, so check HasDeadline first.
func (g Goal) DaysRemaining() int {
	if !g.HasDeadline() {
		return 0
	}
	return int(time.Until(g.TargetDate).Hours() / 24)
}

// Overdue reports an active goal whose date has passed. Only active goals can
// be overdue: an achieved goal finished late is still achieved.
func (g Goal) Overdue() bool {
	return g.IsActive() && g.HasDeadline() && g.DaysRemaining() < 0
}

// Deadline renders the target date for display.
func (g Goal) Deadline() string {
	if !g.HasDeadline() {
		return "No deadline"
	}

	days := g.DaysRemaining()
	switch {
	case days < 0:
		return fmt.Sprintf("%s — %d days ago", g.TargetDate.Format("2 Jan 2006"), -days)
	case days == 0:
		return "Today"
	case days == 1:
		return "Tomorrow"
	case days < 30:
		return fmt.Sprintf("%s — %d days", g.TargetDate.Format("2 Jan"), days)
	default:
		return g.TargetDate.Format("2 January 2006")
	}
}

// Summary renders a goal for the coach's context.
//
// Compact by design: the coach may be carrying several goals plus a training
// plan plus recent check-ins, and a verbose rendering of each crowds out the
// conversation itself.
func (g Goal) Summary() string {
	var b strings.Builder

	b.WriteString(g.Title)

	if g.HasDeadline() {
		fmt.Fprintf(&b, " (by %s", g.TargetDate.Format("2 Jan 2006"))
		if g.Overdue() {
			b.WriteString(", overdue")
		}
		b.WriteString(")")
	}

	if why := strings.TrimSpace(g.Motivation); why != "" {
		fmt.Fprintf(&b, " — because %s", why)
	}
	if how := strings.TrimSpace(g.Success); how != "" {
		fmt.Fprintf(&b, "; done when %s", how)
	}

	if g.MilestoneTotal > 0 {
		fmt.Fprintf(&b, "; %d of %d milestones", g.MilestoneDone, g.MilestoneTotal)
	} else if g.LatestUpdate != nil && g.LatestUpdate.Progress != nil {
		fmt.Fprintf(&b, "; %d%%", *g.LatestUpdate.Progress)
	}

	if g.LatestUpdate != nil {
		fmt.Fprintf(&b, ". Last note %s: %s",
			g.LatestUpdate.CreatedAt.Format("2 Jan"),
			truncate(g.LatestUpdate.Note, 160))
	}

	return b.String()
}

func truncate(s string, max int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
