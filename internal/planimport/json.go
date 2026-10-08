package planimport

import (
	"bytes"
	"encoding/json"
	"strings"
)

// The JSON shapes accepted. Either a bare array of days, or an object with a
// name and the days under "days":
//
//	[{"day": "Monday", "exercises": [{"name", "sets", "reps", "load", "rest", "notes"}]}]
//	[{"day": "Monday", "meals": [{"meal", "food", "quantity", "unit", "protein", "carbs", "fat"}]}]
//
// A meal entry may instead be a named meal holding its foods,
// {"name": "Breakfast", "foods": [...]}, which is how most apps export.
//
// Values are read as text first and then parsed by the same cell parsers a
// spreadsheet goes through, so "sets": 3 and "sets": "3" are one thing, and
// "rest": "90s" is read the same as in a CSV.

// flex is a JSON value read as the text a person would have typed.
type flex string

func (f *flex) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	switch {
	case bytes.Equal(data, []byte("null")):
		*f = ""
	case len(data) > 0 && data[0] == '"':
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		*f = flex(s)
	case len(data) > 0 && (data[0] == '{' || data[0] == '['):
		// A nested object where a value belongs is not something to flatten
		// into a cell; keep it visible so it lands in Unparsed, not nowhere.
		*f = flex(string(data))
	default:
		// Numbers and booleans keep their literal text: 3, 2.5, true.
		*f = flex(string(data))
	}
	return nil
}

func (f flex) String() string { return strings.TrimSpace(string(f)) }

// decodeDays accepts a bare array or {name, days} and decodes the days into
// out.
func decodeDays(data []byte, out any) (string, bool) {
	data = bytes.TrimSpace(data)
	if len(data) > 0 && data[0] == '[' {
		return "", json.Unmarshal(data, out) == nil
	}
	var plan struct {
		Name flex            `json:"name"`
		Days json.RawMessage `json:"days"`
	}
	if err := json.Unmarshal(data, &plan); err != nil || len(plan.Days) == 0 {
		return "", false
	}
	return plan.Name.String(), json.Unmarshal(plan.Days, out) == nil
}

type jsonWorkoutDay struct {
	Day       flex              `json:"day"`
	Exercises []jsonWorkoutItem `json:"exercises"`
}

type jsonWorkoutItem struct {
	Name     flex `json:"name"`
	Exercise flex `json:"exercise"`
	Sets     flex `json:"sets"`
	Reps     flex `json:"reps"`
	Load     flex `json:"load"`
	Weight   flex `json:"weight"`
	Rest     flex `json:"rest"`
	Notes    flex `json:"notes"`
}

// WorkoutFromJSON maps the workout JSON shape onto a draft.
func WorkoutFromJSON(filename string, data []byte) (WorkoutDraft, error) {
	var days []jsonWorkoutDay
	name, ok := decodeDays(data, &days)
	if !ok {
		return WorkoutDraft{}, refuse(ReasonNotAPlan, `This JSON isn't a workout plan. Expected an array of days, each with "exercises".`)
	}

	var rows []WorkoutRow
	for _, d := range days {
		for _, ex := range d.Exercises {
			rows = append(rows, WorkoutRow{
				Day:      d.Day.String(),
				Exercise: first(ex.Name, ex.Exercise),
				Sets:     ex.Sets.String(),
				Reps:     ex.Reps.String(),
				Load:     first(ex.Load, ex.Weight),
				Rest:     ex.Rest.String(),
				Notes:    ex.Notes.String(),
			})
		}
	}
	return buildWorkout(nameOr(name, filename), rows, nil)
}

type jsonMealDay struct {
	Day   flex           `json:"day"`
	Meals []jsonMealItem `json:"meals"`
}

// jsonMealItem is either a food row (food, quantity, ...) with an optional
// meal name, or a named meal holding foods.
type jsonMealItem struct {
	Meal     flex           `json:"meal"`
	Name     flex           `json:"name"`
	Foods    []jsonMealItem `json:"foods"`
	Items    []jsonMealItem `json:"items"`
	Food     flex           `json:"food"`
	Quantity flex           `json:"quantity"`
	Unit     flex           `json:"unit"`
	Protein  flex           `json:"protein"`
	Carbs    flex           `json:"carbs"`
	Fat      flex           `json:"fat"`
}

// MealFromJSON maps the meal JSON shape onto a draft.
func MealFromJSON(filename string, data []byte) (MealDraft, error) {
	var days []jsonMealDay
	name, ok := decodeDays(data, &days)
	if !ok {
		return MealDraft{}, refuse(ReasonNotAPlan, `This JSON isn't a meal plan. Expected an array of days, each with "meals".`)
	}

	var rows []MealRow
	for _, d := range days {
		for _, m := range d.Meals {
			foods := append(m.Foods, m.Items...)
			if len(foods) == 0 {
				rows = append(rows, mealRow(d.Day.String(), m.Meal.String(), m))
				continue
			}
			mealName := first(m.Name, m.Meal)
			for _, f := range foods {
				rows = append(rows, mealRow(d.Day.String(), mealName, f))
			}
		}
	}
	return buildMeal(nameOr(name, filename), rows, nil)
}

func mealRow(day, mealName string, f jsonMealItem) MealRow {
	return MealRow{
		Day:      day,
		Meal:     mealName,
		Food:     first(f.Food, f.Name),
		Quantity: f.Quantity.String(),
		Unit:     f.Unit.String(),
		Protein:  f.Protein.String(),
		Carbs:    f.Carbs.String(),
		Fat:      f.Fat.String(),
	}
}

func first(values ...flex) string {
	for _, v := range values {
		if s := v.String(); s != "" {
			return s
		}
	}
	return ""
}
