package planimport

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/meals"
	"github.com/NorthAIProject/north-client/internal/meals/meal"
)

// candidateLimit caps the ingredients offered for one food line.
const candidateLimit = 8

// gramsPer converts the units that are exact. Volume is deliberately absent:
// a cup of oats and a cup of milk weigh different amounts, and choosing one
// would be inventing the other.
var gramsPer = map[string]float64{
	"": 0, "g": 1, "gr": 1, "gram": 1, "grams": 1, "gramm": 1,
	"kg": 1000, "kilo": 1000, "kilos": 1000, "kilogram": 1000, "kilograms": 1000,
	"oz": 28.349523125, "ounce": 28.349523125, "ounces": 28.349523125,
	"lb": 453.59237, "lbs": 453.59237, "pound": 453.59237, "pounds": 453.59237,
}

// liquidUnits are read as grams of water. Close for most drinks and flagged
// every time, so the person decides whether it is close enough.
var liquidUnits = map[string]float64{"ml": 1, "milliliter": 1, "millilitre": 1, "milliliters": 1, "millilitres": 1, "l": 1000, "liter": 1000, "litre": 1000, "liters": 1000, "litres": 1000}

// countUnits are portions the catalog's serving size can weigh.
var countUnits = map[string]bool{
	"": true, "x": true, "piece": true, "pieces": true, "pc": true, "pcs": true, "each": true, "unit": true, "units": true,
	"serving": true, "servings": true, "portion": true, "portions": true, "slice": true, "slices": true,
	"scoop": true, "scoops": true, "egg": true, "eggs": true, "medium": true, "large": true, "small": true,
}

// statedTolerance is how far the catalog may disagree with the file before the
// line is flagged: 15%, and never less than 2 g, so rounding on a label does
// not cry wolf.
const statedTolerance = 0.15

// PreviewMeal resolves every food line and measures every day against the
// active target. It writes nothing.
//
// Everything it fills in — grams it could derive, the matched ingredient, each
// line's macros, each day's totals and overage — is recomputed on every call,
// from what the person can edit: the food, the chosen ingredient, the grams,
// the weekday, the plan type. That makes it the one place the arithmetic
// happens, for the review screen and for the commit alike.
func (s *Service) PreviewMeal(ctx context.Context, userID uuid.UUID, d MealDraft) MealDraft {
	d.Normalize()
	totals := map[int]meals.Macros{}
	dayTotals := make([]meals.Macros, len(d.Days))

	for di := range d.Days {
		day := &d.Days[di]
		for mi := range day.Meals {
			for fi := range day.Meals[mi].Foods {
				food := &day.Meals[mi].Foods[fi]
				s.resolveFood(ctx, userID, food)
				if food.Macros != nil {
					dayTotals[di] = dayTotals[di].Add(meal.Macros{Calories: food.Macros.Calories, ProteinG: food.Macros.ProteinG, CarbG: food.Macros.CarbG, FatG: food.Macros.FatG})
				}
			}
		}
		day.Totals = macroGrams(dayTotals[di])
		day.Target, day.Remaining, day.Over = nil, nil, []string{}
		if day.Weekday != nil {
			totals[*day.Weekday] = totals[*day.Weekday].Add(dayTotals[di])
		}
	}

	checks, ok := s.mealPlans.CheckDays(ctx, userID, d.PlanType, d.CustomCarbPct, totals, s.goals)
	if !ok {
		return d
	}
	for di := range d.Days {
		day := &d.Days[di]
		if day.Weekday == nil {
			continue
		}
		c := checks[*day.Weekday]
		day.Target = &MacroGrams{Calories: c.Target.Calories, ProteinG: c.Target.ProteinG, CarbG: c.Target.CarbG, FatG: c.Target.FatG}
		day.Remaining = &MacroGrams{
			Calories: c.Target.Calories - c.Totals.Calories,
			ProteinG: c.Target.ProteinG - c.Totals.ProteinG,
			CarbG:    c.Target.CarbG - c.Totals.CarbG,
			FatG:     c.Target.FatG - c.Totals.FatG,
		}
		day.Over = overList(c.Overage)
	}
	return d
}

func overList(o meal.Overage) []string {
	out := []string{}
	for _, m := range []struct {
		label string
		over  float64
	}{{"protein", o.ProteinG}, {"carbs", o.CarbG}, {"fat", o.FatG}} {
		if m.over > 0.01 {
			out = append(out, fmt.Sprintf("%s over by %.0f g", m.label, math.Ceil(m.over)))
		}
	}
	return out
}

// resolveFood fills one line's ingredient, grams and macros, and lists what is
// still missing in Checks.
func (s *Service) resolveFood(ctx context.Context, userID uuid.UUID, f *FoodDraft) {
	f.Checks = []string{}
	f.Macros = nil
	f.MatchedName = ""
	if f.Candidates == nil {
		f.Candidates = []Candidate{}
	}

	var ingredient *meals.Ingredient
	if f.IngredientID != nil {
		ing, err := s.ingredients.Get(ctx, *f.IngredientID, userID)
		if err != nil {
			// An id this account cannot see, or one deleted since the parse.
			f.IngredientID = nil
			f.Checks = append(f.Checks, "The chosen ingredient isn't available. Pick another.")
		} else {
			ingredient = &ing
			f.SaveAsMine = false
		}
	}
	if ingredient == nil && !f.SaveAsMine && len(f.Candidates) == 0 {
		match, found, err := s.ingredients.Offer(ctx, userID, f.Food, candidateLimit)
		if err == nil {
			for _, c := range found {
				f.Candidates = append(f.Candidates, Candidate{ID: c.ID, Name: c.Name})
			}
			if match != nil {
				ingredient = match
				id := match.ID
				f.IngredientID = &id
			}
		}
	}
	if ingredient != nil {
		f.MatchedName = ingredient.Name
	}

	if f.Grams == nil {
		f.Grams = s.derivedGrams(f, ingredient)
	}
	if f.Grams != nil && *f.Grams <= 0 {
		f.Grams = nil
	}

	switch {
	case f.Grams == nil:
		f.Checks = append(f.Checks, "Enter the weight in grams.")
	case ingredient != nil:
		m := ingredient.MacrosFor(*f.Grams)
		f.Macros = macroGrams(m)
		f.Checks = append(f.Checks, statedMismatch(f, m, ingredient.Name)...)
	case f.SaveAsMine:
		if !f.CanSaveAsMine() {
			f.SaveAsMine = false
			f.Checks = append(f.Checks, "Saving as your own food needs protein, carbs and fat from the file.")
		} else {
			f.Macros = &MacroGrams{
				ProteinG: *f.StatedProteinG, CarbG: *f.StatedCarbG, FatG: *f.StatedFatG,
				Calories: 4**f.StatedProteinG + 4**f.StatedCarbG + 9**f.StatedFatG,
			}
		}
	}

	if ingredient == nil && !f.SaveAsMine {
		if len(f.Candidates) == 0 {
			f.Checks = append(f.Checks, fmt.Sprintf("Nothing in the catalog matches %q.", f.Food))
		} else {
			f.Checks = append(f.Checks, "Pick the ingredient this is.")
		}
	}
}

// derivedGrams converts what the file said into grams, flagging every
// conversion that is an assumption rather than arithmetic. The flag goes in
// Flags rather than Checks because it describes how the number was first
// arrived at, and has to survive the later previews that no longer derive it.
func (s *Service) derivedGrams(f *FoodDraft, ingredient *meals.Ingredient) *float64 {
	if f.Quantity == nil {
		return nil
	}
	unit := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(f.Unit), "."))
	qty := *f.Quantity

	if per, ok := gramsPer[unit]; ok && per > 0 {
		g := qty * per
		return &g
	}
	if per, ok := liquidUnits[unit]; ok {
		g := qty * per
		f.Flags = appendOnce(f.Flags, fmt.Sprintf("Read %g %s as %g g. Change it if this isn't a watery drink.", qty, f.Unit, g))
		return &g
	}
	if countUnits[unit] && ingredient != nil && ingredient.ServingSizeGrams > 0 {
		g := qty * ingredient.ServingSizeGrams
		label := f.Unit
		if label == "" {
			label = "serving"
		}
		f.Flags = appendOnce(f.Flags, fmt.Sprintf("Assumed 1 %s = %g g (the catalog's serving). Check it.", label, ingredient.ServingSizeGrams))
		return &g
	}
	return nil
}

func statedMismatch(f *FoodDraft, m meal.Macros, name string) []string {
	var out []string
	for _, c := range []struct {
		label  string
		stated *float64
		got    float64
	}{
		{"protein", f.StatedProteinG, m.ProteinG},
		{"carbs", f.StatedCarbG, m.CarbG},
		{"fat", f.StatedFatG, m.FatG},
	} {
		if c.stated == nil {
			continue
		}
		if math.Abs(c.got-*c.stated) > math.Max(2, *c.stated*statedTolerance) {
			out = append(out, fmt.Sprintf("The file says %.0f g %s; %s at %.0f g has %.0f g.", *c.stated, c.label, name, *f.Grams, c.got))
		}
	}
	return out
}

// mealPlanFromDraft converts a reviewed draft into what meals.ImportPlan
// stores, or reports what still needs the person's attention.
func mealPlanFromDraft(d MealDraft) (meals.ImportedMealPlan, []string) {
	var problems []string
	out := meals.ImportedMealPlan{Name: d.Name, PlanType: d.PlanType, CustomCarbPct: d.CustomCarbPct}
	numbers := map[int]int{}

	for _, day := range d.Days {
		label := day.Label
		if label == "" {
			label = "a day"
		}
		if day.Weekday == nil {
			problems = append(problems, fmt.Sprintf("Choose which day of the week %s is.", label))
			continue
		}
		for _, m := range day.Meals {
			numbers[*day.Weekday]++
			name := strings.TrimSpace(m.Name)
			if name == "" {
				name = fmt.Sprintf("Meal %d", numbers[*day.Weekday])
			}
			im := meals.ImportedMeal{Name: name, Weekday: *day.Weekday}
			for _, f := range m.Foods {
				if !f.Ready() {
					problems = append(problems, fmt.Sprintf("%s, %s: %q isn't ready — %s", time.Weekday(*day.Weekday), name, f.Food, strings.Join(f.Checks, " ")))
					continue
				}
				item := meals.ImportedItem{QuantityGrams: *f.Grams}
				if f.IngredientID != nil {
					item.IngredientID = *f.IngredientID
				} else {
					item.NewIngredient = personalIngredient(f)
				}
				im.Items = append(im.Items, item)
			}
			if len(im.Items) > 0 {
				out.Meals = append(out.Meals, im)
			}
		}
	}
	return out, problems
}

// personalIngredient scales the file's stated macros for this portion to the
// per-100 g profile every ingredient is stored as.
func personalIngredient(f FoodDraft) *meals.IngredientInput {
	scale := 100 / *f.Grams
	p, c, fat := *f.StatedProteinG*scale, *f.StatedCarbG*scale, *f.StatedFatG*scale
	return &meals.IngredientInput{
		Name:             f.Food,
		ServingSizeGrams: *f.Grams,
		Per100g:          meals.Macros{ProteinG: p, CarbG: c, FatG: fat, Calories: 4*p + 4*c + 9*fat},
	}
}

func appendOnce(list []string, s string) []string {
	for _, existing := range list {
		if existing == s {
			return list
		}
	}
	return append(list, s)
}
