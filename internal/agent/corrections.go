package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/users"
)

// Taking back something logged by mistake.
//
// "Undo that", "I didn't actually have the second coffee", "that set was 80,
// not 100" — said in a chat, each needs an entry removed. One tool covers every
// log rather than one undo tool per tracker: the model picks a kind, and what
// differs between trackers stays in the small adapters below.
//
// No ids reach the model, the same rule the plan edits follow. An entry is
// named by kind and, when that is not enough, by part of its description. With
// several matches the error lists today's entries, which is also how the model
// sees what there is to undo without a separate read tool.
//
// Only today. A correction is almost always about the thing just logged, and
// reaching further back by conversation is how the wrong week gets edited.

// loggedEntry is one thing logged today, as the undo tool sees it.
type loggedEntry struct {
	label string
	at    time.Time
	undo  func(context.Context) error
}

// undoSource lists today's entries of one kind.
type undoSource func(ctx context.Context, user users.User) ([]loggedEntry, error)

// undoSources is every kind the wired services can take back, keyed by the
// name the model passes.
func undoSources(s Services) map[string]undoSource {
	sources := map[string]undoSource{}

	if s.Hydration != nil {
		sources["water"] = func(ctx context.Context, user users.User) ([]loggedEntry, error) {
			entries, err := s.Hydration.TodayEntries(ctx, user)
			if err != nil {
				return nil, err
			}
			out := make([]loggedEntry, 0, len(entries))
			for _, e := range entries {
				id := e.ID
				out = append(out, loggedEntry{
					label: fmt.Sprintf("%d ml water", e.AmountML),
					at:    e.LoggedAt,
					undo:  func(ctx context.Context) error { return s.Hydration.Undo(ctx, user, id) },
				})
			}
			return out, nil
		}
	}

	if s.Caffeine != nil {
		sources["caffeine"] = func(ctx context.Context, user users.User) ([]loggedEntry, error) {
			entries, err := s.Caffeine.Today(ctx, user)
			if err != nil {
				return nil, err
			}
			out := make([]loggedEntry, 0, len(entries))
			for _, e := range entries {
				id := e.ID
				out = append(out, loggedEntry{
					label: fmt.Sprintf("%s (%d mg caffeine)", e.Label, e.MG),
					at:    e.LoggedAt,
					undo:  func(ctx context.Context) error { return s.Caffeine.Undo(ctx, user, id) },
				})
			}
			return out, nil
		}
	}

	if s.Supplements != nil {
		sources["supplement"] = func(ctx context.Context, user users.User) ([]loggedEntry, error) {
			entries, err := s.Supplements.Today(ctx, user)
			if err != nil {
				return nil, err
			}
			out := make([]loggedEntry, 0, len(entries))
			for _, e := range entries {
				id := e.ID
				out = append(out, loggedEntry{
					label: e.Label(),
					at:    e.LoggedAt,
					undo:  func(ctx context.Context) error { return s.Supplements.Undo(ctx, user, id) },
				})
			}
			return out, nil
		}
	}

	if s.FoodLog != nil {
		sources["food"] = func(ctx context.Context, user users.User) ([]loggedEntry, error) {
			entries, err := s.FoodLog.Day(ctx, user.ID, time.Now())
			if err != nil {
				return nil, err
			}
			out := make([]loggedEntry, 0, len(entries))
			for _, e := range entries {
				id := e.ID
				label := e.Label
				if e.QuantityGrams != nil {
					label = fmt.Sprintf("%s %.0f g", e.Label, *e.QuantityGrams)
				}
				out = append(out, loggedEntry{
					label: label,
					at:    e.LoggedAt,
					undo:  func(ctx context.Context) error { return s.FoodLog.Delete(ctx, id, user.ID) },
				})
			}
			return out, nil
		}
	}

	if s.Lifts != nil {
		sources["lift_set"] = func(ctx context.Context, user users.User) ([]loggedEntry, error) {
			sets, _, err := s.Lifts.Today(ctx, user)
			if err != nil {
				return nil, err
			}
			out := make([]loggedEntry, 0, len(sets))
			for _, set := range sets {
				id := set.ID
				out = append(out, loggedEntry{
					label: fmt.Sprintf("%s set %d: %g kg x %d", set.ExerciseName, set.SetNumber, set.WeightKg, set.Reps),
					at:    set.PerformedAt,
					undo:  func(ctx context.Context) error { return s.Lifts.Undo(ctx, user, id) },
				})
			}
			return out, nil
		}
	}

	if s.Fasting != nil {
		sources["fast"] = func(ctx context.Context, user users.User) ([]loggedEntry, error) {
			now := time.Now().In(user.Location())
			fasts, err := s.Fasting.Overlapping(ctx, user, timerange.Range{Since: timerange.StartOfDay(now), Until: now})
			if err != nil {
				return nil, err
			}
			out := make([]loggedEntry, 0, len(fasts))
			for _, f := range fasts {
				id := f.ID
				label := "fast started " + f.StartedAt.In(user.Location()).Format("Mon 15:04")
				if f.Open() {
					label += " (still going)"
				}
				out = append(out, loggedEntry{
					label: label,
					at:    f.StartedAt,
					undo:  func(ctx context.Context) error { return s.Fasting.Delete(ctx, user, id) },
				})
			}
			return out, nil
		}
	}

	if s.Habits != nil {
		sources["habit"] = func(ctx context.Context, user users.User) ([]loggedEntry, error) {
			stats, err := s.Habits.Today(ctx, user)
			if err != nil {
				return nil, err
			}
			var out []loggedEntry
			for _, st := range stats {
				if !st.DoneToday {
					continue
				}
				id := st.Habit.ID
				out = append(out, loggedEntry{
					// A tick has no time of its own, so it sorts as the oldest
					// and is never the one "undo that" picks by default.
					label: st.Habit.Name + " ticked off",
					undo:  func(ctx context.Context) error { return s.Habits.Uncomplete(ctx, user, id) },
				})
			}
			return out, nil
		}
	}

	if s.Medications != nil {
		sources["medication"] = func(ctx context.Context, user users.User) ([]loggedEntry, error) {
			doses, err := s.Medications.TodayDoses(ctx, user)
			if err != nil {
				return nil, err
			}
			out := make([]loggedEntry, 0, len(doses))
			for _, d := range doses {
				id := d.ID
				label := fmt.Sprintf("%s %s", d.Label(), d.Status)
				if d.Slot != nil {
					label += " for " + *d.Slot
				}
				out = append(out, loggedEntry{
					label: label,
					at:    d.LoggedAt,
					undo:  func(ctx context.Context) error { return s.Medications.UndoDose(ctx, user, id) },
				})
			}
			return out, nil
		}
	}

	return sources
}

// pickEntry chooses which of today's entries to take back: the newest when
// nothing narrows it, otherwise the single entry whose description contains
// match. Several matches is an error listing them, never a guess.
func pickEntry(entries []loggedEntry, kind, match string, loc *time.Location) (loggedEntry, error) {
	if len(entries) == 0 {
		return loggedEntry{}, fmt.Errorf("nothing of kind %s is logged today", kind)
	}

	sorted := append([]loggedEntry(nil), entries...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].at.After(sorted[j].at) })

	needle := strings.TrimSpace(strings.ToLower(match))
	if needle == "" {
		return sorted[0], nil
	}

	var hits []loggedEntry
	for _, e := range sorted {
		if strings.Contains(strings.ToLower(e.label), needle) {
			hits = append(hits, e)
		}
	}
	switch len(hits) {
	case 1:
		return hits[0], nil
	case 0:
		return loggedEntry{}, fmt.Errorf("nothing logged today matches %q; today's %s entries are: %s", match, kind, describeEntries(sorted, loc))
	default:
		return loggedEntry{}, fmt.Errorf("%q matches several entries (%s); be more specific", match, describeEntries(hits, loc))
	}
}

func describeEntries(entries []loggedEntry, loc *time.Location) string {
	parts := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.at.IsZero() {
			parts = append(parts, e.label)
			continue
		}
		parts = append(parts, fmt.Sprintf("%s at %s", e.label, e.at.In(loc).Format("15:04")))
	}
	return strings.Join(parts, "; ")
}

func undoLog(sources map[string]undoSource, userSvc *users.Service) Capability {
	type args struct {
		Kind  string `json:"kind"`
		Match string `json:"match"`
	}

	kinds := make([]string, 0, len(sources))
	for kind := range sources {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)

	return Capability{
		Tool: ai.Tool{
			Name: "undo_log",
			Description: "Take back something logged today by mistake: water, food, a supplement, caffeine, a lifting set, a fast, a habit tick or a medication dose. " +
				"With no match it removes the most recent entry of that kind. To correct an amount, undo the wrong entry and log the right one. " +
				"If several entries match, the error lists today's entries so you can be specific.",
			Parameters: ai.Object("what to take back", map[string]*ai.Schema{
				"kind":  ai.Enum("what kind of log", kinds...),
				"match": ai.String("part of the entry's description, such as 'espresso' or '500'; an empty string for the most recent"),
			}, "kind", "match"),
		},
		Invoke: func(ctx context.Context, userID uuid.UUID, raw json.RawMessage) (string, error) {
			in, err := Decode[args](raw)
			if err != nil {
				return "", err
			}
			source, ok := sources[in.Kind]
			if !ok {
				return "", fmt.Errorf("cannot undo %q; the kinds are %s", in.Kind, strings.Join(kinds, ", "))
			}

			user, err := userSvc.ByID(ctx, userID)
			if err != nil {
				return "", err
			}
			entries, err := source(ctx, user)
			if err != nil {
				return "", err
			}
			entry, err := pickEntry(entries, in.Kind, in.Match, user.Location())
			if err != nil {
				return "", err
			}
			if err := entry.undo(ctx); err != nil {
				return "", err
			}
			return "Removed " + entry.label + ".", nil
		},
	}
}
