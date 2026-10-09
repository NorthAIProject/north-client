package meals

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/meals/meal"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// MealPlanService owns meal plans and holds every change to them to the
// person's current macro target (see meal/plan_rules.go for the rules).
type MealPlanService struct {
	repo  *Repository
	goals MacroGoalLookup
}

func NewMealPlanService(repo *Repository, goals MacroGoalLookup) *MealPlanService {
	return &MealPlanService{repo: repo, goals: goals}
}

// ActiveTarget is the person's current macro target, exactly as the
// calculator produced it; nil when it has not produced one yet.
func (s *MealPlanService) ActiveTarget(ctx context.Context, userID uuid.UUID) (*Macros, error) {
	goal, err := s.goals.Current(ctx, userID)
	if apperr.Is(err, apperr.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, apperr.Wrap(err, "load macro target")
	}
	return &Macros{Calories: goal.CalorieGoal, ProteinG: goal.ProteinG, FatG: goal.FatG, CarbG: goal.CarbG}, nil
}

// requireTarget is ActiveTarget for a change: with no target there are no
// limits to hold a plan to, so the change is refused rather than let through
// unchecked.
func (s *MealPlanService) requireTarget(ctx context.Context, userID uuid.UUID) (Macros, error) {
	target, err := s.ActiveTarget(ctx, userID)
	if err != nil {
		return Macros{}, err
	}
	if target == nil {
		return Macros{}, apperr.FieldErrors{}.Add("macro_target",
			"Work out your macro target in the calculator first; meal plans are built on it.").OrNil()
	}
	return *target, nil
}

type MealPlanInput struct {
	Name          string
	Description   string
	Objective     string
	ActivityLevel string
	Gender        string
	Settings      meal.PlanSettings
	// DayCount fills the plan with that many days from Monday. Weekdays,
	// advanced plans only, names the days instead.
	DayCount int
	Weekdays []time.Weekday
	// Notes is free text an imported plan carried beside its meals.
	Notes string
}

// MaxPlanNotesRunes caps a plan's notes: room for a nutritionist's page of
// advice, not a whole document.
const MaxPlanNotesRunes = 8000

func ValidateMealPlan(in MealPlanInput) (MealPlanInput, error) {
	var errs apperr.FieldErrors

	in.Name, errs = validatePlanName(in.Name, errs)
	in.Settings, errs = validateSettings(in.Settings, errs)
	in.Notes = strings.TrimSpace(in.Notes)
	if utf8.RuneCountInString(in.Notes) > MaxPlanNotesRunes {
		errs = errs.Add("notes", fmt.Sprintf("Keep the notes to %d characters.", MaxPlanNotesRunes))
	}

	switch {
	case len(in.Weekdays) > 0 && in.Settings.Mode != meal.Advanced:
		errs = errs.Add("weekdays", "Easy plans take a number of days; choose weekdays in advanced mode.")
	case len(in.Weekdays) > 0:
		seen := map[time.Weekday]bool{}
		for _, wd := range in.Weekdays {
			if wd < time.Sunday || wd > time.Saturday || seen[wd] {
				errs = errs.Add("weekdays", "Choose each weekday at most once.")
				break
			}
			seen[wd] = true
		}
	case in.Settings.Mode == meal.Advanced && in.DayCount == 0:
		errs = errs.Add("weekdays", "Choose at least one weekday.")
	case in.DayCount < 1 || in.DayCount > meal.MaxDays:
		errs = errs.Add("day_count", fmt.Sprintf("Choose between 1 and %d days.", meal.MaxDays))
	default:
		in.Weekdays = meal.EasyWeekdays(in.DayCount)
	}

	return in, errs.OrNil()
}

func validatePlanName(name string, errs apperr.FieldErrors) (string, apperr.FieldErrors) {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		errs = errs.Add("name", "Give the plan a name.")
	case len(name) > 200:
		errs = errs.Add("name", "Keep the name under 200 characters.")
	}
	return name, errs
}

func validateSettings(s meal.PlanSettings, errs apperr.FieldErrors) (meal.PlanSettings, apperr.FieldErrors) {
	if !s.Mode.Valid() {
		errs = errs.Add("mode", "Choose easy or advanced.")
	}
	switch {
	case !s.Type.Valid():
		errs = errs.Add("plan_type", "Choose a plan type.")
	case s.Type != meal.Custom:
		s.CustomCarbPct = nil
	case s.Mode != meal.Advanced:
		errs = errs.Add("plan_type", "A custom carb share is an advanced-mode choice.")
	case s.CustomCarbPct == nil || *s.CustomCarbPct < 0 || *s.CustomCarbPct > 100:
		errs = errs.Add("custom_carb_pct", "Enter a share of your carb target between 0 and 100%.")
	}
	return s, errs
}

// ValidateDayOverride checks an advanced day's override against the active
// target, which no override may exceed.
func ValidateDayOverride(o meal.DayOverride, active Macros) error {
	var errs apperr.FieldErrors
	if o.CarbType != nil {
		if _, ok := meal.BandFor(*o.CarbType); !ok {
			errs = errs.Add("carb_type", "Choose no, low, mid or high carb.")
		}
		if o.CarbG != nil {
			errs = errs.Add("carb_g", "Set the day's carbs by type or by grams, not both.")
		}
	}
	errs = checkGrams(errs, "carb_g", "carbs", o.CarbG, active.CarbG)
	errs = checkGrams(errs, "protein_g", "protein", o.ProteinG, active.ProteinG)
	errs = checkGrams(errs, "fat_g", "fat", o.FatG, active.FatG)
	return errs.OrNil()
}

func checkGrams(errs apperr.FieldErrors, field, name string, grams *float64, limit float64) apperr.FieldErrors {
	if grams != nil && (*grams < 0 || *grams > limit) {
		return errs.Add(field, fmt.Sprintf("Keep %s between 0 and your target of %.0f g.", name, limit))
	}
	return errs
}

type MealIngredientInput struct {
	IngredientID  uuid.UUID
	QuantityGrams float64
	// SourceText is the line an imported plan had for this food; Estimated
	// marks a food and quantity the importer guessed at. See MealIngredient.
	SourceText string
	Estimated  bool
}

func ValidateMealIngredient(in MealIngredientInput) (MealIngredientInput, error) {
	var errs apperr.FieldErrors

	if in.IngredientID == uuid.Nil {
		errs = errs.Add("ingredient_id", "Choose an ingredient.")
	}
	if in.QuantityGrams <= 0 {
		errs = errs.Add("quantity_grams", "Enter a quantity greater than zero.")
	}

	return in, errs.OrNil()
}

// DayDraft is the meals to create a plan's day with.
type DayDraft struct {
	Meals []MealDraft
}

// MealDraft is a meal slot to create: its default option's portions, and the
// slot's other options. Only the default counts toward the day's target.
type MealDraft struct {
	Name         string
	OptionLabel  string
	Portions     []MealIngredientInput
	Alternatives []MealOptionDraft
}

// MealOptionDraft is a further option of a drafted meal slot, sharing its name.
type MealOptionDraft struct {
	Label    string
	Portions []MealIngredientInput
}

// OverageError refuses a change that would take a day further over its
// target. The verdict says whether the person may confirm it, and by how much
// each day would be over.
type OverageError struct {
	// PlanID is the plan the change was to; nil for a plan being created.
	PlanID  uuid.UUID
	Verdict meal.Verdict
}

func (e *OverageError) Error() string {
	days := make([]string, 0, len(e.Verdict.Over))
	for _, d := range e.Verdict.Over {
		var parts []string
		for _, m := range []struct {
			name string
			over float64
		}{{"protein", d.Status.Over.ProteinG}, {"carbs", d.Status.Over.CarbG}, {"fat", d.Status.Over.FatG}} {
			if m.over >= 0.5 {
				parts = append(parts, fmt.Sprintf("%.0f g over on %s", m.over, m.name))
			}
		}
		days = append(days, fmt.Sprintf("%s would be %s", d.Weekday, strings.Join(parts, " and ")))
	}
	return strings.Join(days, "; ") + "."
}

func (e *OverageError) Unwrap() error { return apperr.ErrConflict }

// checkOverage applies the overage rule to a change to plan whose result is
// after. fresh compares against nothing, for a plan switching to easy.
func checkOverage(active Macros, plan MealPlan, after meal.PlanState, fresh, confirm bool) error {
	var before *meal.PlanState
	if !fresh {
		state := plan.State()
		before = &state
	}
	v := meal.CheckWrite(active, before, after, confirm)
	if v.Allowed {
		return nil
	}
	return &OverageError{PlanID: plan.ID, Verdict: v}
}

// CreatePlan creates a plan with its days. days, when given, fills the plan's
// days in order with meals; a plan that would start over its target is
// refused like any other change.
func (s *MealPlanService) CreatePlan(ctx context.Context, userID uuid.UUID, in MealPlanInput, days []DayDraft, confirm bool) (MealPlan, error) {
	clean, err := ValidateMealPlan(in)
	if err != nil {
		return MealPlan{}, err
	}
	if len(days) > len(clean.Weekdays) {
		return MealPlan{}, apperr.FieldErrors{}.Add("days", "There are more days of meals than days in the plan.").OrNil()
	}
	active, err := s.requireTarget(ctx, userID)
	if err != nil {
		return MealPlan{}, err
	}

	newDays := make([]NewDay, len(clean.Weekdays))
	after := meal.PlanState{Settings: clean.Settings, Days: make([]meal.DayState, len(clean.Weekdays))}
	for i, wd := range clean.Weekdays {
		newDays[i].Weekday = wd
		after.Days[i].Weekday = wd
		if i >= len(days) {
			continue
		}
		newDays[i].Meals, after.Days[i].Consumed, err = s.newMeals(ctx, userID, days[i].Meals)
		if err != nil {
			return MealPlan{}, err
		}
	}
	if v := meal.CheckWrite(active, nil, after, confirm); !v.Allowed {
		return MealPlan{}, &OverageError{Verdict: v}
	}

	id, err := s.repo.CreatePlan(ctx, userID, NewPlan{
		Name: clean.Name, Description: clean.Description, Objective: clean.Objective,
		ActivityLevel: clean.ActivityLevel, Gender: clean.Gender, Settings: clean.Settings, Notes: clean.Notes,
	}, newDays)
	if err != nil {
		return MealPlan{}, err
	}
	return s.repo.GetPlan(ctx, id, userID)
}

// PlanIDOfDay, PlanIDOfMeal and PlanIDOfMealIngredient find the plan a part
// of a plan belongs to, so a page can return to it. apperr.ErrNotFound if the
// part is not the user's.
func (s *MealPlanService) PlanIDOfDay(ctx context.Context, dayID, userID uuid.UUID) (uuid.UUID, error) {
	return s.repo.PlanIDOfDay(ctx, dayID, userID)
}

func (s *MealPlanService) PlanIDOfMeal(ctx context.Context, mealID, userID uuid.UUID) (uuid.UUID, error) {
	return s.repo.PlanIDOfMeal(ctx, mealID, userID)
}

func (s *MealPlanService) PlanIDOfMealIngredient(ctx context.Context, mealIngredientID, userID uuid.UUID) (uuid.UUID, error) {
	return s.repo.PlanIDOfMealIngredient(ctx, mealIngredientID, userID)
}

// newMeals validates a day's drafted meals and works out their portions,
// returning them with what the day's defaults add up to. Alternatives are
// worked out the same way but never counted: one is eaten instead of its
// default, not as well.
func (s *MealPlanService) newMeals(ctx context.Context, userID uuid.UUID, drafts []MealDraft) ([]NewMeal, Macros, error) {
	out := make([]NewMeal, 0, len(drafts))
	var consumed Macros
	for _, draft := range drafts {
		name, err := validateMealName(draft.Name)
		if err != nil {
			return nil, Macros{}, err
		}
		portions, total, err := s.portions(ctx, userID, draft.Portions)
		if err != nil {
			return nil, Macros{}, err
		}
		slot := NewMeal{Name: name, OptionLabel: strings.TrimSpace(draft.OptionLabel), Portions: portions}
		for _, alt := range draft.Alternatives {
			label, err := validateOptionLabel(alt.Label)
			if err != nil {
				return nil, Macros{}, err
			}
			altPortions, _, err := s.portions(ctx, userID, alt.Portions)
			if err != nil {
				return nil, Macros{}, err
			}
			slot.Alternatives = append(slot.Alternatives, NewMeal{Name: name, OptionLabel: label, Portions: altPortions})
		}
		out = append(out, slot)
		consumed = consumed.Add(total)
	}
	return out, consumed, nil
}

func (s *MealPlanService) GetPlan(ctx context.Context, id, userID uuid.UUID) (MealPlan, error) {
	return s.repo.GetPlan(ctx, id, userID)
}

func (s *MealPlanService) ListPlans(ctx context.Context, userID uuid.UUID) ([]MealPlan, error) {
	return s.repo.ListPlans(ctx, userID)
}

func (s *MealPlanService) DeletePlan(ctx context.Context, id, userID uuid.UUID) error {
	return s.repo.DeletePlan(ctx, id, userID)
}

// PlanSettingsInput is an edit of a plan's name and plan-wide choices.
type PlanSettingsInput struct {
	Name        string
	Description string
	Settings    meal.PlanSettings
	// ConfirmReset accepts that switching an advanced plan to easy returns
	// every day to the plan's default target.
	ConfirmReset   bool
	ConfirmOverage bool
}

// UpdateSettings renames a plan or changes its type or mode. Switching from
// advanced to easy needs ConfirmReset, drops every day's override, and is
// refused while any day would be over its target.
func (s *MealPlanService) UpdateSettings(ctx context.Context, planID, userID uuid.UUID, in PlanSettingsInput) error {
	var errs apperr.FieldErrors
	in.Name, errs = validatePlanName(in.Name, errs)
	in.Settings, errs = validateSettings(in.Settings, errs)
	if err := errs.OrNil(); err != nil {
		return err
	}
	active, err := s.requireTarget(ctx, userID)
	if err != nil {
		return err
	}

	return s.repo.WithPlanLocked(ctx, planID, userID, func(tx *PlanTx, plan MealPlan) error {
		toEasy := plan.Settings.Mode == meal.Advanced && in.Settings.Mode == meal.Easy
		if toEasy && !in.ConfirmReset {
			return apperr.FieldErrors{}.Add("confirm_reset",
				"Switching to easy resets every day to the plan's default target.").OrNil()
		}
		after := plan.State()
		after.Settings = in.Settings
		if toEasy {
			for i := range after.Days {
				after.Days[i].Override = meal.DayOverride{}
			}
		}
		if err := checkOverage(active, plan, after, toEasy, in.ConfirmOverage); err != nil {
			return err
		}
		if toEasy {
			if err := tx.ClearOverrides(ctx, planID); err != nil {
				return err
			}
		}
		return tx.UpdateSettings(ctx, planID, in.Name, in.Description, in.Settings)
	})
}

// AddDay adds a day to a plan: the given weekday in an advanced plan, or the
// next free one from Monday.
func (s *MealPlanService) AddDay(ctx context.Context, planID, userID uuid.UUID, weekday *time.Weekday) (meal.Day, error) {
	var day meal.Day
	err := s.repo.WithPlanLocked(ctx, planID, userID, func(tx *PlanTx, plan MealPlan) error {
		taken := plan.Weekdays()
		wd, free := meal.NextFreeWeekday(taken)
		switch {
		case !free:
			return apperr.FieldErrors{}.Add("weekday", fmt.Sprintf("A plan has at most %d days.", meal.MaxDays)).OrNil()
		case weekday == nil:
		case plan.Settings.Mode != meal.Advanced:
			return apperr.FieldErrors{}.Add("weekday", "Easy plans add the next day of the week; choose weekdays in advanced mode.").OrNil()
		case *weekday < time.Sunday || *weekday > time.Saturday || slices.Contains(taken, *weekday):
			return apperr.FieldErrors{}.Add("weekday", "Choose a weekday the plan does not have yet.").OrNil()
		default:
			wd = *weekday
		}
		var err error
		day, err = tx.AddDay(ctx, planID, wd)
		return err
	})
	return day, err
}

// UpdateDay sets an advanced plan's override for one day.
func (s *MealPlanService) UpdateDay(ctx context.Context, dayID, userID uuid.UUID, o meal.DayOverride, confirm bool) error {
	active, err := s.requireTarget(ctx, userID)
	if err != nil {
		return err
	}
	if err = ValidateDayOverride(o, active); err != nil {
		return err
	}
	planID, err := s.repo.PlanIDOfDay(ctx, dayID, userID)
	if err != nil {
		return err
	}
	return s.repo.WithPlanLocked(ctx, planID, userID, func(tx *PlanTx, plan MealPlan) error {
		if plan.Settings.Mode != meal.Advanced {
			return apperr.FieldErrors{}.Add("mode", "Switch the plan to advanced to change a single day.").OrNil()
		}
		i, ok := plan.DayIndex(dayID)
		if !ok {
			return apperr.ErrNotFound
		}
		after := plan.State()
		after.Days[i].Override = o
		if err := checkOverage(active, plan, after, false, confirm); err != nil {
			return err
		}
		return tx.UpdateDay(ctx, dayID, o)
	})
}

// RemoveDay deletes a day and its meals. A plan keeps at least one day.
func (s *MealPlanService) RemoveDay(ctx context.Context, dayID, userID uuid.UUID) error {
	planID, err := s.repo.PlanIDOfDay(ctx, dayID, userID)
	if err != nil {
		return err
	}
	return s.repo.WithPlanLocked(ctx, planID, userID, func(tx *PlanTx, plan MealPlan) error {
		if len(plan.Days) == 1 {
			return apperr.FieldErrors{}.Add("day", "A plan needs at least one day.").OrNil()
		}
		return tx.RemoveDay(ctx, planID, dayID)
	})
}

func validateMealName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", apperr.FieldErrors{}.Add("name", "Give the meal a name.").OrNil()
	}
	return name, nil
}

// MaxOptionLabelRunes caps the label that tells a slot's options apart. A
// label is a tab's title, not a description.
const MaxOptionLabelRunes = 60

// validateOptionLabel checks the label that tells an alternative apart from
// its slot's other options; without one, two options would read the same.
func validateOptionLabel(label string) (string, error) {
	label = strings.TrimSpace(label)
	switch {
	case label == "":
		return "", apperr.FieldErrors{}.Add("option_label", "Give the option a label.").OrNil()
	case utf8.RuneCountInString(label) > MaxOptionLabelRunes:
		return "", apperr.FieldErrors{}.Add("option_label",
			fmt.Sprintf("Keep the label to %d characters.", MaxOptionLabelRunes)).OrNil()
	}
	return label, nil
}

// AddMeal adds a meal at the end of a day.
func (s *MealPlanService) AddMeal(ctx context.Context, dayID, userID uuid.UUID, name string) (Meal, error) {
	name, err := validateMealName(name)
	if err != nil {
		return Meal{}, err
	}
	planID, err := s.repo.PlanIDOfDay(ctx, dayID, userID)
	if err != nil {
		return Meal{}, err
	}
	var added Meal
	err = s.repo.WithPlanLocked(ctx, planID, userID, func(tx *PlanTx, _ MealPlan) error {
		var addErr error
		added, addErr = tx.AddMeal(ctx, planID, dayID, name, "")
		return addErr
	})
	return added, err
}

// AddOption adds an empty option to the end of the meal slot that mealID —
// any of the slot's options — belongs to. A blank label becomes the first
// "Option N" the slot does not use, so no two options read the same. A new
// option is empty, so it needs no overage check.
func (s *MealPlanService) AddOption(ctx context.Context, userID, planID, mealID uuid.UUID, label string) (meal.Meal, error) {
	label = strings.TrimSpace(label)
	if label != "" {
		var err error
		if label, err = validateOptionLabel(label); err != nil {
			return meal.Meal{}, err
		}
	}
	var added meal.Meal
	err := s.repo.WithPlanLocked(ctx, planID, userID, func(tx *PlanTx, plan MealPlan) error {
		slot, ok := findSlot(plan, mealID)
		if !ok {
			return apperr.ErrNotFound
		}
		if label == "" {
			label = freeOptionLabel(slot)
		}
		var addErr error
		added, addErr = tx.AddOption(ctx, planID, slot.DayID, slot.MealNumber, slot.Name, label)
		return addErr
	})
	return added, err
}

// RemoveMeal deletes a meal with its ingredients: the whole slot, every
// option, when it is the slot's default; only itself when it is an
// alternative. Taking food away never takes a day further over, so it is not
// checked, but it holds the plan's lock like every other change so a
// concurrent checked change sees it.
func (s *MealPlanService) RemoveMeal(ctx context.Context, mealID, userID uuid.UUID) error {
	planID, err := s.repo.PlanIDOfMeal(ctx, mealID, userID)
	if err != nil {
		return err
	}
	return s.repo.WithPlanLocked(ctx, planID, userID, func(tx *PlanTx, plan MealPlan) error {
		m, ok := findMeal(plan, mealID)
		if !ok {
			return apperr.ErrNotFound
		}
		return tx.RemoveMeal(ctx, planID, m)
	})
}

// freeOptionLabel is the first "Option N", N from 2, that none of slot's
// options carries in any letter case. Counting the slot's options instead
// would repeat a label once a middle option is gone.
func freeOptionLabel(slot Meal) string {
	used := map[string]bool{}
	for _, o := range slot.Options() {
		used[strings.ToLower(strings.TrimSpace(o.OptionLabel))] = true
	}
	for n := 2; ; n++ {
		label := fmt.Sprintf("Option %d", n)
		if !used[strings.ToLower(label)] {
			return label
		}
	}
}

// findSlot finds the slot — its default, holding the alternatives — that any
// of its options' IDs names.
func findSlot(plan MealPlan, mealID uuid.UUID) (Meal, bool) {
	for _, d := range plan.Days {
		for _, slot := range d.Meals {
			for _, m := range slot.Options() {
				if m.ID == mealID {
					return slot, true
				}
			}
		}
	}
	return Meal{}, false
}

// findMeal finds a meal of the plan by id, default or alternative.
func findMeal(plan MealPlan, mealID uuid.UUID) (Meal, bool) {
	for _, d := range plan.Days {
		for _, slot := range d.Meals {
			for _, m := range slot.Options() {
				if m.ID == mealID {
					return m, true
				}
			}
		}
	}
	return Meal{}, false
}

// AddIngredient adds one portion to a meal; see AddIngredients.
func (s *MealPlanService) AddIngredient(ctx context.Context, mealID, userID uuid.UUID, in MealIngredientInput, confirm bool) (MealIngredient, error) {
	added, err := s.AddIngredients(ctx, mealID, userID, []MealIngredientInput{in}, confirm)
	if err != nil {
		return MealIngredient{}, err
	}
	return added[0], nil
}

// AddIngredients adds portions to a meal, as a spoken meal arrives, with each
// portion's macros snapshotted and the meal's and plan's totals recalculated.
//
// The portions are checked together against the meal's day, and written
// together or not at all: a bad line — a missing quantity, an id this account
// cannot see — or a day the batch would take over refuses the whole batch
// instead of leaving half a meal behind. An alternative option never counts
// toward its day, so adding to one is never an overage.
func (s *MealPlanService) AddIngredients(ctx context.Context, mealID, userID uuid.UUID, in []MealIngredientInput, confirm bool) ([]MealIngredient, error) {
	if len(in) == 0 {
		return nil, apperr.FieldErrors{}.Add("ingredient_id", "Choose at least one ingredient.").OrNil()
	}
	portions, total, err := s.portions(ctx, userID, in)
	if err != nil {
		return nil, err
	}
	active, err := s.requireTarget(ctx, userID)
	if err != nil {
		return nil, err
	}
	planID, err := s.repo.PlanIDOfMeal(ctx, mealID, userID)
	if err != nil {
		return nil, err
	}

	var added []MealIngredient
	err = s.repo.WithPlanLocked(ctx, planID, userID, func(tx *PlanTx, plan MealPlan) error {
		i, alternative, ok := plan.LocateMeal(mealID)
		if !ok {
			return apperr.ErrNotFound
		}
		if !alternative {
			after := plan.State()
			after.Days[i].Consumed = after.Days[i].Consumed.Add(total)
			if overErr := checkOverage(active, plan, after, false, confirm); overErr != nil {
				return overErr
			}
		}
		var addErr error
		added, addErr = tx.AddPortions(ctx, planID, mealID, portions)
		return addErr
	})
	return added, err
}

// portions validates lines and works out the macros each adds, from the
// ingredient's per-100g profile.
func (s *MealPlanService) portions(ctx context.Context, userID uuid.UUID, lines []MealIngredientInput) ([]NewPortion, Macros, error) {
	out := make([]NewPortion, len(lines))
	var total Macros
	for i, line := range lines {
		clean, err := ValidateMealIngredient(line)
		if err != nil {
			return nil, Macros{}, err
		}
		ingredient, err := s.repo.GetIngredient(ctx, clean.IngredientID, userID)
		if err != nil {
			return nil, Macros{}, err
		}
		macros := ingredient.MacrosFor(clean.QuantityGrams)
		out[i] = NewPortion{
			IngredientID: clean.IngredientID, QuantityGrams: clean.QuantityGrams, Macros: macros,
			SourceText: strings.TrimSpace(clean.SourceText), Estimated: clean.Estimated,
		}
		total = total.Add(macros)
	}
	return out, total, nil
}

// RemoveIngredient takes one ingredient off a meal, under the plan's lock for
// the reason RemoveMeal gives.
func (s *MealPlanService) RemoveIngredient(ctx context.Context, mealIngredientID, userID uuid.UUID) error {
	planID, mealID, err := s.repo.LocateMealIngredient(ctx, mealIngredientID, userID)
	if err != nil {
		return err
	}
	return s.repo.WithPlanLocked(ctx, planID, userID, func(tx *PlanTx, plan MealPlan) error {
		if _, ok := plan.DayIndexOfMeal(mealID); !ok {
			return apperr.ErrNotFound
		}
		return tx.RemovePortion(ctx, planID, mealID, mealIngredientID)
	})
}
