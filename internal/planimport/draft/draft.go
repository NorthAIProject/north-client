// Package draft holds the shape of an import before anyone has confirmed it,
// shared by the import service, the JSON API and the review pages.
package draft

import (
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
// PlanType and CustomCarbPct are the person's choice on the review screen —
// the same carb preset a hand-made plan is created with — and decide the target
// each day is measured against. A file never sets them.
type MealDraft struct {
	Name          string         `json:"name"`
	PlanType      string         `json:"planType"`
	CustomCarbPct *float64       `json:"customCarbPct,omitempty"`
	Days          []MealDayDraft `json:"days"`
	Unparsed      []string       `json:"unparsed"`
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

// MealDraftMeal is one meal within a day.
type MealDraftMeal struct {
	Name  string      `json:"name"`
	Foods []FoodDraft `json:"foods"`
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
			day.Meals[j].Foods = orEmpty(day.Meals[j].Foods)
			for k := range day.Meals[j].Foods {
				f := &day.Meals[j].Foods[k]
				f.Candidates = orEmpty(f.Candidates)
				f.Flags = orEmpty(f.Flags)
				f.Checks = orEmpty(f.Checks)
			}
		}
	}
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
