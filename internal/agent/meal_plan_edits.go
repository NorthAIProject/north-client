package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/meals"
	"github.com/NorthAIProject/north-client/internal/meals/meal"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

// Reading and editing meal plans from the chat.
//
// "Add 30 g oats to breakfast every day", "drop Tuesday's snack", "make
// lunch's rice 150 g" — the meal plan page's edits, said instead of clicked.
//
// The shape follows workout_edits.go. No plan, meal or portion id ever reaches
// the model: plans are named, days are weekdays, meals and foods are the names
// the plan shows, and all of it is resolved here or in the meals service.
//
// edit_meal_plan takes a list of changes rather than one, because each write
// shows the person an approval card (see coach.writingCalls): a three-part
// edit is one card to approve, applied all-or-nothing by
// MealPlanService.ApplyChanges, not three cards that could leave the plan
// half-edited if one is declined.

// maxPlanChanges bounds one edit_meal_plan call; more is a new plan, which
// create_meal_plan makes.
const maxPlanChanges = 20

// maxFoodGrams is the most one food line may weigh, the bound create_meal_plan
// holds portions to.
const maxFoodGrams = 2000

func getMealPlan(plans *meals.MealPlanService) Capability {
	type args struct {
		Plan string `json:"plan"`
	}

	// Listed as required because an empty required list makes every property
	// required anyway (see ai.JSONSchema); "" means the most recently changed
	// plan.
	params := ai.Object("which plan", map[string]*ai.Schema{
		"plan": ai.String("the plan's name; empty for their most recently changed plan"),
	}, "plan")

	return Capability{
		Tool: ai.Tool{
			Name: "get_meal_plan",
			Description: "Read one of this person's meal plans: each day's meals with every food and its weight, and each day's " +
				"calories and macros against their target. A meal with several options lists them numbered; option 1 is the " +
				"one the day's totals count. Days that are all the same are shown once. Read it before editing a plan or " +
				"answering what is in it. " +
				"With several plans and none named, it lists them and shows the most recently changed one.",
			Parameters: params,
		},
		ReadOnly: true,
		Invoke: func(ctx context.Context, userID uuid.UUID, raw json.RawMessage) (string, error) {
			in, err := Decode[args](raw)
			if err != nil {
				return "", err
			}
			list, err := plans.ListPlans(ctx, userID)
			if err != nil {
				return "", err
			}
			chosen, err := pickMealPlan(list, in.Plan, false)
			if err != nil {
				return "", err
			}
			full, err := plans.GetPlan(ctx, chosen.ID, userID)
			if err != nil {
				return "", err
			}
			target, err := plans.ActiveTarget(ctx, userID)
			if err != nil {
				return "", err
			}

			var b strings.Builder
			if strings.TrimSpace(in.Plan) == "" && len(list) > 1 {
				fmt.Fprintf(&b, "This person has %d meal plans: %s. Showing %q, the most recently changed; name another to see it.\n\n",
					len(list), mealPlanNames(list), full.Name)
			}
			describeMealPlan(&b, full, target)
			return b.String(), nil
		},
	}
}

func editMealPlan(plans *meals.MealPlanService, ingredients *meals.IngredientService) Capability {
	type args struct {
		Plan            string      `json:"plan"`
		Changes         []changeArg `json:"changes"`
		AllowOverTarget bool        `json:"allow_over_target"`
	}

	ops := make([]string, len(meals.PlanChangeOps))
	for i, op := range meals.PlanChangeOps {
		ops[i] = string(op)
	}
	weekdayNames := make([]string, len(meal.WeekOrder))
	for i, wd := range meal.WeekOrder {
		weekdayNames[i] = wd.String()
	}

	return Capability{
		Tool: ai.Tool{
			Name: "edit_meal_plan",
			Description: "Change the meals in one of this person's meal plans. Put every change they asked for in one call: the " +
				"changes are applied together or not at all. Ops: add_meal (an empty meal at the end of the day), remove_meal (the " +
				"meal with all its options), add_food (grams of a catalog food added to a meal), remove_food (every portion of that " +
				"food in the meal), set_grams (the meal holds exactly that many grams of a food already in it), add_option (an empty " +
				"option named option_label after the meal's last one), remove_option (option 2 or later), set_optional (mark a food " +
				"already in the meal optional — shown, not counted — with optional true, or counted again with optional false). A " +
				"food added with optional true is shown but never counted. A meal can hold several " +
				"interchangeable options; option 1 is the default, the one the day's totals count. add_food, remove_food and " +
				"set_grams change option 1 unless option says otherwise; an option number a target day's meal does not have " +
				"refuses the whole change, so name days when the days differ. Leave days out to change every day " +
				"of the plan; a change applies on the days where its meal (and food) exist, and is refused only if it matches on " +
				"none. Use get_meal_plan first to see the meal, option and food names. A result that would take a day further over its " +
				"macro target is refused with the amounts and nothing is saved: shrink the change, or tell the person and ask. " +
				"Set allow_over_target only after the person has explicitly accepted an overage that a refused call reported; " +
				"easy plans never go over, whatever it says.",
			Parameters: ai.Object("the changes to make", map[string]*ai.Schema{
				"plan": ai.String("the plan's name; needed only when they have more than one"),
				"changes": ai.Array("every change, applied in order", ai.Object("one change", map[string]*ai.Schema{
					"op":    ai.Enum("what to do", ops...),
					"days":  ai.Array("the weekdays to change; omit for every day of the plan", ai.Enum("a weekday", weekdayNames...)),
					"meal":  ai.String("the meal's name, such as 'Breakfast'"),
					"food":  ai.String("the food: a plain catalog name for add_food, the name the plan shows for remove_food and set_grams"),
					"grams": ai.Number("grams to add (add_food) or to leave (set_grams)"),
					"option": ai.Integer("which of the meal's options, counting from 1 as get_meal_plan lists them; 1 is the " +
						"default option counted in the day's totals; omit for 1"),
					"option_label": ai.String("the new option's name, for add_option, such as 'Peixe'"),
					"optional": ai.Boolean("for add_food, add it as optional (shown, not counted); for set_optional, true " +
						"makes the food optional and false counts it again"),
				}, "op", "meal")),
				"allow_over_target": ai.Boolean("true only once the person accepted going over their target, after a refusal said by how much"),
			}, "changes"),
		},
		Invoke: func(ctx context.Context, userID uuid.UUID, raw json.RawMessage) (string, error) {
			in, err := Decode[args](raw)
			if err != nil {
				return "", err
			}
			changes, err := planChangesFromArgs(in.Changes)
			if err != nil {
				return "", err
			}
			// Catalog names are resolved before anything is written, as
			// create_meal_plan does.
			for i, c := range changes {
				if c.Op != meals.OpAddFood {
					continue
				}
				match, matchErr := matchIngredient(ctx, ingredients, userID, c.Food)
				if matchErr != nil {
					return "", apperr.Wrap(matchErr, "change %d", i+1)
				}
				changes[i].IngredientID = match.ID
			}

			list, err := plans.ListPlans(ctx, userID)
			if err != nil {
				return "", err
			}
			chosen, err := pickMealPlan(list, in.Plan, true)
			if err != nil {
				return "", err
			}

			stored, applied, err := plans.ApplyChanges(ctx, chosen.ID, userID, changes, in.AllowOverTarget)
			if err != nil {
				return "", explainOverage(err)
			}
			target, err := plans.ActiveTarget(ctx, userID)
			if err != nil {
				return "", err
			}

			var b strings.Builder
			fmt.Fprintf(&b, "Updated the meal plan %q:", stored.Name)
			changed := map[time.Weekday]bool{}
			for _, a := range applied {
				fmt.Fprintf(&b, "\n- %s", a)
				for _, wd := range a.Weekdays {
					changed[wd] = true
				}
			}
			b.WriteString("\n\nThe changed days now:")
			var days []meal.Day
			for _, day := range stored.Days {
				if changed[day.Weekday] {
					days = append(days, day)
				}
			}
			describeMealDays(&b, stored, days, target)
			return b.String(), nil
		},
	}
}

func logPlannedMeal(plans *meals.MealPlanService, foodLog *meals.FoodLogService, userSvc *users.Service) Capability {
	type args struct {
		Meal   string `json:"meal"`
		Plan   string `json:"plan"`
		Option string `json:"option"`
	}

	return Capability{
		Tool: ai.Tool{
			Name: "log_planned_meal",
			Description: "Record that this person ate one of today's meals from their meal plan as planned, such as 'I had my " +
				"breakfast'. It logs the meal's foods and macros for today's weekday in their timezone. If they ate something " +
				"different, use log_food instead.",
			Parameters: ai.Object("the meal they ate", map[string]*ai.Schema{
				"meal": ai.String("the meal's name in the plan, such as 'Breakfast'"),
				"plan": ai.String("the plan's name; empty for their most recently changed plan"),
				"option": ai.String("which of the meal's options they ate: its number, such as '2', or its name, such as " +
					"'Peixe'; empty for option 1"),
			}, "meal"),
		},
		// Every call is another entry: eating the meal twice is two meals.
		Idempotent: false,
		Invoke: func(ctx context.Context, userID uuid.UUID, raw json.RawMessage) (string, error) {
			in, err := Decode[args](raw)
			if err != nil {
				return "", err
			}
			// The whole record, for the timezone that decides what today is.
			user, err := userSvc.ByID(ctx, userID)
			if err != nil {
				return "", err
			}
			now := time.Now().In(user.Location())

			list, err := plans.ListPlans(ctx, userID)
			if err != nil {
				return "", err
			}
			chosen, err := pickMealPlan(list, in.Plan, false)
			if err != nil {
				return "", err
			}
			full, err := plans.GetPlan(ctx, chosen.ID, userID)
			if err != nil {
				return "", err
			}
			planned, err := todaysMeal(full, now.Weekday(), in.Meal, in.Option)
			if err != nil {
				return "", err
			}

			// LogDate in their timezone, so a late dinner is filed on their
			// day rather than the server's.
			entry, err := foodLog.LogMeal(ctx, userID, meals.LogMealInput{MealID: planned.ID, LogDate: now})
			if err != nil {
				return "", err
			}
			m := entry.Macros
			return fmt.Sprintf("Logged %s from %q for today (%s): %.0f kcal, %.0f g protein, %.0f g carbs, %.0f g fat.",
				planned.DisplayName(), full.Name, now.Weekday(), m.Calories, m.ProteinG, m.CarbG, m.FatG), nil
		},
	}
}

// changeArg is one change as the model writes it.
type changeArg struct {
	Op          string   `json:"op"`
	Days        []string `json:"days"`
	Meal        string   `json:"meal"`
	Food        string   `json:"food"`
	Grams       float64  `json:"grams"`
	Option      int      `json:"option"`
	OptionLabel string   `json:"option_label"`
	Optional    bool     `json:"optional"`
}

// planChangesFromArgs turns the model's changes into the service's, checking
// what can be checked without a lookup. add_food's food stays a name in Food
// for the caller to resolve to an ingredient.
func planChangesFromArgs(in []changeArg) ([]meals.PlanChange, error) {
	if len(in) == 0 || len(in) > maxPlanChanges {
		return nil, apperr.Wrap(apperr.ErrValidation, "give between 1 and %d changes", maxPlanChanges)
	}
	out := make([]meals.PlanChange, len(in))
	for i, a := range in {
		c, err := planChangeFromArg(a)
		if err != nil {
			return nil, apperr.Wrap(err, "change %d", i+1)
		}
		out[i] = c
	}
	return out, nil
}

func planChangeFromArg(a changeArg) (meals.PlanChange, error) {
	invalid := func(format string, args ...any) error {
		return apperr.Wrap(apperr.ErrValidation, format, args...)
	}
	op := meals.PlanChangeOp(strings.TrimSpace(strings.ToLower(a.Op)))
	if !op.Valid() {
		return meals.PlanChange{}, invalid("%q is not a change; use one of %s", a.Op, opNames())
	}
	c := meals.PlanChange{
		Op: op, Meal: strings.TrimSpace(a.Meal), Food: strings.TrimSpace(a.Food), Grams: a.Grams,
		Option: a.Option, OptionLabel: strings.TrimSpace(a.OptionLabel), Optional: a.Optional,
	}
	if c.Meal == "" {
		return meals.PlanChange{}, invalid("name the meal")
	}
	if c.Option < 0 {
		return meals.PlanChange{}, invalid("count options from 1; option 1 is the one the day's totals count")
	}

	days, err := parseWeekdays(a.Days)
	if err != nil {
		return meals.PlanChange{}, err
	}
	c.Days = days

	switch op {
	case meals.OpAddMeal, meals.OpRemoveMeal, meals.OpAddOption, meals.OpRemoveOption:
		c.Food, c.Grams = "", 0
	case meals.OpAddFood, meals.OpSetGrams:
		if c.Food == "" {
			return meals.PlanChange{}, invalid("name the food")
		}
		if c.Grams <= 0 || c.Grams > maxFoodGrams {
			return meals.PlanChange{}, invalid("%q needs a weight between 1 and %d g", c.Food, maxFoodGrams)
		}
	case meals.OpRemoveFood, meals.OpSetOptional:
		if c.Food == "" {
			return meals.PlanChange{}, invalid("name the food")
		}
		c.Grams = 0
	}
	return c, nil
}

// parseWeekdays reads weekday names, whole or abbreviated ("Tue"), ignoring
// case. None is every day.
func parseWeekdays(names []string) ([]time.Weekday, error) {
	out := make([]time.Weekday, 0, len(names))
	for _, name := range names {
		wd, ok := parseWeekday(name)
		if !ok {
			return nil, apperr.Wrap(apperr.ErrValidation, "%q is not a weekday", name)
		}
		if !slices.Contains(out, wd) {
			out = append(out, wd)
		}
	}
	return out, nil
}

func parseWeekday(name string) (time.Weekday, bool) {
	needle := strings.ToLower(strings.TrimSpace(name))
	if len(needle) < 2 {
		return 0, false
	}
	var hits []time.Weekday
	for _, wd := range meal.WeekOrder {
		if strings.HasPrefix(strings.ToLower(wd.String()), needle) {
			hits = append(hits, wd)
		}
	}
	if len(hits) != 1 {
		return 0, false
	}
	return hits[0], true
}

func opNames() string {
	names := make([]string, len(meals.PlanChangeOps))
	for i, op := range meals.PlanChangeOps {
		names[i] = string(op)
	}
	return strings.Join(names, ", ")
}

// pickMealPlan chooses the plan a call is about: the one named, matched
// exactly or as a unique part of a name, ignoring case; otherwise the most
// recently changed. requireName refuses to guess between several plans, for
// writes, where the approval card would not show which plan was meant.
func pickMealPlan(list []meals.MealPlan, name string, requireName bool) (meals.MealPlan, error) {
	if len(list) == 0 {
		return meals.MealPlan{}, apperr.Wrap(apperr.ErrNotFound,
			"this person has no meal plan yet; create_meal_plan can make one")
	}
	needle := strings.ToLower(strings.TrimSpace(name))
	if needle == "" {
		if len(list) > 1 && requireName {
			return meals.MealPlan{}, apperr.Wrap(apperr.ErrValidation,
				"this person has several meal plans (%s); name the one to change", mealPlanNames(list))
		}
		return mostRecentlyChanged(list), nil
	}

	for _, p := range list {
		if strings.ToLower(p.Name) == needle {
			return p, nil
		}
	}
	var hits []meals.MealPlan
	for _, p := range list {
		if strings.Contains(strings.ToLower(p.Name), needle) {
			hits = append(hits, p)
		}
	}
	switch len(hits) {
	case 1:
		return hits[0], nil
	case 0:
		return meals.MealPlan{}, apperr.Wrap(apperr.ErrNotFound,
			"no meal plan called %q; their plans are %s", name, mealPlanNames(list))
	default:
		return meals.MealPlan{}, apperr.Wrap(apperr.ErrValidation,
			"%q matches several meal plans (%s); say which", name, mealPlanNames(hits))
	}
}

func mostRecentlyChanged(list []meals.MealPlan) meals.MealPlan {
	latest := list[0]
	for _, p := range list[1:] {
		if p.UpdatedAt.After(latest.UpdatedAt) {
			latest = p
		}
	}
	return latest
}

func mealPlanNames(list []meals.MealPlan) string {
	out := make([]string, len(list))
	for i, p := range list {
		out[i] = strconv.Quote(p.Name)
	}
	return strings.Join(out, ", ")
}

// todaysMeal finds a meal on the plan's day for weekday, and the option of it
// named by option: its number or its label; empty is option 1.
func todaysMeal(plan meals.MealPlan, weekday time.Weekday, name, option string) (meals.Meal, error) {
	if strings.TrimSpace(name) == "" {
		return meals.Meal{}, apperr.Wrap(apperr.ErrValidation, "name the meal to log")
	}
	i := slices.IndexFunc(plan.Days, func(d meal.Day) bool { return d.Weekday == weekday })
	if i < 0 {
		return meals.Meal{}, apperr.Wrap(apperr.ErrNotFound,
			"%q has nothing planned for %s; log what they ate with log_food", plan.Name, weekday)
	}
	day := plan.Days[i]
	m, found, err := meals.PickMeal(day, name)
	if err != nil {
		return meals.Meal{}, err
	}
	if !found {
		names := make([]string, len(day.Meals))
		for k, dm := range day.Meals {
			names[k] = strconv.Quote(dm.Name)
		}
		if len(names) == 0 {
			return meals.Meal{}, apperr.Wrap(apperr.ErrNotFound, "%s in %q has no meals yet", weekday, plan.Name)
		}
		return meals.Meal{}, apperr.Wrap(apperr.ErrNotFound,
			"%s in %q has no meal called %q; its meals are %s", weekday, plan.Name, name, strings.Join(names, ", "))
	}
	chosen, err := pickLoggedOption(day.Meals[m], option, weekday)
	if err != nil {
		return meals.Meal{}, err
	}
	if len(chosen.Ingredients) == 0 {
		return meals.Meal{}, apperr.Wrap(apperr.ErrValidation,
			"%s on %s has nothing in it yet, so there is nothing to log", chosen.DisplayName(), weekday)
	}
	return chosen, nil
}

// pickLoggedOption finds one of slot's options by its number ("2") or by its
// label, matched exactly and then as a unique part of one, ignoring case.
// Empty is option 1.
func pickLoggedOption(slot meals.Meal, want string, weekday time.Weekday) (meals.Meal, error) {
	want = strings.TrimSpace(want)
	options := slot.Options()
	if want == "" {
		return slot, nil
	}
	if n, err := strconv.Atoi(want); err == nil {
		if n < 1 || n > len(options) {
			return meals.Meal{}, apperr.Wrap(apperr.ErrNotFound,
				"there is no option %d: %s on %s has %s", n, slot.Name, weekday, optionCount(len(options)))
		}
		return options[n-1], nil
	}

	needle := strings.ToLower(want)
	var hits []int
	for k, o := range options {
		if strings.ToLower(o.OptionLabel) == needle {
			return o, nil
		}
		if strings.Contains(strings.ToLower(o.OptionLabel), needle) {
			hits = append(hits, k)
		}
	}
	switch len(hits) {
	case 1:
		return options[hits[0]], nil
	case 0:
		all := make([]int, len(options))
		for k := range options {
			all[k] = k
		}
		return meals.Meal{}, apperr.Wrap(apperr.ErrNotFound,
			"%s on %s has no option called %q; its options are %s", slot.Name, weekday, want, optionNames(options, all))
	default:
		return meals.Meal{}, apperr.Wrap(apperr.ErrValidation,
			"%q matches several options of %s (%s); say which", want, slot.Name, optionNames(options, hits))
	}
}

func optionCount(n int) string {
	if n == 1 {
		return "only one option"
	}
	return fmt.Sprintf("%d options", n)
}

// optionNames lists the options at positions as get_meal_plan numbers them,
// by position from 1: 2 "Peixe".
func optionNames(options []meals.Meal, positions []int) string {
	out := make([]string, len(positions))
	for i, k := range positions {
		out[i] = fmt.Sprintf("%d %q", k+1, options[k].OptionLabel)
	}
	return strings.Join(out, ", ")
}

// explainOverage adds to an overage refusal what the model can do about it.
// Other errors pass through unchanged.
func explainOverage(err error) error {
	var over *meals.OverageError
	if !errors.As(err, &over) {
		return err
	}
	if over.Verdict.CanConfirm {
		return fmt.Errorf("nothing was saved: %w If the person accepts going over, call again with allow_over_target set", err)
	}
	return fmt.Errorf("nothing was saved: %w This plan never goes over its target; make the change smaller", err)
}

// describeMealPlan writes a plan the way get_meal_plan shows it.
func describeMealPlan(b *strings.Builder, plan meals.MealPlan, target *meals.Macros) {
	fmt.Fprintf(b, "Meal plan %q (%s, %s mode, %d days).", plan.Name, plan.Settings.Type.Label(), plan.Settings.Mode, len(plan.Days))
	if target == nil {
		b.WriteString(" They have no macro target yet, so the days are not measured against one.")
	}
	describeMealDays(b, plan, plan.Days, target)
}

// describeMealDays writes days, each distinct day once, headed by every day
// that reads exactly like it: "Monday: …", "Tuesday–Sunday: …", "Tuesday,
// Thursday: …". An imported every-day plan is seven copies of one day, and
// listing each — or each again once one day of it is edited — would hand the
// model the same meals seven times. "Every day (…)" heads them only when days
// are all of the plan's days and read alike; a few changed days that match
// are just their span.
func describeMealDays(b *strings.Builder, plan meals.MealPlan, days []meal.Day, target *meals.Macros) {
	groups := groupDays(plan, days, target)
	if len(groups) == 1 && len(days) > 1 && len(days) == len(plan.Days) {
		fmt.Fprintf(b, "\nEvery day (%s): %s", weekdaySpan(groups[0].days), groups[0].body)
		return
	}
	for _, g := range groups {
		fmt.Fprintf(b, "\n%s: %s", weekdaySpan(g.days), g.body)
	}
}

// dayGroup is days that read exactly alike, and how they read.
type dayGroup struct {
	days []meal.Day
	body string
}

// groupDays groups days by their description, in the order each group's
// first day comes.
func groupDays(plan meals.MealPlan, days []meal.Day, target *meals.Macros) []dayGroup {
	var groups []dayGroup
	at := map[string]int{}
	for _, day := range days {
		var body strings.Builder
		describeDayBody(&body, plan, day, target)
		key := body.String()
		if i, ok := at[key]; ok {
			groups[i].days = append(groups[i].days, day)
			continue
		}
		at[key] = len(groups)
		groups = append(groups, dayGroup{days: []meal.Day{day}, body: key})
	}
	return groups
}

// weekdaySpan names days: one by its name, a run of consecutive weekdays as
// "Tuesday–Sunday", anything else listed.
func weekdaySpan(days []meal.Day) string {
	names := make([]string, len(days))
	consecutive := len(days) > 1
	for i, day := range days {
		names[i] = day.Weekday.String()
		if i > 0 && consecutive {
			consecutive = slices.Index(meal.WeekOrder, day.Weekday) == slices.Index(meal.WeekOrder, days[i-1].Weekday)+1
		}
	}
	if consecutive {
		return names[0] + "–" + names[len(names)-1]
	}
	return strings.Join(names, ", ")
}

// describeDayBody writes one day after its name: its totals against its
// target, then each meal with its foods and, where it has them, its options.
func describeDayBody(b *strings.Builder, plan meals.MealPlan, day meal.Day, target *meals.Macros) {
	consumed := day.Consumed()
	fmt.Fprintf(b, "%.0f kcal, %.0f g protein, %.0f g carbs, %.0f g fat.",
		consumed.Calories, consumed.ProteinG, consumed.CarbG, consumed.FatG)
	if target != nil {
		status := meal.StatusOf(meal.ResolveDayTarget(*target, plan.Settings, day.Override), consumed)
		fmt.Fprintf(b, " Target %.0f kcal, %.0f g protein, %.0f g carbs, %.0f g fat.",
			status.Target.Calories, status.Target.ProteinG, status.Target.CarbG, status.Target.FatG)
		if status.IsOver() {
			fmt.Fprintf(b, " Over by %.0f g protein, %.0f g carbs, %.0f g fat.",
				status.Over.ProteinG, status.Over.CarbG, status.Over.FatG)
		}
	}
	if len(day.Meals) == 0 {
		b.WriteString("\n  No meals yet.")
	}
	for _, m := range day.Meals {
		if len(m.Alternatives) == 0 {
			fmt.Fprintf(b, "\n  %s (%.0f kcal): %s", m.Name, m.TotalMacros.Calories, describeFoods(m))
			continue
		}
		fmt.Fprintf(b, "\n  %s (option 1%s, counted, %.0f kcal): %s", m.Name, quotedLabel(m), m.TotalMacros.Calories, describeFoods(m))
		for k, alt := range m.Alternatives {
			fmt.Fprintf(b, "\n    option %d%s (%.0f kcal): %s", k+2, quotedLabel(alt), alt.TotalMacros.Calories, describeFoods(alt))
		}
	}
}

func quotedLabel(m meals.Meal) string {
	if m.OptionLabel == "" {
		return ""
	}
	return " " + strconv.Quote(m.OptionLabel)
}

// describeFoods lists a meal option's foods by weight, marking the ones an
// import estimated.
func describeFoods(m meals.Meal) string {
	if len(m.Ingredients) == 0 {
		return "nothing yet"
	}
	foods := make([]string, len(m.Ingredients))
	for j, ing := range m.Ingredients {
		foods[j] = fmt.Sprintf("%.0f g %s", ing.QuantityGrams, ing.IngredientName)
		if ing.Estimated {
			foods[j] += " (estimated)"
		}
		if ing.Optional {
			foods[j] += " (optional, not counted)"
		}
	}
	return strings.Join(foods, ", ")
}
