package dashboard

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

	"github.com/NorthAIProject/north-client/internal/activity/activity"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/users"
)

// EntryKind names the slice an entry came from. The feed groups and filters by
// it, and each kind carries its own icon.
type EntryKind string

const (
	KindCheckIn   EntryKind = "checkin"
	KindHydration EntryKind = "hydration"
	KindSleep     EntryKind = "sleep"
	KindHabit     EntryKind = "habit"
	KindJournal   EntryKind = "journal"
	KindGoal      EntryKind = "goal"
	KindGoalNote  EntryKind = "goal-note"
	KindActivity  EntryKind = "activity"
)

// Entry is one thing that happened, flattened out of whichever slice owns it.
//
// The feed is assembled by querying each slice for its window and merging the
// results, rather than by reading an events table. That choice is deliberate:
// an events table would only contain what happened after it shipped, whereas
// this shows a person their whole history from the first deploy.
type Entry struct {
	Kind EntryKind

	// At is when the thing happened, in the reader's timezone — not when the
	// row was written. A night's sleep belongs to the morning it counts
	// toward, not to the evening someone got round to logging it.
	At time.Time

	Title  string
	Detail string

	// Href deep-links to the record in the slice that owns it. Empty when the
	// slice has no page for a single row.
	Href string

	// Icon is a Lucide name, resolved by the template through icon.Icon.
	Icon string
}

// Label is the kind rendered for a human, used by the feed's filter chips.
func (k EntryKind) Label() string {
	switch k {
	case KindCheckIn:
		return "Check-in"
	case KindHydration:
		return "Water"
	case KindSleep:
		return "Sleep"
	case KindHabit:
		return "Habit"
	case KindJournal:
		return "Journal"
	case KindGoal:
		return "Goal"
	case KindGoalNote:
		return "Goal note"
	case KindActivity:
		return "Activity"
	default:
		return string(k)
	}
}

// Timeline returns everything that happened in a window, newest first.
//
// Each slice is queried concurrently; the first real error fails the whole
// feed rather than silently returning a partial history, because a feed with a
// hole in it is worse than an error message.
func (s *Service) Timeline(ctx context.Context, user users.User, rg timerange.Range, limit int) ([]Entry, error) {
	var (
		g, gctx = errgroup.WithContext(ctx)
		parts   = make([][]Entry, 8)
	)

	g.Go(func() (err error) { parts[0], err = s.checkInEntries(gctx, user, rg); return })
	g.Go(func() (err error) { parts[1], err = s.hydrationEntries(gctx, user, rg); return })
	g.Go(func() (err error) { parts[2], err = s.sleepEntries(gctx, user, rg); return })
	g.Go(func() (err error) { parts[3], err = s.habitEntries(gctx, user, rg); return })
	g.Go(func() (err error) { parts[4], err = s.journalEntries(gctx, user, rg); return })
	g.Go(func() (err error) { parts[5], err = s.goalNoteEntries(gctx, user, rg); return })
	g.Go(func() (err error) { parts[6], err = s.goalEntries(gctx, user, rg); return })
	g.Go(func() (err error) { parts[7], err = s.activityEntries(gctx, user, rg); return })

	if err := g.Wait(); err != nil {
		return nil, err
	}

	var all []Entry
	for _, p := range parts {
		all = append(all, p...)
	}

	loc := rg.Location()
	for i := range all {
		all[i].At = all[i].At.In(loc)
	}

	// Newest first, with the kind as a tiebreak so two things logged in the
	// same second do not shuffle between renders of the same page.
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].At.Equal(all[j].At) {
			return all[i].Kind < all[j].Kind
		}
		return all[i].At.After(all[j].At)
	})

	if limit > 0 && len(all) > limit {
		all = all[:limit]
	}
	return all, nil
}

func (s *Service) checkInEntries(ctx context.Context, user users.User, rg timerange.Range) ([]Entry, error) {
	if s.checkins == nil {
		return nil, nil
	}
	list, err := s.checkins.ListBetween(ctx, user.ID, rg)
	if err != nil {
		return nil, err
	}

	out := make([]Entry, 0, len(list))
	for _, c := range list {
		detail := fmt.Sprintf("Mood %d · Energy %d", c.Mood, c.Energy)
		if c.Wins != "" {
			detail += " · " + firstLine(c.Wins)
		}
		out = append(out, Entry{
			Kind:   KindCheckIn,
			At:     c.CreatedAt,
			Title:  "Checked in",
			Detail: detail,
			Href:   "/app/check-ins",
			Icon:   "smile",
		})
	}
	return out, nil
}

func (s *Service) hydrationEntries(ctx context.Context, user users.User, rg timerange.Range) ([]Entry, error) {
	if s.hydration == nil {
		return nil, nil
	}
	list, err := s.hydration.EntriesBetween(ctx, user, rg)
	if err != nil {
		return nil, err
	}

	out := make([]Entry, 0, len(list))
	for _, e := range list {
		out = append(out, Entry{
			Kind:  KindHydration,
			At:    e.LoggedAt,
			Title: fmt.Sprintf("Drank %s", formatML(e.AmountML)),
			Href:  "/app/care",
			Icon:  "droplet",
		})
	}
	return out, nil
}

func (s *Service) sleepEntries(ctx context.Context, user users.User, rg timerange.Range) ([]Entry, error) {
	if s.sleep == nil {
		return nil, nil
	}
	list, err := s.sleep.ListBetween(ctx, user, rg)
	if err != nil {
		return nil, err
	}

	out := make([]Entry, 0, len(list))
	for _, l := range list {
		detail := ""
		if l.Quality != nil {
			detail = fmt.Sprintf("Quality %d/5", *l.Quality)
		}
		out = append(out, Entry{
			Kind: KindSleep,
			// The morning the sleep counts toward, not when it was typed in.
			At:     l.LocalDate,
			Title:  fmt.Sprintf("Slept %s", formatMinutes(l.DurationMinutes)),
			Detail: detail,
			Href:   "/app/care",
			Icon:   "moon",
		})
	}
	return out, nil
}

func (s *Service) habitEntries(ctx context.Context, user users.User, rg timerange.Range) ([]Entry, error) {
	if s.habits == nil {
		return nil, nil
	}
	done, err := s.habits.CompletionsBetween(ctx, user, rg)
	if err != nil {
		return nil, err
	}
	if len(done) == 0 {
		return nil, nil
	}

	// Names come from a second query rather than a join: the completions table
	// is the hot one, and a person has a handful of habits, not thousands.
	all, err := s.habits.List(ctx, user, false)
	if err != nil {
		return nil, err
	}
	names := make(map[uuid.UUID]string, len(all))
	for _, h := range all {
		names[h.ID] = h.Name
	}

	out := make([]Entry, 0, len(done))
	for _, c := range done {
		name := names[c.HabitID]
		if name == "" {
			name = "a habit"
		}
		out = append(out, Entry{
			Kind:  KindHabit,
			At:    c.CompletedAt,
			Title: "Kept " + name,
			Href:  "/app/care",
			Icon:  "circle-check",
		})
	}
	return out, nil
}

func (s *Service) journalEntries(ctx context.Context, user users.User, rg timerange.Range) ([]Entry, error) {
	if s.mind == nil {
		return nil, nil
	}
	list, err := s.mind.ListBetween(ctx, user.ID, rg)
	if err != nil {
		return nil, err
	}

	out := make([]Entry, 0, len(list))
	for _, e := range list {
		out = append(out, Entry{
			Kind:   KindJournal,
			At:     e.CreatedAt,
			Title:  "Wrote in the journal",
			Detail: firstLine(e.Content),
			Href:   "/app/mind",
			Icon:   "notebook-pen",
		})
	}
	return out, nil
}

func (s *Service) goalNoteEntries(ctx context.Context, user users.User, rg timerange.Range) ([]Entry, error) {
	if s.goals == nil {
		return nil, nil
	}
	list, err := s.goals.UpdatesBetween(ctx, user.ID, rg)
	if err != nil {
		return nil, err
	}

	out := make([]Entry, 0, len(list))
	for _, u := range list {
		detail := firstLine(u.Note)
		if u.Progress != nil {
			detail = fmt.Sprintf("%d%% · %s", *u.Progress, detail)
		}
		out = append(out, Entry{
			Kind:   KindGoalNote,
			At:     u.CreatedAt,
			Title:  "Progress on " + u.GoalTitle,
			Detail: detail,
			Href:   "/app/goals/" + u.GoalID.String(),
			Icon:   "flag",
		})
	}
	return out, nil
}

func (s *Service) goalEntries(ctx context.Context, user users.User, rg timerange.Range) ([]Entry, error) {
	if s.goals == nil {
		return nil, nil
	}
	list, err := s.goals.CreatedBetween(ctx, user.ID, rg)
	if err != nil {
		return nil, err
	}

	out := make([]Entry, 0, len(list))
	for _, g := range list {
		out = append(out, Entry{
			Kind:  KindGoal,
			At:    g.CreatedAt,
			Title: "Set a goal: " + g.Title,
			Href:  "/app/goals/" + g.ID.String(),
			Icon:  "target",
		})
	}
	return out, nil
}

func (s *Service) activityEntries(ctx context.Context, user users.User, rg timerange.Range) ([]Entry, error) {
	if s.activity == nil {
		return nil, nil
	}
	list, err := s.activity.ListBetween(ctx, user.ID, rg)
	if err != nil {
		return nil, err
	}

	out := make([]Entry, 0, len(list))
	for _, sess := range list {
		if sess.EndedAt == nil {
			continue
		}
		name := sess.ActivityCode
		if met, ok := activity.LookupMET(sess.ActivityCode); ok {
			name = met.Name
		}
		detail := ""
		if sess.CaloriesBurned != nil {
			detail = fmt.Sprintf("%.0f kcal", *sess.CaloriesBurned)
		}
		out = append(out, Entry{
			Kind:   KindActivity,
			At:     *sess.EndedAt,
			Title:  name,
			Detail: detail,
			Href:   "/app/fitness/activities",
			Icon:   "activity",
		})
	}
	return out, nil
}

// firstLine keeps a feed row to one line. Free text in a timeline that wraps
// to a paragraph turns the feed into a document.
func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	const max = 80
	if len(s) > max {
		return strings.TrimSpace(s[:max]) + "…"
	}
	return s
}

func formatML(ml int) string {
	if ml >= 1000 {
		return fmt.Sprintf("%.1f L", float64(ml)/1000)
	}
	return fmt.Sprintf("%d ml", ml)
}

func formatMinutes(minutes int) string {
	h, m := minutes/60, minutes%60
	if m == 0 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh %dm", h, m)
}
