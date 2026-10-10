package planimport

import (
	"fmt"
	"slices"
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
//
// Option names which of the meal's interchangeable options the food belongs
// to ("Opção 2"), or is empty for a meal with one. FoodEN is the reader's
// catalog stand-in for Food, and GramsEstimate its estimate of a vague
// amount; both are empty from a spreadsheet.
type MealRow struct {
	Day, Meal, Option, Food, Quantity, Unit, Protein, Carbs, Fat string
	FoodEN, GramsEstimate                                        string
	Uncertain                                                    bool
	// Optional is a food the source offers but does not count.
	Optional bool
}

// MealReading is everything a reader made of a meal plan, still as text.
type MealReading struct {
	Name     string
	Rows     []MealRow
	Unparsed []string
	// Notes is the file's advice, recipes and guidance.
	Notes string
	// SameAs lists meals the file said are the same as another ("Jantar
	// igual ao almoço"), copied by buildMeal rather than repeated as rows.
	SameAs []SameMeal
}

// SameMeal says meal Meal is eaten the same as meal SameAs.
type SameMeal struct {
	Meal, SameAs string
}

// everyDayLabel names the one day of a plan that named none.
const everyDayLabel = "Every day"

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

// buildMeal groups rows into days, then meals, then options, in first-seen
// order. A meal's first option becomes its Foods; the rest its Alternatives.
// A plan whose rows name no day becomes one day eaten every day.
func buildMeal(reading MealReading) (MealDraft, error) {
	draft := MealDraft{
		Name: strings.TrimSpace(reading.Name), Days: []MealDayDraft{}, Unparsed: reading.Unparsed,
		Notes: strings.TrimSpace(reading.Notes),
	}
	dayIndex := map[string]int{}
	mealIndex := map[string]int{}
	optionIndex := map[string]int{}
	anyDay := false

	for _, row := range reading.Rows {
		if strings.TrimSpace(row.Food) == "" {
			if line := joinNonEmpty(row.Day, row.Meal, row.Quantity, row.Unit, row.Protein, row.Carbs, row.Fat); line != "" {
				draft.Unparsed = append(draft.Unparsed, line)
			}
			continue
		}
		food := foodFromRow(row)

		dayLabel := strings.TrimSpace(row.Day)
		anyDay = anyDay || dayLabel != ""
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
		m := &draft.Days[di].Meals[mi]

		label := strings.TrimSpace(row.Option)
		oKey := mKey + "\x00" + dayKey(label)
		oi, seen := optionIndex[oKey]
		if !seen {
			// Positions: 0 is the first option, n is Alternatives[n-1].
			oi = len(m.Alternatives) + 1
			if len(m.Foods) == 0 && len(m.Alternatives) == 0 {
				oi = 0
				m.OptionLabel = label
			} else {
				if label == "" {
					label = fmt.Sprintf("Option %d", oi+1)
				}
				m.Alternatives = append(m.Alternatives, MealDraftOption{Label: label, Foods: []FoodDraft{}})
			}
			optionIndex[oKey] = oi
		}
		if oi == 0 {
			m.Foods = append(m.Foods, food)
		} else {
			m.Alternatives[oi-1].Foods = append(m.Alternatives[oi-1].Foods, food)
		}
	}

	if len(draft.Days) == 0 {
		return MealDraft{}, refuse(ReasonNotAPlan, "No foods were found in this file, so it doesn't look like a meal plan.")
	}
	for _, same := range reading.SameAs {
		if !copySameMeal(&draft, same) {
			// The model was told not to repeat the target's rows, so a
			// source that isn't there would lose the meal without a trace.
			draft.Unparsed = append(draft.Unparsed, fmt.Sprintf("%s: same as %s — no meal called %s was found",
				strings.TrimSpace(same.Meal), strings.TrimSpace(same.SameAs), strings.TrimSpace(same.SameAs)))
		}
	}
	if !anyDay {
		draft.EveryDay = true
		draft.Days[0].Label = everyDayLabel
	}
	draft.Normalize()
	return draft, nil
}

// foodFromRow reads one food line's text into numbers.
func foodFromRow(row MealRow) FoodDraft {
	original := strings.TrimSpace(row.Food)
	food := FoodDraft{
		Food: original, Unit: strings.TrimSpace(row.Unit), Candidates: []Candidate{}, Flags: []string{},
		SourceText: joinWords(row.Quantity, row.Unit, original),
		Optional:   row.Optional,
	}
	if en := strings.TrimSpace(row.FoodEN); en != "" {
		food.Food = en
	}

	qty, unitFromQty, ok := parseAmount(row.Quantity)
	food.Quantity = qty
	if food.Unit == "" {
		food.Unit = unitFromQty
	}
	estimateFlag := ""
	if !exactGrams(food) {
		if g, _, read := parseAmount(row.GramsEstimate); read && g != nil && *g > 0 {
			food.Grams, food.Estimated = g, true
			amount := joinWords(row.Quantity, row.Unit)
			if amount == "" {
				amount = original
			}
			estimateFlag = fmt.Sprintf("Estimated weight for %q. Check it.", amount)
		}
	}
	// A quantity an estimate stood in for ("150–250") is already flagged as
	// estimated; saying it isn't a number as well is the same warning twice.
	if !ok && !food.Estimated {
		food.Flags = append(food.Flags, fmt.Sprintf("Quantity %q isn't a number.", strings.TrimSpace(row.Quantity)))
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

	if estimateFlag != "" {
		food.Flags = append(food.Flags, estimateFlag)
	}
	if row.Uncertain {
		food.Flags = append(food.Flags, uncertainFlag)
	}
	return food
}

// exactGrams reports whether a line's quantity is already a weight, which no
// estimate is ever preferred over.
func exactGrams(f FoodDraft) bool {
	per, ok := gramsPer[normalizeUnit(f.Unit)]
	return f.Quantity != nil && ok && per > 0
}

// copySameMeal gives meal same.Meal a copy of same.SameAs's options on every
// day that has the latter, adding the meal where the day lacks it. Foods the
// target already had of its own ("sopa + prato idêntico ao almoço") are
// added to every copied option, after the copied foods. It reports whether
// the source meal was found on any day.
func copySameMeal(d *MealDraft, same SameMeal) bool {
	target, source := dayKey(same.Meal), dayKey(same.SameAs)
	if target == "" || source == "" || target == source {
		return true
	}
	found := false
	for di := range d.Days {
		day := &d.Days[di]
		si, ti := -1, -1
		for mi, m := range day.Meals {
			switch dayKey(m.Name) {
			case source:
				si = mi
			case target:
				ti = mi
			}
		}
		if si < 0 {
			continue
		}
		found = true
		copied := cloneMeal(day.Meals[si])
		if ti < 0 {
			copied.Name = strings.TrimSpace(same.Meal)
			day.Meals = append(day.Meals, copied)
			continue
		}
		var own []FoodDraft
		for _, opt := range day.Meals[ti].Options() {
			own = append(own, opt.Foods...)
		}
		copied.Name = day.Meals[ti].Name
		copied.Foods = append(copied.Foods, cloneFoods(own)...)
		for ai := range copied.Alternatives {
			copied.Alternatives[ai].Foods = append(copied.Alternatives[ai].Foods, cloneFoods(own)...)
		}
		day.Meals[ti] = copied
	}
	return found
}

// cloneMeal deep-copies a meal, so a copy edited on review leaves the
// original alone.
func cloneMeal(m MealDraftMeal) MealDraftMeal {
	out := MealDraftMeal{Name: m.Name, OptionLabel: m.OptionLabel, Foods: cloneFoods(m.Foods)}
	for _, alt := range m.Alternatives {
		out.Alternatives = append(out.Alternatives, MealDraftOption{Label: alt.Label, Foods: cloneFoods(alt.Foods)})
	}
	return out
}

func cloneFoods(foods []FoodDraft) []FoodDraft {
	out := make([]FoodDraft, len(foods))
	for i, f := range foods {
		f.Quantity = clonePtr(f.Quantity)
		f.Grams = clonePtr(f.Grams)
		f.StatedProteinG = clonePtr(f.StatedProteinG)
		f.StatedCarbG = clonePtr(f.StatedCarbG)
		f.StatedFatG = clonePtr(f.StatedFatG)
		f.IngredientID = clonePtr(f.IngredientID)
		f.Macros = clonePtr(f.Macros)
		f.Candidates = slices.Clone(f.Candidates)
		f.Flags = slices.Clone(f.Flags)
		f.Checks = slices.Clone(f.Checks)
		out[i] = f
	}
	return out
}

func clonePtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func joinNonEmpty(cells ...string) string { return joinTrimmed(" · ", cells) }

// joinWords puts cells back into the line they were read from: "2 fatias pão".
func joinWords(cells ...string) string { return joinTrimmed(" ", cells) }

func joinTrimmed(sep string, cells []string) string {
	var parts []string
	for _, c := range cells {
		if c = strings.TrimSpace(c); c != "" {
			parts = append(parts, c)
		}
	}
	return strings.Join(parts, sep)
}
