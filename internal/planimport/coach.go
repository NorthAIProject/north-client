package planimport

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/FACorreiaa/go-utils/pkg/util"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/meals"
	"github.com/NorthAIProject/north-client/internal/meals/meal"
	"github.com/NorthAIProject/north-client/internal/quota"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	"github.com/NorthAIProject/north-client/internal/users"
	"github.com/NorthAIProject/north-client/internal/workouts"
)

// What a coach import reads a file as.
const (
	CoachImportMeal    = "meal"
	CoachImportWorkout = "workout"
)

// ErrQuotaUsed is ImportForCoach's refusal once the person has spent every
// plan import their window allows. The error's text says when to try again.
var ErrQuotaUsed = errors.New("you have used all your plan imports for now")

// QuotaConsumer spends one request against an action's budget. *quota.Service
// is one; the import pages are guarded by its middleware instead.
type QuotaConsumer interface {
	Consume(ctx context.Context, userID uuid.UUID, tier string, action quota.Action) (quota.Decision, error)
}

// CoachImport is what the coach asks of a file a person attached.
type CoachImport struct {
	// Kind is CoachImportMeal or CoachImportWorkout.
	Kind string
	// PlanType is a meal plan's carb type; empty is mid carb. Custom needs a
	// share the coach has no way to state, so it is refused.
	PlanType string
	// Hint is the person's own words about the file, passed to the reader.
	Hint string
	// Weekdays are the training days a workout plan's unnamed days take, in
	// order. Empty spreads them over the week.
	Weekdays []time.Weekday
}

// CoachResult is what an import saved, and what it left behind.
type CoachResult struct {
	MealPlan *meals.MealPlan
	Workout  *workouts.StoredPlan
	// Skipped lists the food lines left out, as "line: why".
	Skipped []string
	// Over lists the days saved over their target, as "Day: what is over".
	Over     []string
	Unparsed []string
	Notes    string
}

// ImportForCoach reads a file and saves it as a plan in one step, for the
// coach's import tool.
//
// The tool's approval card stands in for the review screen, so the choices
// the screen would have asked for are made here: a meal plan is advanced,
// takes the carb type asked for, fills unnamed days Monday onwards, and is
// saved even where a day runs over its target — the person approved it — with
// those days reported back. A food line that cannot be saved as it stands is
// left out and reported rather than guessed at. A workout's unnamed days take
// the weekdays asked for, or are spread over the week.
//
// The request is checked before anything is spent: an unknown kind or plan
// type, or a meal plan for someone with no macro target, costs no import.
func (s *Service) ImportForCoach(ctx context.Context, user users.User, filename string, data []byte, req CoachImport) (CoachResult, error) {
	if err := s.checkCoachImport(ctx, user.ID, req); err != nil {
		return CoachResult{}, err
	}
	if err := s.spendImport(ctx, user); err != nil {
		return CoachResult{}, err
	}
	if req.Kind == CoachImportWorkout {
		return s.importWorkoutForCoach(ctx, user, filename, data, req)
	}
	return s.importMealForCoach(ctx, user, filename, data, req)
}

func (s *Service) checkCoachImport(ctx context.Context, userID uuid.UUID, req CoachImport) error {
	switch req.Kind {
	case CoachImportWorkout:
		return nil
	case CoachImportMeal:
	default:
		return apperr.FieldErrors{}.Add("kind", fmt.Sprintf("An import is of a %q or a %q plan, not %q.", CoachImportMeal, CoachImportWorkout, req.Kind)).OrNil()
	}

	if t := meal.PlanType(req.PlanType); req.PlanType != "" && (!t.Valid() || t == meal.Custom) {
		types := []string{string(meal.NoCarb), string(meal.LowCarb), string(meal.MidCarb), string(meal.HighCarb)}
		return apperr.FieldErrors{}.Add("plan_type", fmt.Sprintf("%q isn't a carb type an imported plan can use; use one of %s.", req.PlanType, strings.Join(types, ", "))).OrNil()
	}

	target, err := s.mealPlans.ActiveTarget(ctx, userID)
	if err != nil {
		return err
	}
	if target == nil {
		return apperr.FieldErrors{}.Add("macro_target", "Work out your macro target in the calculator first; meal plans are built on it.").OrNil()
	}
	return nil
}

// spendImport counts the import against the person's plan-import budget. With
// no quota wired in, nothing is counted.
func (s *Service) spendImport(ctx context.Context, user users.User) error {
	if s.quota == nil {
		return nil
	}
	decision, err := s.quota.Consume(ctx, user.ID, string(user.Tier), quota.PlanImport)
	if err != nil {
		// Consume fails open by design; an error here is a counter problem,
		// not the person's.
		middleware.FromContext(ctx).Warn("could not check the plan import quota", slog.Any("error", err))
		return nil
	}
	if !decision.Allowed {
		return fmt.Errorf("%w. Try again in %s", ErrQuotaUsed, waitFor(decision.RetryAfter))
	}
	return nil
}

func waitFor(d time.Duration) string {
	switch minutes := int(math.Ceil(d.Minutes())); {
	case minutes <= 1:
		return "a minute"
	case minutes < 60:
		return fmt.Sprintf("%d minutes", minutes)
	default:
		return "about an hour"
	}
}

func (s *Service) importMealForCoach(ctx context.Context, user users.User, filename string, data []byte, req CoachImport) (CoachResult, error) {
	d, err := s.ParseMeal(ctx, user, filename, data, req.Hint)
	if err != nil {
		return CoachResult{}, err
	}

	d.PlanType = req.PlanType
	if d.PlanType == "" {
		d.PlanType = string(meal.MidCarb)
	}
	d.Mode = string(meal.Advanced)

	res := CoachResult{Unparsed: d.Unparsed, Notes: d.Notes, Skipped: skipUnready(&d)}
	if len(d.Days) == 0 {
		return CoachResult{}, apperr.FieldErrors{}.Add("file", fmt.Sprintf("Nothing in %s could be matched to foods.", filepath.Base(filename))).OrNil()
	}
	// An every-day plan's one day is saved as all seven already.
	if !d.EveryDay {
		fillMealWeekdays(&d)
	}

	saved, previewed, err := s.CommitMeal(ctx, user, d, true)
	if err != nil {
		return CoachResult{}, err
	}
	res.MealPlan = &saved
	res.Over = overLines(previewed)
	return res, nil
}

func (s *Service) importWorkoutForCoach(ctx context.Context, user users.User, filename string, data []byte, req CoachImport) (CoachResult, error) {
	d, err := s.ParseWorkout(ctx, user, filename, data, req.Hint)
	if err != nil {
		return CoachResult{}, err
	}
	fillWorkoutWeekdays(&d, req.Weekdays)

	stored, err := s.CommitWorkout(ctx, user, d)
	if err != nil {
		return CoachResult{}, err
	}
	return CoachResult{Workout: &stored, Unparsed: d.Unparsed}, nil
}

// skipUnready takes every food line that cannot be saved as it stands out of
// d, and says what each one was and why.
//
// It removes them the way the review page's delete does: an option left with
// no food goes and the next takes its place, keeping its label, and a meal or
// a day left with nothing goes too. Done on the draft rather than left to the
// save, so the next preview counts the option that is actually first.
func skipUnready(d *MealDraft) []string {
	var skipped []string
	for di := range d.Days {
		for mi := range d.Days[di].Meals {
			m := &d.Days[di].Meals[mi]
			for o := 0; o <= len(m.Alternatives); o++ {
				foods := optionFoods(m, o)
				*foods = slices.DeleteFunc(*foods, func(f FoodDraft) bool {
					if f.Ready() {
						return false
					}
					skipped = append(skipped, skippedLine(f))
					return true
				})
			}
			// From the last option back, so a removal never moves an
			// option still to be looked at.
			for o := len(m.Alternatives); o >= 0; o-- {
				if len(*optionFoods(m, o)) == 0 {
					deleteOption(m, o)
				}
			}
		}
	}
	dropEmpty(d)
	return skipped
}

func skippedLine(f FoodDraft) string {
	line := strings.TrimSpace(f.SourceText)
	if line == "" {
		line = strings.TrimSpace(f.Food)
	}
	why := strings.Join(f.Checks, " ")
	if why == "" {
		why = "It couldn't be matched to a food."
	}
	return line + ": " + why
}

// fillMealWeekdays gives each day without a weekday the first one free,
// Monday onwards.
func fillMealWeekdays(d *MealDraft) {
	var taken []time.Weekday
	missing := 0
	for _, day := range d.Days {
		if day.Weekday == nil {
			missing++
			continue
		}
		taken = append(taken, time.Weekday(*day.Weekday))
	}
	free := freeWeekdays(missing, taken, nil)
	for i := range d.Days {
		if d.Days[i].Weekday == nil && len(free) > 0 {
			d.Days[i].Weekday = util.Ptr(int(free[0]))
			free = free[1:]
		}
	}
}

// fillWorkoutWeekdays gives each day without a weekday one of prefer, or,
// with no preference, a day spread over the week for the plan's length.
func fillWorkoutWeekdays(d *WorkoutDraft, prefer []time.Weekday) {
	var taken []time.Weekday
	missing := 0
	for _, day := range d.Days {
		if strings.TrimSpace(day.Weekday) == "" {
			missing++
			continue
		}
		if wd, ok := weekdayOf(day.Weekday); ok {
			taken = append(taken, wd)
		}
	}
	if len(prefer) == 0 {
		prefer = spreadWeekdays(len(d.Days))
	}
	free := freeWeekdays(missing, taken, prefer)
	for i := range d.Days {
		if strings.TrimSpace(d.Days[i].Weekday) == "" && len(free) > 0 {
			d.Days[i].Weekday = free[0].String()
			free = free[1:]
		}
	}
}

// freeWeekdays picks up to n weekdays not in taken: prefer's first, in their
// order, then the rest of the week Monday onwards. Fewer than n come back
// when the week runs out.
func freeWeekdays(n int, taken, prefer []time.Weekday) []time.Weekday {
	used := make(map[time.Weekday]bool, len(taken))
	for _, d := range taken {
		used[d] = true
	}
	var out []time.Weekday
	for _, d := range slices.Concat(prefer, meal.WeekOrder) {
		if len(out) == n {
			break
		}
		if d < time.Sunday || d > time.Saturday || used[d] {
			continue
		}
		used[d] = true
		out = append(out, d)
	}
	return out
}

// spreadWeekdays spaces n training days over the week so rest falls between
// them where it can. Nil for a plan longer than a week.
func spreadWeekdays(n int) []time.Weekday {
	switch n {
	case 1:
		return []time.Weekday{time.Monday}
	case 2:
		return []time.Weekday{time.Monday, time.Thursday}
	case 3:
		return []time.Weekday{time.Monday, time.Wednesday, time.Friday}
	case 4:
		return []time.Weekday{time.Monday, time.Tuesday, time.Thursday, time.Friday}
	case 5, 6, 7:
		return slices.Clone(meal.WeekOrder[:n])
	}
	return nil
}

// overLines is each day's overage, named by the day it is saved on.
func overLines(d MealDraft) []string {
	var out []string
	for i, day := range d.Days {
		name := day.Label
		switch {
		case !d.EveryDay && day.Weekday != nil:
			name = time.Weekday(*day.Weekday).String()
		case name == "":
			name = fmt.Sprintf("Day %d", i+1)
		}
		for _, over := range day.Over {
			out = append(out, name+": "+over)
		}
	}
	return out
}
