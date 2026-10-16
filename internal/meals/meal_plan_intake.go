package meals

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	mealsdb "github.com/NorthAIProject/north-client/internal/meals/db"
	"github.com/NorthAIProject/north-client/internal/meals/meal"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// Limits on what a person can ask a meal plan to be built around.
const (
	MaxMealsPerDay      = 8
	MaxExclusions       = 30
	MaxExclusionRunes   = 60
	MaxIntakeNotesRunes = 500
)

// Objectives a generated meal plan can be built for, matching the calculator's
// goals.
var Objectives = []string{"cutting", "maintenance", "bulking"}

// MealPlanIntake is what a person told the coach when asking it to build a
// meal plan: the counterpart of a workout intake.
//
// The macros are not here. A generated plan is held to the calculator's
// target like every other plan, so there is one definition of "fits".
type MealPlanIntake struct {
	ID          uuid.UUID
	Objective   string
	Days        int
	MealsPerDay int
	PlanType    meal.PlanType
	// Exclusions are foods the person can't or won't eat, in their words:
	// "dairy", "peanuts", "mushrooms".
	Exclusions []string
	// Notes is anything else they want the plan to respect: "cheap",
	// "quick breakfasts", "I train at 7pm".
	Notes     string
	CreatedAt time.Time
}

// ValidateMealPlanIntake checks an intake and returns it cleaned: trimmed,
// with blank and repeated exclusions dropped.
//
// Custom carb shares are not offered. They are an advanced-mode choice the
// review screen still allows once the plan is drafted.
func ValidateMealPlanIntake(in MealPlanIntake) (MealPlanIntake, error) {
	var errs apperr.FieldErrors

	in.Objective = strings.TrimSpace(in.Objective)
	if !slices.Contains(Objectives, in.Objective) {
		errs = errs.Add("objective", "Choose cutting, maintenance or bulking.")
	}
	if in.Days < 1 || in.Days > meal.MaxDays {
		errs = errs.Add("days", "Choose between 1 and 7 days.")
	}
	if in.MealsPerDay < 1 || in.MealsPerDay > MaxMealsPerDay {
		errs = errs.Add("meals_per_day", "Choose between 1 and 8 meals a day.")
	}
	if _, ok := meal.BandFor(in.PlanType); !ok {
		errs = errs.Add("plan_type", "Choose no, low, mid or high carb.")
	}

	in.Exclusions = cleanExclusions(in.Exclusions)
	if len(in.Exclusions) > MaxExclusions {
		errs = errs.Add("exclusions", "List at most 30 foods to leave out.")
	}
	for _, e := range in.Exclusions {
		if utf8.RuneCountInString(e) > MaxExclusionRunes {
			errs = errs.Add("exclusions", "Keep each food to leave out under 60 characters.")
			break
		}
	}

	in.Notes = strings.TrimSpace(in.Notes)
	if utf8.RuneCountInString(in.Notes) > MaxIntakeNotesRunes {
		errs = errs.Add("notes", "Keep this under 500 characters.")
	}

	return in, errs.OrNil()
}

// ParseExclusions splits a typed list — "dairy, peanuts; mushrooms" or one
// per line — into foods.
func ParseExclusions(s string) []string {
	return cleanExclusions(strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n'
	}))
}

// cleanExclusions trims, drops blanks, and keeps the first of any repeated
// food, ignoring case.
func cleanExclusions(in []string) []string {
	out := []string{}
	for _, e := range in {
		e = strings.TrimSpace(e)
		if e == "" || slices.ContainsFunc(out, func(seen string) bool { return strings.EqualFold(seen, e) }) {
			continue
		}
		out = append(out, e)
	}
	return out
}

// SaveIntake validates and stores what the person asked for.
func (s *MealPlanService) SaveIntake(ctx context.Context, userID uuid.UUID, in MealPlanIntake) (MealPlanIntake, error) {
	clean, err := ValidateMealPlanIntake(in)
	if err != nil {
		return MealPlanIntake{}, err
	}
	return s.repo.CreateMealPlanIntake(ctx, userID, clean)
}

// LatestIntake is the person's most recent answers, or nil when they have
// never asked for a plan to be built.
func (s *MealPlanService) LatestIntake(ctx context.Context, userID uuid.UUID) (*MealPlanIntake, error) {
	return s.repo.LatestMealPlanIntake(ctx, userID)
}

func (r *Repository) CreateMealPlanIntake(ctx context.Context, userID uuid.UUID, in MealPlanIntake) (MealPlanIntake, error) {
	row, err := r.q.CreateMealPlanIntake(ctx, mealsdb.CreateMealPlanIntakeParams{
		UserID:      userID,
		Objective:   in.Objective,
		Days:        int16(in.Days),
		MealsPerDay: int16(in.MealsPerDay),
		PlanType:    string(in.PlanType),
		Exclusions:  in.Exclusions,
		Notes:       in.Notes,
	})
	if err != nil {
		return MealPlanIntake{}, apperr.Wrap(err, "create meal plan intake")
	}
	return intakeFromDB(row), nil
}

func (r *Repository) LatestMealPlanIntake(ctx context.Context, userID uuid.UUID) (*MealPlanIntake, error) {
	row, err := r.q.LatestMealPlanIntake(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, apperr.Wrap(err, "latest meal plan intake")
	}
	in := intakeFromDB(row)
	return &in, nil
}

func intakeFromDB(row mealsdb.MealPlanIntake) MealPlanIntake {
	return MealPlanIntake{
		ID:          row.ID,
		Objective:   row.Objective,
		Days:        int(row.Days),
		MealsPerDay: int(row.MealsPerDay),
		PlanType:    meal.PlanType(row.PlanType),
		Exclusions:  row.Exclusions,
		Notes:       row.Notes,
		CreatedAt:   row.CreatedAt,
	}
}

// CatalogFood is a shared catalog ingredient with its macros per 100 g.
type CatalogFood struct {
	Name    string
	Per100g Macros
}

// SharedCatalog lists the shared ingredients with their macros, A to Z.
func (s *IngredientService) SharedCatalog(ctx context.Context) ([]CatalogFood, error) {
	return s.repo.SharedCatalog(ctx)
}

func (r *Repository) SharedCatalog(ctx context.Context) ([]CatalogFood, error) {
	rows, err := r.q.ListSharedIngredientMacros(ctx)
	if err != nil {
		return nil, apperr.Wrap(err, "list shared ingredient macros")
	}
	out := make([]CatalogFood, len(rows))
	for i, row := range rows {
		out[i] = CatalogFood{Name: row.Name, Per100g: meal.MacrosFromGrams(row.ProteinGPer100g, row.FatGPer100g, row.CarbsGPer100g)}
	}
	return out, nil
}
