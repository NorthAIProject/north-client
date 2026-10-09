package planimport

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// The review page posts the whole draft back on every button press.
//
// The draft travels as JSON in one hidden field, and the fields a person can
// edit are posted on top of it, named by position: "d0.e2.sets" is the third
// exercise of the first day, and "d0.m1.o2.f0.grams" the first food of the
// third option of the second meal (o0 is a meal's first option, oN its Nth
// alternative). Nothing in either is trusted — a commit
// re-validates every value and recomputes every number — so the hidden JSON
// only saves re-reading the file, not checking it.

// reviewAction is what the pressed button asked for.
type reviewAction struct {
	verb string // update, delete-day, delete-exercise, delete-meal, delete-option, delete-food, save, save-confirm
	path []int
}

func parseAction(raw string) reviewAction {
	parts := strings.Split(raw, ":")
	a := reviewAction{verb: parts[0]}
	for _, p := range parts[1:] {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return reviewAction{verb: "update"}
		}
		a.path = append(a.path, n)
	}
	if a.verb == "" {
		a.verb = "update"
	}
	return a
}

func baseDraft(r *http.Request, into any) error {
	raw := r.PostFormValue("draft")
	if raw == "" || json.Unmarshal([]byte(raw), into) != nil {
		return apperr.Wrap(apperr.ErrValidation, "the review form arrived without its plan")
	}
	return nil
}

// workoutFromForm rebuilds the reviewed workout draft.
func workoutFromForm(r *http.Request) (WorkoutDraft, reviewAction, error) {
	var d WorkoutDraft
	if err := baseDraft(r, &d); err != nil {
		return WorkoutDraft{}, reviewAction{}, err
	}
	d.Normalize()

	d.Name = r.PostFormValue("name")
	for i := range d.Days {
		day := &d.Days[i]
		day.Weekday = r.PostFormValue(fmt.Sprintf("d%d.weekday", i))
		for j := range day.Exercises {
			ex := &day.Exercises[j]
			key := func(field string) string { return r.PostFormValue(fmt.Sprintf("d%d.e%d.%s", i, j, field)) }

			ex.Name = key("name")
			ex.Reps = key("reps")
			ex.Load = key("load")
			ex.Notes = key("notes")
			ex.Sets = formInt(key("sets"), "Sets", &ex.Flags)
			ex.RestSeconds = formInt(key("rest"), "Rest", &ex.Flags)
		}
	}
	return d, parseAction(r.PostFormValue("action")), nil
}

// applyWorkoutAction removes what a delete button named.
func applyWorkoutAction(d *WorkoutDraft, a reviewAction) {
	switch {
	case a.verb == "delete-day" && len(a.path) == 1 && a.path[0] < len(d.Days):
		d.Days = append(d.Days[:a.path[0]], d.Days[a.path[0]+1:]...)
	case a.verb == "delete-exercise" && len(a.path) == 2 && a.path[0] < len(d.Days):
		day := &d.Days[a.path[0]]
		if a.path[1] < len(day.Exercises) {
			day.Exercises = append(day.Exercises[:a.path[1]], day.Exercises[a.path[1]+1:]...)
		}
		if len(day.Exercises) == 0 {
			d.Days = append(d.Days[:a.path[0]], d.Days[a.path[0]+1:]...)
		}
	}
}

// mealFromForm rebuilds the reviewed meal draft.
func mealFromForm(r *http.Request) (MealDraft, reviewAction, error) {
	var d MealDraft
	if err := baseDraft(r, &d); err != nil {
		return MealDraft{}, reviewAction{}, err
	}
	d.Normalize()

	d.Name = r.PostFormValue("name")
	d.PlanType = r.PostFormValue("plan_type")
	d.Mode = r.PostFormValue("mode")
	d.CustomCarbPct = nil
	if raw := strings.TrimSpace(r.PostFormValue("custom_carb_pct")); raw != "" {
		if v, err := strconv.ParseFloat(raw, 64); err == nil {
			d.CustomCarbPct = &v
		}
	}

	for i := range d.Days {
		day := &d.Days[i]
		day.Weekday = nil
		if n, err := strconv.Atoi(r.PostFormValue(fmt.Sprintf("d%d.weekday", i))); err == nil && n >= 0 && n <= 6 {
			day.Weekday = &n
		}
		for j := range day.Meals {
			m := &day.Meals[j]
			m.Name = r.PostFormValue(fmt.Sprintf("d%d.m%d.name", i, j))
			foodsFromForm(r, fmt.Sprintf("d%d.m%d.o0", i, j), m.Foods)
			for o := range m.Alternatives {
				foodsFromForm(r, fmt.Sprintf("d%d.m%d.o%d", i, j, o+1), m.Alternatives[o].Foods)
			}
		}
	}
	return d, parseAction(r.PostFormValue("action")), nil
}

// foodsFromForm reads one option's food lines, posted under prefix.
func foodsFromForm(r *http.Request, prefix string, foods []FoodDraft) {
	for k := range foods {
		f := &foods[k]
		key := func(field string) string { return r.PostFormValue(fmt.Sprintf("%s.f%d.%s", prefix, k, field)) }

		f.Grams = nil
		if v, err := strconv.ParseFloat(strings.TrimSpace(key("grams")), 64); err == nil && v > 0 {
			f.Grams = &v
		}

		switch choice := key("ingredient"); choice {
		case "mine":
			f.IngredientID, f.SaveAsMine = nil, true
		case "":
			f.IngredientID, f.SaveAsMine = nil, false
		default:
			if id, err := uuid.Parse(choice); err == nil {
				f.IngredientID, f.SaveAsMine = &id, false
			}
		}
	}
}

// applyMealAction removes what a delete button named, and any option, meal
// or day that leaves empty.
func applyMealAction(d *MealDraft, a reviewAction) {
	p := a.path
	switch {
	case a.verb == "delete-day" && len(p) == 1 && p[0] < len(d.Days):
		d.Days = append(d.Days[:p[0]], d.Days[p[0]+1:]...)
	case a.verb == "delete-meal" && len(p) == 2 && p[0] < len(d.Days) && p[1] < len(d.Days[p[0]].Meals):
		day := &d.Days[p[0]]
		day.Meals = append(day.Meals[:p[1]], day.Meals[p[1]+1:]...)
	case a.verb == "delete-option" && len(p) == 3 && p[0] < len(d.Days) && p[1] < len(d.Days[p[0]].Meals):
		deleteOption(&d.Days[p[0]].Meals[p[1]], p[2])
	case a.verb == "delete-food" && len(p) == 4 && p[0] < len(d.Days) && p[1] < len(d.Days[p[0]].Meals):
		m := &d.Days[p[0]].Meals[p[1]]
		foods := optionFoods(m, p[2])
		if foods == nil || p[3] >= len(*foods) {
			break
		}
		*foods = append((*foods)[:p[3]], (*foods)[p[3]+1:]...)
		if len(*foods) == 0 {
			deleteOption(m, p[2])
		}
	}
	for i := range d.Days {
		d.Days[i].Meals = slices.DeleteFunc(d.Days[i].Meals, func(m MealDraftMeal) bool { return len(m.Foods) == 0 && len(m.Alternatives) == 0 })
	}
	d.Days = slices.DeleteFunc(d.Days, func(day MealDayDraft) bool { return len(day.Meals) == 0 })
}

// optionFoods is option o's food lines (0 being the first option), or nil
// when the meal has no such option.
func optionFoods(m *MealDraftMeal, o int) *[]FoodDraft {
	switch {
	case o == 0:
		return &m.Foods
	case o <= len(m.Alternatives):
		return &m.Alternatives[o-1].Foods
	}
	return nil
}

// deleteOption removes option o. Removing the first promotes the next
// option to be the one counted; removing the only one empties the meal,
// which applyMealAction then drops.
func deleteOption(m *MealDraftMeal, o int) {
	switch {
	case o == 0 && len(m.Alternatives) > 0:
		next := m.Alternatives[0]
		m.OptionLabel, m.Foods, m.Alternatives = next.Label, next.Foods, m.Alternatives[1:]
	case o == 0:
		m.OptionLabel, m.Foods = "", nil
	case o <= len(m.Alternatives):
		m.Alternatives = append(m.Alternatives[:o-1], m.Alternatives[o:]...)
	}
	if len(m.Alternatives) == 0 {
		m.Alternatives = nil
	}
}

// formInt reads an optional whole number typed on the review page. Blank is
// "not stated"; anything else that is not a whole number is flagged rather
// than guessed at.
func formInt(raw, label string, flags *[]string) *int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		*flags = appendOnce(*flags, fmt.Sprintf("%s %q isn't a whole number.", label, raw))
		return nil
	}
	return &n
}
