// Package draft holds the shape of an import before anyone has confirmed it,
// shared by the import service, the JSON API and the review pages.
package draft

import (
	"fmt"

	"github.com/google/uuid"
)

// A draft is what a file appeared to say, before anyone has checked it.
//
// The same types travel to the review screen and back: the web form posts them
// as indexed fields, the phone as JSON. Nothing in a draft is trusted on the way
// back in — commit validates it and recomputes every number — so a client that
// edits a total has edited a display, not the plan.
//
// Optional numbers are pointers. Nil is "the file did not say", which is a
// different answer from zero, and conflating them is exactly how an import
// invents a set count.

// WorkoutDraft is a training plan read from a file.
type WorkoutDraft struct {
	Name     string            `json:"name"`
	Days     []WorkoutDayDraft `json:"days"`
	Unparsed []string          `json:"unparsed"`
}

// WorkoutDayDraft is one training day.
//
// Label is what the file called it ("Day 1", "Push", "Monday"). Weekday is the
// day of the week it will be trained on, "Monday".."Sunday", or empty until the
// person picks one. A label that already names a weekday fills it in.
type WorkoutDayDraft struct {
	Label     string          `json:"label"`
	Weekday   string          `json:"weekday"`
	Exercises []ExerciseDraft `json:"exercises"`
}

// ExerciseDraft is one exercise line.
type ExerciseDraft struct {
	Name        string   `json:"name"`
	Sets        *int     `json:"sets,omitempty"`
	Reps        string   `json:"reps"`
	Load        string   `json:"load"`
	RestSeconds *int     `json:"restSeconds,omitempty"`
	Notes       string   `json:"notes"`
	Flags       []string `json:"flags"`
}

// MealDraft is a meal plan read from a file.
//
// PlanType, CustomCarbPct and Mode are the person's choices on the review
// screen — the same settings a plan made in the app is created with — and
// decide the target each day is measured against. A file never sets them.
//
// In easy mode the days fill Monday onwards in order, as an easy plan's days
// always do, and Weekday is shown rather than chosen. In advanced mode each
// day's weekday is the person's to pick.
type MealDraft struct {
	Name          string         `json:"name"`
	PlanType      string         `json:"planType"`
	CustomCarbPct *float64       `json:"customCarbPct,omitempty"`
	Mode          string         `json:"mode"`
	Days          []MealDayDraft `json:"days"`
	Unparsed      []string       `json:"unparsed"`

	// HasTarget is false when the person has no macro target yet, so there is
	// nothing to measure the days against and nothing can be saved.
	HasTarget bool `json:"hasTarget"`
	// CanConfirm is set when some day is over and the plan is advanced: saving
	// with confirmOverage goes ahead. An easy plan that is over cannot be
	// saved until it isn't.
	CanConfirm bool `json:"canConfirm"`

	// EveryDay is set when the file named no days: its single day is eaten
	// every day of the week and is saved as seven copies of itself.
	EveryDay bool `json:"everyDay,omitempty"`
	// Notes is the file's advice, recipes and general guidance, kept beside
	// the plan rather than forced into rows.
	Notes string `json:"notes,omitempty"`
}

// MealDayDraft is one day of meals, with that day's arithmetic.
//
// Weekday is 0–6, Sunday–Saturday, matching meal.Meal, or nil until assigned.
// Totals, Target, Remaining and Over are computed by Preview and never read
// back from a client.
type MealDayDraft struct {
	Label   string          `json:"label"`
	Weekday *int            `json:"weekday,omitempty"`
	Meals   []MealDraftMeal `json:"meals"`

	Totals    *MacroGrams `json:"totals,omitempty"`
	Target    *MacroGrams `json:"target,omitempty"`
	Remaining *MacroGrams `json:"remaining,omitempty"`
	Over      []string    `json:"over"`
}

// MealDraftMeal is one meal within a day: a slot holding one or more
// interchangeable options.
//
// Foods is the first option, the one counted toward the day, and OptionLabel
// is what the file called it ("Opção 1"), or empty. Alternatives are the
// other options, in the file's order. A client that knows nothing of options
// round-trips Foods alone and still saves the first.
type MealDraftMeal struct {
	Name         string            `json:"name"`
	Foods        []FoodDraft       `json:"foods"`
	OptionLabel  string            `json:"optionLabel,omitempty"`
	Alternatives []MealDraftOption `json:"alternatives,omitempty"`
}

// MealDraftOption is a further option of a meal: eaten instead of the first,
// never counted toward the day.
type MealDraftOption struct {
	Label string      `json:"label"`
	Foods []FoodDraft `json:"foods"`
}

// Options lists the meal's options in order, the first one first. The Foods
// slices are the meal's own, so a food changed through them is changed in
// the meal; adding or removing an option is not.
func (m MealDraftMeal) Options() []MealDraftOption {
	return append([]MealDraftOption{{Label: m.OptionLabel, Foods: m.Foods}}, m.Alternatives...)
}

// OptionName is what option o (0 being the first) is shown as: its label, or
// "Option N" when the file gave it none.
func (m MealDraftMeal) OptionName(o int) string {
	opts := m.Options()
	if o >= 0 && o < len(opts) && opts[o].Label != "" {
		return opts[o].Label
	}
	return fmt.Sprintf("Option %d", o+1)
}

// FoodDraft is one food line.
//
// Food, Quantity and Unit are what the file said. Grams is the weight that will
// be stored — converted from Quantity and Unit when that is exact, flagged when
// it is an assumption, and nil when it cannot be known. The Stated macros are
// the file's own numbers, kept for display: what is stored is always the
// catalog ingredient's macros at Grams, like every other meal.
type FoodDraft struct {
	Food     string   `json:"food"`
	Quantity *float64 `json:"quantity,omitempty"`
	Unit     string   `json:"unit"`
	Grams    *float64 `json:"grams,omitempty"`

	// SourceText is the line as the file wrote it ("2 fatias pão integral"),
	// kept because Food may be a catalog stand-in for it. Estimated marks
	// Grams as the reader's estimate of a vague amount, not the file's number.
	SourceText string `json:"sourceText,omitempty"`
	Estimated  bool   `json:"estimated,omitempty"`

	StatedProteinG *float64 `json:"statedProteinG,omitempty"`
	StatedCarbG    *float64 `json:"statedCarbG,omitempty"`
	StatedFatG     *float64 `json:"statedFatG,omitempty"`

	IngredientID *uuid.UUID  `json:"ingredientId,omitempty"`
	MatchedName  string      `json:"matchedName"`
	Candidates   []Candidate `json:"candidates"`
	Macros       *MacroGrams `json:"macros,omitempty"`

	// SaveAsMine creates a personal ingredient from the stated macros when
	// nothing in the catalog matches. Only possible when the file stated all
	// three macros and a gram weight is known.
	SaveAsMine bool `json:"saveAsMine"`

	// Flags are what reading the file noticed: a cell that wasn't a number, a
	// guessed column, an assumed serving weight. Carried with the draft.
	Flags []string `json:"flags"`

	// Checks are what still stands between this line and saving it, recomputed
	// by every preview: no ingredient chosen, no weight, the catalog
	// disagreeing with the file.
	Checks []string `json:"checks"`
}

// Candidate is a catalog ingredient the person can pick for a food line.
type Candidate struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// MacroGrams is the three macros the plan is constrained by, plus calories for
// display.
type MacroGrams struct {
	Calories float64 `json:"calories"`
	ProteinG float64 `json:"proteinG"`
	CarbG    float64 `json:"carbG"`
	FatG     float64 `json:"fatG"`
}

// Normalize replaces nil slices with empty ones, so the JSON a client receives
// always has its arrays — a draft decoded from a client that left one out
// would otherwise go back as null.
func (d *WorkoutDraft) Normalize() {
	d.Days = orEmpty(d.Days)
	d.Unparsed = orEmpty(d.Unparsed)
	for i := range d.Days {
		d.Days[i].Exercises = orEmpty(d.Days[i].Exercises)
		for j := range d.Days[i].Exercises {
			d.Days[i].Exercises[j].Flags = orEmpty(d.Days[i].Exercises[j].Flags)
		}
	}
}

func (d *MealDraft) Normalize() {
	d.Days = orEmpty(d.Days)
	d.Unparsed = orEmpty(d.Unparsed)
	for i := range d.Days {
		day := &d.Days[i]
		day.Meals = orEmpty(day.Meals)
		day.Over = orEmpty(day.Over)
		for j := range day.Meals {
			m := &day.Meals[j]
			m.Foods = normalizeFoods(m.Foods)
			for o := range m.Alternatives {
				m.Alternatives[o].Foods = normalizeFoods(m.Alternatives[o].Foods)
			}
		}
	}
}

func normalizeFoods(foods []FoodDraft) []FoodDraft {
	foods = orEmpty(foods)
	for k := range foods {
		f := &foods[k]
		f.Candidates = orEmpty(f.Candidates)
		f.Flags = orEmpty(f.Flags)
		f.Checks = orEmpty(f.Checks)
	}
	return foods
}

func orEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// CanSaveAsMine reports whether the file stated enough to create a personal
// ingredient from: all three macros and a weight to scale them by.
func (f FoodDraft) CanSaveAsMine() bool {
	return f.StatedProteinG != nil && f.StatedCarbG != nil && f.StatedFatG != nil && f.Grams != nil && *f.Grams > 0
}

// Ready reports whether a line can be saved.
func (f FoodDraft) Ready() bool {
	return f.Macros != nil && f.Grams != nil && (f.IngredientID != nil || f.SaveAsMine)
}
