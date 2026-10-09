package planimport

import (
	"fmt"
	"strings"

	"github.com/FACorreiaa/go-utils/pkg/util"
)

// Every reader — spreadsheet, JSON, and the model — ends in the same place: a
// list of rows of text, one per exercise or food, exactly as the source wrote
// them. The builders below are the only code that turns that text into numbers,
// so a "90s" rest or a "3x10" means the same thing whichever file it came from.

// WorkoutRow is one exercise as written in the source.
type WorkoutRow struct {
	Day, Exercise, Sets, Reps, Load, Rest, Notes string

	// Uncertain is set by the model when it had to guess which column a value
	// belonged to. Spreadsheets have headers and never set it.
	Uncertain bool
}

// MealRow is one food as written in the source.
type MealRow struct {
	Day, Meal, Food, Quantity, Unit, Protein, Carbs, Fat string
	Uncertain                                            bool
}

const uncertainFlag = "Check this row — the reader wasn't sure about it."

// buildWorkout groups rows into days in the order the days first appear.
//
// Rows for the same day need not be contiguous: a sheet sorted by exercise
// still describes the same Monday.
func buildWorkout(name string, rows []WorkoutRow, unparsed []string) (WorkoutDraft, error) {
	draft := WorkoutDraft{Name: strings.TrimSpace(name), Days: []WorkoutDayDraft{}, Unparsed: unparsed}
	index := map[string]int{}

	for _, row := range rows {
		exName := strings.TrimSpace(row.Exercise)
		if exName == "" {
			if line := joinNonEmpty(row.Day, row.Sets, row.Reps, row.Load, row.Rest, row.Notes); line != "" {
				draft.Unparsed = append(draft.Unparsed, line)
			}
			continue
		}

		ex := ExerciseDraft{
			Name:  exName,
			Reps:  strings.TrimSpace(row.Reps),
			Load:  strings.TrimSpace(row.Load),
			Notes: strings.TrimSpace(row.Notes),
			Flags: []string{},
		}

		sets, reps, flag := parseSets(row.Sets)
		ex.Sets = sets
		if flag != "" {
			ex.Flags = append(ex.Flags, flag)
		}
		if reps != "" {
			if ex.Reps == "" {
				ex.Reps = reps
			} else if !strings.EqualFold(ex.Reps, reps) {
				ex.Flags = append(ex.Flags, fmt.Sprintf("Sets said %q but reps said %q — check which is right.", row.Sets, row.Reps))
			}
		}

		rest, flag := parseRest(row.Rest)
		ex.RestSeconds = rest
		if flag != "" {
			ex.Flags = append(ex.Flags, flag)
		}
		if row.Uncertain {
			ex.Flags = append(ex.Flags, uncertainFlag)
		}

		label := strings.TrimSpace(row.Day)
		key := dayKey(label)
		i, seen := index[key]
		if !seen {
			day := WorkoutDayDraft{Label: label, Exercises: []ExerciseDraft{}}
			if wd, ok := weekdayOf(label); ok {
				day.Weekday = wd.String()
			}
			draft.Days = append(draft.Days, day)
			i = len(draft.Days) - 1
			index[key] = i
		}
		draft.Days[i].Exercises = append(draft.Days[i].Exercises, ex)
	}

	if len(draft.Days) == 0 {
		return WorkoutDraft{}, refuse(ReasonNotAPlan, "No exercises were found in this file, so it doesn't look like a workout plan.")
	}
	draft.Normalize()
	return draft, nil
}

// buildMeal groups rows into days, then meals, in first-seen order.
func buildMeal(name string, rows []MealRow, unparsed []string) (MealDraft, error) {
	draft := MealDraft{Name: strings.TrimSpace(name), Days: []MealDayDraft{}, Unparsed: unparsed}
	dayIndex := map[string]int{}
	mealIndex := map[string]int{}

	for _, row := range rows {
		foodName := strings.TrimSpace(row.Food)
		if foodName == "" {
			if line := joinNonEmpty(row.Day, row.Meal, row.Quantity, row.Unit, row.Protein, row.Carbs, row.Fat); line != "" {
				draft.Unparsed = append(draft.Unparsed, line)
			}
			continue
		}

		food := FoodDraft{Food: foodName, Unit: strings.TrimSpace(row.Unit), Candidates: []Candidate{}, Flags: []string{}}

		qty, unitFromQty, ok := parseAmount(row.Quantity)
		if !ok {
			food.Flags = append(food.Flags, fmt.Sprintf("Quantity %q isn't a number.", strings.TrimSpace(row.Quantity)))
		}
		food.Quantity = qty
		if food.Unit == "" {
			food.Unit = unitFromQty
		}

		for _, m := range []struct {
			label string
			raw   string
			dst   **float64
		}{
			{"Protein", row.Protein, &food.StatedProteinG},
			{"Carbs", row.Carbs, &food.StatedCarbG},
			{"Fat", row.Fat, &food.StatedFatG},
		} {
			v, flag := parseGrams(m.label, m.raw)
			*m.dst = v
			if flag != "" {
				food.Flags = append(food.Flags, flag)
			}
		}
		if row.Uncertain {
			food.Flags = append(food.Flags, uncertainFlag)
		}

		dayLabel := strings.TrimSpace(row.Day)
		dKey := dayKey(dayLabel)
		di, seen := dayIndex[dKey]
		if !seen {
			day := MealDayDraft{Label: dayLabel, Meals: []MealDraftMeal{}, Over: []string{}}
			if wd, ok := weekdayOf(dayLabel); ok {
				day.Weekday = util.Ptr(int(wd))
			}
			draft.Days = append(draft.Days, day)
			di = len(draft.Days) - 1
			dayIndex[dKey] = di
		}

		mealName := strings.TrimSpace(row.Meal)
		mKey := dKey + "\x00" + dayKey(mealName)
		mi, seen := mealIndex[mKey]
		if !seen {
			draft.Days[di].Meals = append(draft.Days[di].Meals, MealDraftMeal{Name: mealName, Foods: []FoodDraft{}})
			mi = len(draft.Days[di].Meals) - 1
			mealIndex[mKey] = mi
		}
		draft.Days[di].Meals[mi].Foods = append(draft.Days[di].Meals[mi].Foods, food)
	}

	if len(draft.Days) == 0 {
		return MealDraft{}, refuse(ReasonNotAPlan, "No foods were found in this file, so it doesn't look like a meal plan.")
	}
	draft.Normalize()
	return draft, nil
}

func joinNonEmpty(cells ...string) string {
	var parts []string
	for _, c := range cells {
		if c = strings.TrimSpace(c); c != "" {
			parts = append(parts, c)
		}
	}
	return strings.Join(parts, " · ")
}
