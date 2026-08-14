package reports

import (
	"context"
	"fmt"
	"time"

	"github.com/NorthAIProject/north-client/internal/insights"
	"github.com/NorthAIProject/north-client/internal/meals"
	"github.com/NorthAIProject/north-client/internal/memories"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/users"
)

// InsightsContext is the production ContextLoader.
//
// It reads through internal/insights rather than through the eight slices
// directly, because insights already answers exactly the question a weekly
// review asks — "what happened between these two instants" — over a
// timerange.Range, and it already does so concurrently. A second traversal of
// the same slices would be a second thing to keep correct.
//
// Nutrition and remembered facts are not part of insights and are read here.
type InsightsContext struct {
	insights *insights.Service
	meals    *meals.TrackMealProgressService
	memories *memories.Service
}

// NewInsightsContext wires the loader. Any dependency may be nil: a deployment
// that has not built one of these slices should produce a review missing that
// section, not a review that fails.
func NewInsightsContext(
	in *insights.Service,
	m *meals.TrackMealProgressService,
	mem *memories.Service,
) *InsightsContext {
	return &InsightsContext{insights: in, meals: m, memories: mem}
}

var _ ContextLoader = (*InsightsContext)(nil)

// Load collects the week.
//
// Every section is optional and an empty one is not an error: a person who
// logged nothing but check-ins should get a review about check-ins, not a
// failed job. What must not happen is the opposite — a week full of records
// rendering as "(none recorded)" because a dependency was never wired.
func (l *InsightsContext) Load(ctx context.Context, user users.User, start, end time.Time) (ReviewContext, error) {
	rg := timerange.Between(start.In(user.Location()), end.In(user.Location()))

	var out ReviewContext

	if l.insights != nil {
		if err := l.loadInsights(ctx, user, rg, &out); err != nil {
			return ReviewContext{}, err
		}
	}

	if l.meals != nil {
		nutrition, err := l.loadNutrition(ctx, user, rg)
		if err != nil {
			return ReviewContext{}, err
		}
		out.Nutrition = nutrition
	}

	if l.memories != nil {
		facts, err := l.loadMemories(ctx, user)
		if err != nil {
			return ReviewContext{}, err
		}
		out.Memories = facts
	}

	return out, nil
}

// loadInsights fills goals, check-ins, training, sleep, hydration and habits.
//
// Sequential rather than concurrent: insights already runs its own errgroup
// inside each of these four calls, and a weekly review is a background job
// where four round trips cost nothing anybody is waiting on.
func (l *InsightsContext) loadInsights(
	ctx context.Context, user users.User, rg timerange.Range, out *ReviewContext,
) error {
	progress, err := l.insights.Progress(ctx, user, rg)
	if err != nil {
		return err
	}
	for _, g := range progress.Active {
		out.Goals = append(out.Goals, g.Summary())
	}
	for _, g := range progress.Opened {
		out.Goals = append(out.Goals, "Opened this week: "+g.Summary())
	}
	for _, note := range progress.Notes {
		out.Goals = append(out.Goals, formatGoalNote(note.GoalTitle, note.Note, note.Progress))
	}
	if progress.Overdue > 0 {
		out.Goals = append(out.Goals,
			fmt.Sprintf("%d milestone(s) are past their target date", progress.Overdue))
	}

	mind, err := l.insights.Mind(ctx, user, rg)
	if err != nil {
		return err
	}
	for _, c := range mind.CheckIns {
		out.CheckIns = append(out.CheckIns, c.Summary())
	}
	if progress.Streak > 0 {
		out.CheckIns = append(out.CheckIns,
			fmt.Sprintf("Current check-in streak: %d day(s)", progress.Streak))
	}
	for _, entry := range mind.Journal {
		out.CheckIns = append(out.CheckIns, "Journal — "+entry.Summary())
	}

	training, err := l.insights.Training(ctx, user, rg)
	if err != nil {
		return err
	}
	if len(training.Sessions) > 0 {
		out.Training = append(out.Training,
			fmt.Sprintf("%d logged session(s) this week", len(training.Sessions)))
	}
	if training.Calories > 0 {
		out.Training = append(out.Training,
			fmt.Sprintf("~%.0f kcal burned (previous week: ~%.0f kcal)", training.Calories, training.Prior))
	}

	body, err := l.insights.Body(ctx, user, rg)
	if err != nil {
		return err
	}
	// Only days with something logged. A run of "drank nothing" lines would
	// crowd out the week's actual signal, and the absence is already legible
	// from a short list against a seven-day window.
	for _, day := range body.Hydration {
		if day.TotalML > 0 {
			out.Hydration = append(out.Hydration, day.Summary())
		}
	}
	if body.SleepTrend.Nights > 0 {
		out.Sleep = append(out.Sleep, body.SleepTrend.Summary())
	}
	for _, night := range body.Nights {
		out.Sleep = append(out.Sleep, night.Summary())
	}
	for _, st := range body.Habits {
		out.Habits = append(out.Habits, st.Summary())
	}

	return nil
}

// loadNutrition reports the days of the week that carry a macro goal.
//
// ForRange skips days with no plan, so a person who logged food on two days
// gets two lines rather than seven, five of which say nothing.
func (l *InsightsContext) loadNutrition(
	ctx context.Context, user users.User, rg timerange.Range,
) ([]string, error) {
	days, err := l.meals.ForRange(ctx, user.ID, rg.Since, rg.Until)
	if err != nil {
		return nil, err
	}

	out := make([]string, 0, len(days))
	for _, day := range days {
		out = append(out, day.Summary())
	}
	return out, nil
}

// loadMemories is the standing profile, not the week: a durable fact about
// somebody is context for reading their week, whenever it was recorded.
func (l *InsightsContext) loadMemories(ctx context.Context, user users.User) ([]string, error) {
	approved, err := l.memories.ListApproved(ctx, user.ID)
	if err != nil {
		return nil, err
	}

	out := make([]string, 0, len(approved))
	for _, m := range approved {
		if m.Excluded {
			continue
		}
		out = append(out, m.Summary())
	}
	return out, nil
}

func formatGoalNote(title, note string, progress *int) string {
	if progress != nil {
		return fmt.Sprintf("Note on %q: %s (%d%%)", title, note, *progress)
	}
	return fmt.Sprintf("Note on %q: %s", title, note)
}
