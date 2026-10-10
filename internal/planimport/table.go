package planimport

import (
	"fmt"
	"regexp"
	"strings"
)

// Column aliases, after normalizeHeader. Generous on purpose: a header row is
// written by a coach, not by a schema, and "Exercise Name", "Movement" and
// "exercise" mean one thing.
var (
	workoutColumns = map[string][]string{
		"day":      {"day", "days", "weekday", "session", "workout", "workout day", "training day"},
		"exercise": {"exercise", "exercises", "exercise name", "movement", "lift", "name"},
		"sets":     {"sets", "set", "no of sets", "number of sets"},
		"reps":     {"reps", "rep", "repetitions", "rep range", "reps per set"},
		"load":     {"load", "weight", "kg", "lbs", "lb", "intensity", "load note", "load notes"},
		"rest":     {"rest", "rest time", "rest period", "rest seconds", "rest sec", "rest s", "recovery"},
		"notes":    {"notes", "note", "cues", "form", "form notes", "form cues", "how to", "instructions", "comments", "technique"},
	}
	mealColumns = map[string][]string{
		"day":      {"day", "days", "weekday", "date"},
		"meal":     {"meal", "meals", "meal name", "time"},
		"option":   {"option", "options", "choice", "alternative", "opcao"},
		"food":     {"food", "foods", "item", "food item", "ingredient", "ingredients", "name"},
		"quantity": {"quantity", "qty", "amount", "serving", "portion"},
		"unit":     {"unit", "units", "uom", "measure"},
		"protein":  {"protein", "proteins", "prot", "p", "protein g"},
		"carbs":    {"carbs", "carb", "carbohydrate", "carbohydrates", "c", "carbs g"},
		"fat":      {"fat", "fats", "f", "fat g", "total fat"},
	}
)

var (
	bracketed = regexp.MustCompile(`[(\[].*?[)\]]`)
	nonWord   = regexp.MustCompile(`[^a-z0-9]+`)
)

// normalizeHeader folds "Protein (g)", "REST_seconds" and " Exercise  Name " to
// "protein", "rest seconds" and "exercise name".
func normalizeHeader(s string) string {
	s = strings.ToLower(s)
	s = bracketed.ReplaceAllString(s, " ")
	s = nonWord.ReplaceAllString(s, " ")
	return strings.Join(strings.Fields(s), " ")
}

// mapHeader finds each known column in the header row. Columns it does not
// recognise are ignored, so a sheet with a "Video link" or "Week 2" column
// still imports.
func mapHeader(header []string, columns map[string][]string) map[string]int {
	lookup := map[string]string{}
	for field, aliases := range columns {
		for _, a := range aliases {
			lookup[a] = field
		}
	}

	found := map[string]int{}
	for i, cell := range header {
		field, ok := lookup[normalizeHeader(cell)]
		if !ok {
			continue
		}
		if _, dup := found[field]; !dup {
			found[field] = i
		}
	}
	return found
}

// splitHeader returns the first non-blank row as the header and the rest as
// data. A title line above the header ("Coach Sam — Block 3") is common enough
// that the header is looked for in the first few rows rather than only the
// first.
func splitHeader(rows [][]string, columns map[string][]string, required string) (map[string]int, [][]string, string, error) {
	var title string
	for i, row := range rows {
		if i > 5 {
			break
		}
		if blankRow(row) {
			continue
		}
		cols := mapHeader(row, columns)
		if _, ok := cols[required]; ok {
			return cols, rows[i+1:], title, nil
		}
		if title == "" && nonBlankCells(row) == 1 {
			title = firstNonBlank(row)
		}
	}

	return nil, nil, "", refuse(ReasonNotAPlan, fmt.Sprintf(
		"This sheet needs a header row with a %q column. Expected columns: %s.",
		titleCase(required), expectedColumns(columns)))
}

// WorkoutFromRows maps a spreadsheet onto a workout draft.
func WorkoutFromRows(filename string, rows [][]string) (WorkoutDraft, error) {
	cols, data, title, err := splitHeader(rows, workoutColumns, "exercise")
	if err != nil {
		return WorkoutDraft{}, err
	}

	var (
		out     []WorkoutRow
		lastDay string
	)
	for _, row := range data {
		if blankRow(row) {
			continue
		}
		r := WorkoutRow{
			Day:      cell(row, cols, "day"),
			Exercise: cell(row, cols, "exercise"),
			Sets:     cell(row, cols, "sets"),
			Reps:     cell(row, cols, "reps"),
			Load:     cell(row, cols, "load"),
			Rest:     cell(row, cols, "rest"),
			Notes:    cell(row, cols, "notes"),
		}
		// A day written once above its exercises, with the cells below left
		// blank, is how most sheets are laid out.
		if r.Day == "" {
			r.Day = lastDay
		}
		lastDay = r.Day
		out = append(out, r)
	}

	return buildWorkout(nameOr(title, filename), out, nil)
}

// MealFromRows maps a spreadsheet onto a meal draft.
func MealFromRows(filename string, rows [][]string) (MealDraft, error) {
	cols, data, title, err := splitHeader(rows, mealColumns, "food")
	if err != nil {
		return MealDraft{}, err
	}

	var (
		out                           []MealRow
		lastDay, lastMeal, lastOption string
	)
	for _, row := range data {
		if blankRow(row) {
			continue
		}
		r := MealRow{
			Day:      cell(row, cols, "day"),
			Meal:     cell(row, cols, "meal"),
			Option:   cell(row, cols, "option"),
			Food:     cell(row, cols, "food"),
			Quantity: cell(row, cols, "quantity"),
			Unit:     cell(row, cols, "unit"),
			Protein:  cell(row, cols, "protein"),
			Carbs:    cell(row, cols, "carbs"),
			Fat:      cell(row, cols, "fat"),
		}
		if r.Day == "" {
			r.Day = lastDay
		} else if r.Day != lastDay {
			lastMeal, lastOption = "", ""
		}
		if r.Meal == "" {
			r.Meal = lastMeal
		} else if r.Meal != lastMeal {
			lastOption = ""
		}
		// An option written once above its foods, like a day or a meal.
		if r.Option == "" && r.Meal == lastMeal {
			r.Option = lastOption
		}
		lastDay, lastMeal, lastOption = r.Day, r.Meal, r.Option
		out = append(out, r)
	}

	return buildMeal(MealReading{Name: nameOr(title, filename), Rows: out})
}

func cell(row []string, cols map[string]int, field string) string {
	i, ok := cols[field]
	if !ok || i >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[i])
}

func blankRow(row []string) bool { return nonBlankCells(row) == 0 }

func nonBlankCells(row []string) int {
	n := 0
	for _, c := range row {
		if strings.TrimSpace(c) != "" {
			n++
		}
	}
	return n
}

func firstNonBlank(row []string) string {
	for _, c := range row {
		if c = strings.TrimSpace(c); c != "" {
			return c
		}
	}
	return ""
}

func expectedColumns(columns map[string][]string) string {
	order := []string{"day", "exercise", "sets", "reps", "load", "rest", "notes"}
	if _, meal := columns["food"]; meal {
		order = []string{"day", "meal", "food", "quantity", "unit", "protein", "carbs", "fat"}
	}
	return strings.Join(order, ", ")
}

func titleCase(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// nameOr names a plan after its title line, or failing that its file, so the
// review screen never opens on an empty name field for the person to fill in.
func nameOr(title, filename string) string {
	if title = strings.TrimSpace(title); title != "" {
		return title
	}
	base := strings.TrimSuffix(filename, fileExt(filename))
	base = strings.NewReplacer("_", " ", "-", " ").Replace(base)
	return strings.Join(strings.Fields(base), " ")
}

func fileExt(name string) string {
	if i := strings.LastIndexByte(name, '.'); i > 0 {
		return name[i:]
	}
	return ""
}
