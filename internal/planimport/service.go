package planimport

import (
	"context"
	"strconv"
	"strings"
	"sync"

	"github.com/FACorreiaa/go-utils/pkg/util"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/meals"
	"github.com/NorthAIProject/north-client/internal/meals/meal"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
	"github.com/NorthAIProject/north-client/internal/workouts"
	"github.com/NorthAIProject/north-client/internal/workouts/plan"
)

// Service reads plans out of files and saves the ones a person confirms.
type Service struct {
	reader      Reader
	workouts    *workouts.Service
	mealPlans   *meals.MealPlanService
	ingredients *meals.IngredientService
	quota       QuotaConsumer

	// inFlight holds the users with a parse running. One import at a time per
	// person: a second upload while the first is still being read is almost
	// always a double tap, and two drafts racing to the same review screen
	// help nobody. Per process — with more than one web replica, two parses
	// could still overlap on different pods, which costs a model call and
	// nothing else.
	inFlight sync.Map
}

// Options wires the service.
type Options struct {
	Reader      Reader
	Workouts    *workouts.Service
	MealPlans   *meals.MealPlanService
	Ingredients *meals.IngredientService
	// Quota is spent by ImportForCoach, one plan import per call. The import
	// pages spend theirs in their route middleware, so they never reach it.
	// Nil counts nothing.
	Quota QuotaConsumer
}

func NewService(opts Options) *Service {
	return &Service{
		reader:      opts.Reader,
		workouts:    opts.Workouts,
		mealPlans:   opts.MealPlans,
		ingredients: opts.Ingredients,
		quota:       opts.Quota,
	}
}

func (s *Service) begin(userID uuid.UUID) error {
	if _, running := s.inFlight.LoadOrStore(userID, struct{}{}); running {
		return refuse(ReasonBusy, "An import is already running. Wait for it to finish.")
	}
	return nil
}

func (s *Service) end(userID uuid.UUID) { s.inFlight.Delete(userID) }

// ParseWorkout reads a workout plan out of a file. It writes nothing.
//
// hint is the person's own request about the file, passed to the reader for a
// document or a photo; a spreadsheet or JSON is read whole. Empty from the
// import pages.
func (s *Service) ParseWorkout(ctx context.Context, user users.User, filename string, data []byte, hint string) (WorkoutDraft, error) {
	if err := s.begin(user.ID); err != nil {
		return WorkoutDraft{}, err
	}
	defer s.end(user.ID)

	src, err := Open(filename, data)
	if err != nil {
		return WorkoutDraft{}, err
	}

	switch src.Kind {
	case KindCSV, KindTSV, KindXLSX:
		return WorkoutFromRows(src.Filename, src.Rows)
	case KindJSON:
		return WorkoutFromJSON(src.Filename, src.JSON)
	}

	name, rows, unparsed, err := s.reader.ReadWorkout(ctx, user, src, hint)
	if err != nil {
		return WorkoutDraft{}, err
	}
	return buildWorkout(nameOr(name, src.Filename), rows, unparsed)
}

// ParseMeal reads a meal plan out of a file and previews it against the
// person's target. It writes nothing. hint is as for ParseWorkout.
func (s *Service) ParseMeal(ctx context.Context, user users.User, filename string, data []byte, hint string) (MealDraft, error) {
	if err := s.begin(user.ID); err != nil {
		return MealDraft{}, err
	}
	defer s.end(user.ID)

	src, err := Open(filename, data)
	if err != nil {
		return MealDraft{}, err
	}

	var draft MealDraft
	switch src.Kind {
	case KindCSV, KindTSV, KindXLSX:
		draft, err = MealFromRows(src.Filename, src.Rows)
	case KindJSON:
		draft, err = MealFromJSON(src.Filename, src.JSON)
	default:
		var reading MealReading
		reading, err = s.reader.ReadMeal(ctx, user, src, hint)
		if err == nil {
			reading.Name = nameOr(reading.Name, src.Filename)
			draft, err = buildMeal(reading)
		}
	}
	if err != nil {
		return MealDraft{}, err
	}
	return s.PreviewMeal(ctx, user.ID, draft), nil
}

// CommitWorkout saves a reviewed workout draft as a plan.
func (s *Service) CommitWorkout(ctx context.Context, user users.User, d WorkoutDraft) (workouts.StoredPlan, error) {
	p, problems := workoutPlanFromDraft(d)
	if len(problems) > 0 {
		return workouts.StoredPlan{}, apperr.FieldErrors{{Field: "plan", Message: strings.Join(problems, " ")}}
	}
	return s.workouts.ImportPlan(ctx, user, p)
}

// CommitMeal saves a reviewed meal draft as a plan.
//
// The draft is previewed again first, so what is saved is what the server
// computed, never a total the client sent. The previewed draft is returned on
// every path, so a refused commit can re-render the review with the reason
// beside the line or day it is about.
//
// A day over its target is refused by meals.CreatePlan exactly as any other
// change to a meal plan is: always on an easy plan, until confirmed on an
// advanced one.
func (s *Service) CommitMeal(ctx context.Context, user users.User, d MealDraft, confirmOverage bool) (meals.MealPlan, MealDraft, error) {
	d = s.PreviewMeal(ctx, user.ID, d)

	plan, problems := mealPlanFromDraft(d)
	if len(problems) > 0 {
		return meals.MealPlan{}, d, apperr.FieldErrors{{Field: "plan", Message: strings.Join(problems, " ")}}
	}

	// Checked here as well as in CreatePlan, and before any personal
	// ingredient is created: otherwise a refusal followed by "confirm and
	// save" would leave a second copy of each one behind.
	if len(plan.mine) > 0 {
		active, err := s.mealPlans.ActiveTarget(ctx, user.ID)
		if err != nil {
			return meals.MealPlan{}, d, err
		}
		settings, _ := planSettings(d)
		state, _ := planState(d, settings)
		if v := meal.CheckWrite(*active, nil, state, confirmOverage); !v.Allowed {
			return meals.MealPlan{}, d, &meals.OverageError{Verdict: v}
		}
	}
	for _, m := range plan.mine {
		created, err := s.ingredients.Create(ctx, user.ID, m.ingredient)
		if err != nil {
			return meals.MealPlan{}, d, err
		}
		plan.setIngredient(m, created.ID)
	}

	saved, err := s.mealPlans.CreatePlan(ctx, user.ID, plan.input, plan.daysToSave(), confirmOverage)
	return saved, d, err
}

// workoutPlanFromDraft converts a reviewed draft into a stored plan's shape.
//
// Unstated numbers become zero, which the plan type reads as "not stated" for
// imported plans. A day label that is not itself a weekday becomes the day's
// focus, so "Push A" stays visible on the plan page.
func workoutPlanFromDraft(d WorkoutDraft) (plan.Plan, []string) {
	var problems []string
	out := plan.Plan{Name: strings.TrimSpace(d.Name)}

	for i, day := range d.Days {
		if strings.TrimSpace(day.Weekday) == "" {
			label := day.Label
			if label == "" {
				label = "training day " + strconv.Itoa(i+1)
			}
			problems = append(problems, "Choose which day of the week "+label+" is.")
		}
		pd := plan.PlanDay{Weekday: day.Weekday}
		if _, isWeekday := weekdayOf(day.Label); !isWeekday {
			pd.Focus = strings.TrimSpace(day.Label)
		}
		for _, ex := range day.Exercises {
			e := plan.Exercise{
				Name:     strings.TrimSpace(ex.Name),
				Reps:     strings.TrimSpace(ex.Reps),
				Load:     strings.TrimSpace(ex.Load),
				FormCues: strings.TrimSpace(ex.Notes),
			}
			e.Sets = util.Val(ex.Sets)
			e.RestSeconds = util.Val(ex.RestSeconds)
			pd.Exercises = append(pd.Exercises, e)
		}
		out.Days = append(out.Days, pd)
	}
	return out, problems
}
