package planimport

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/FACorreiaa/go-utils/pkg/util"
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

// typicalCount is what one of a counted portion usually weighs, for when
// the catalog has no serving size to go by. Plans written by dietitians count
// in these far more than in grams; without a weight the line would be lost.
// Every use is flagged as an estimate.
var typicalCount = map[string]float64{
	"slice": 30, "slices": 30, "fatia": 30, "fatias": 30,
	"piece": 120, "pieces": 120, "peça": 120, "peças": 120, "peca": 120, "pecas": 120,
	"unit": 100, "units": 100, "unidade": 100, "unidades": 100,
	"can": 120, "cans": 120, "lata": 120, "latas": 120,
	"yogurt": 125, "yogurts": 125, "yoghurt": 125, "iogurte": 125, "iogurtes": 125,
	"egg": 50, "eggs": 50, "ovo": 50, "ovos": 50,
	"scoop": 30, "scoops": 30,
	"serving": 100, "servings": 100, "portion": 100, "portions": 100, "dose": 100, "doses": 100, "porção": 100, "porções": 100,
	"tortita": 8, "tortitas": 8, "rice cake": 8, "rice cakes": 8,
	"handful": 30, "handfuls": 30, "punhado": 30, "punhados": 30,
}

// kitchenMeasures are spoons and cups, read at their usual weight and
// flagged every time.
var kitchenMeasures = map[string]float64{
	"cup": 240, "cups": 240, "chávena": 240, "chávenas": 240, "chavena": 240, "chavenas": 240,
	"glass": 200, "glasses": 200, "copo": 200, "copos": 200,
	"tbsp": 15, "tablespoon": 15, "tablespoons": 15, "colher de sopa": 15, "colheres de sopa": 15,
	"colher de sobremesa": 10, "colheres de sobremesa": 10, "dessertspoon": 10, "dessertspoons": 10,
	"tsp": 5, "teaspoon": 5, "teaspoons": 5, "colher de chá": 5, "colheres de chá": 5, "colher de cha": 5, "colheres de cha": 5,
}

// statedTolerance is how far the catalog may disagree with the file before the
// line is flagged: 15%, and never less than 2 g, so rounding on a label does
// not cry wolf.
const statedTolerance = 0.15

// PreviewMeal resolves every food line and measures every day against the
// active target. It writes nothing.
//
// Everything it fills in — grams it could derive, the matched ingredient, each
// line's macros, each day's totals, target and overage — is recomputed on
// every call from what the person can edit: the food, the chosen ingredient,
// the grams, the weekday, the plan type and mode. The overage verdict is
// meal.CheckWrite, the rule every other change to a meal plan is held to, so
// the review screen and the save cannot disagree.
func (s *Service) PreviewMeal(ctx context.Context, userID uuid.UUID, d MealDraft) MealDraft {
	d.Normalize()
	if d.Mode == "" {
		d.Mode = string(defaultMode(d))
	}

	for di := range d.Days {
		day := &d.Days[di]
		var total meal.Macros
		for mi := range day.Meals {
			m := &day.Meals[mi]
			// Only the first option counts toward the day; the others are
			// resolved so they can be saved, and eaten instead of it.
			for fi := range m.Foods {
				food := &m.Foods[fi]
				s.resolveFood(ctx, userID, food)
				if food.Macros != nil && !food.Optional {
					total = total.Add(meal.Macros{Calories: food.Macros.Calories, ProteinG: food.Macros.ProteinG, CarbG: food.Macros.CarbG, FatG: food.Macros.FatG})
				}
			}
			for oi := range m.Alternatives {
				for fi := range m.Alternatives[oi].Foods {
					s.resolveFood(ctx, userID, &m.Alternatives[oi].Foods[fi])
				}
			}
		}
		day.Totals = macroGrams(total)
		day.Target, day.Remaining, day.Over = nil, nil, []string{}
		switch {
		case d.EveryDay:
			// Every day of the week is this day, so it is measured once,
			// as the Monday it is saved as first.
			day.Weekday = util.Ptr(int(meal.WeekOrder[0]))
		case meal.Mode(d.Mode) == meal.Easy:
			day.Weekday = nil
			if di < meal.MaxDays {
				day.Weekday = util.Ptr(int(meal.WeekOrder[di]))
			}
		}
	}

	d.HasTarget, d.CanConfirm = false, false
	active, err := s.mealPlans.ActiveTarget(ctx, userID)
	if err != nil || active == nil {
		return d
	}
	d.HasTarget = true

	settings, ok := planSettings(d)
	if !ok {
		return d
	}
	state, index := planState(d, settings)
	statuses := state.Statuses(*active)
	for di, si := range index {
		if si < 0 {
			continue
		}
		st := statuses[si]
		d.Days[di].Target = macroGrams(st.Target)
		d.Days[di].Remaining = macroGrams(st.Remaining)
		if st.IsOver() {
			d.Days[di].Over = overList(st.Over)
		}
	}
	d.CanConfirm = meal.CheckWrite(*active, nil, state, false).CanConfirm
	return d
}

// defaultMode starts a draft in the mode its days already fit: easy when they
// are unnamed or run Monday onwards in order, advanced when the file names
// weekdays an easy plan could not have.
func defaultMode(d MealDraft) meal.Mode {
	for i, day := range d.Days {
		if day.Weekday != nil && (i >= meal.MaxDays || *day.Weekday != int(meal.WeekOrder[i])) {
			return meal.Advanced
		}
	}
	return meal.Easy
}

// planSettings reads the draft's choices, reporting false while they are not
// yet a valid plan: a plan type to pick, or a custom share without one.
func planSettings(d MealDraft) (meal.PlanSettings, bool) {
	settings := meal.PlanSettings{Type: meal.PlanType(d.PlanType), CustomCarbPct: d.CustomCarbPct, Mode: meal.Mode(d.Mode)}
	switch {
	case !settings.Type.Valid() || !settings.Mode.Valid():
		return settings, false
	case settings.Type == meal.Custom && (settings.Mode != meal.Advanced || settings.CustomCarbPct == nil):
		return settings, false
	}
	return settings, true
}

// planState is the draft as the overage rule sees it. index maps each draft
// day to its place in state.Days, or -1 for a day with no weekday yet.
func planState(d MealDraft, settings meal.PlanSettings) (meal.PlanState, []int) {
	state := meal.PlanState{Settings: settings}
	index := make([]int, len(d.Days))
	for i, day := range d.Days {
		index[i] = -1
		if day.Weekday == nil {
			continue
		}
		consumed := meal.Macros{}
		if day.Totals != nil {
			consumed = meal.Macros{Calories: day.Totals.Calories, ProteinG: day.Totals.ProteinG, CarbG: day.Totals.CarbG, FatG: day.Totals.FatG}
		}
		index[i] = len(state.Days)
		// Each day gets its own id: CheckWrite tells days apart by id, and
		// there are no stored ones yet.
		state.Days = append(state.Days, meal.DayState{ID: uuid.New(), Weekday: time.Weekday(*day.Weekday), Consumed: consumed})
	}
	return state, index
}

func overList(o meal.Macros) []string {
	out := []string{}
	for _, m := range []struct {
		label string
		over  float64
	}{{"protein", o.ProteinG}, {"carbs", o.CarbG}, {"fat", o.FatG}} {
		if m.over >= 0.5 {
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
				f.IngredientID = util.Ptr(match.ID)
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
	unit := normalizeUnit(f.Unit)
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
	_, typical := typicalCount[unit]
	if (countUnits[unit] || typical) && ingredient != nil && ingredient.ServingSizeGrams > 0 {
		g := qty * ingredient.ServingSizeGrams
		label := f.Unit
		if label == "" {
			label = "serving"
		}
		f.Estimated = true
		f.Flags = appendOnce(f.Flags, fmt.Sprintf("Assumed 1 %s = %g g (the catalog's serving). Check it.", label, ingredient.ServingSizeGrams))
		return &g
	}
	if per, ok := typicalCount[unit]; ok {
		return estimatedGrams(f, qty, per)
	}
	if per, ok := kitchenMeasures[unit]; ok {
		return estimatedGrams(f, qty, per)
	}
	return nil
}

// estimatedGrams weighs a counted or measured amount at its usual weight and
// says so on the line.
func estimatedGrams(f *FoodDraft, qty, per float64) *float64 {
	g := qty * per
	f.Estimated = true
	f.Flags = appendOnce(f.Flags, fmt.Sprintf("Assumed 1 %s ≈ %g g. Check it.", strings.TrimSpace(f.Unit), per))
	return &g
}

// normalizeUnit folds "G.", " kg" and "Slices" to the keys the unit tables use.
func normalizeUnit(unit string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(unit), "."))
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

// importPlan is a reviewed draft in the shape meals.CreatePlan takes, plus
// the personal ingredients to create first.
type importPlan struct {
	input meals.MealPlanInput
	// days are the drafted days, one per day of the draft. An every-day plan
	// has one, saved as seven copies by daysToSave.
	days     []meals.DayDraft
	everyDay bool
	// mine lists the portions whose ingredient is still to be created, by
	// position in days, with what to create. Listing them against the
	// drafted day rather than its copies creates each one once.
	mine []minePortion
}

// minePortion is a portion of days[day].Meals[meal] waiting for its personal
// ingredient. option 0 is the slot's default; option N is Alternatives[N-1].
type minePortion struct {
	day, meal, option, portion int
	ingredient                 meals.IngredientInput
}

// setIngredient fills in the id of the personal ingredient created for m.
func (p importPlan) setIngredient(m minePortion, id uuid.UUID) {
	slot := &p.days[m.day].Meals[m.meal]
	portions := slot.Portions
	if m.option > 0 {
		portions = slot.Alternatives[m.option-1].Portions
	}
	portions[m.portion].IngredientID = id
}

// daysToSave is the days meals.CreatePlan stores: the drafted days, or seven
// independent copies of an every-day plan's one day.
func (p importPlan) daysToSave() []meals.DayDraft {
	if !p.everyDay || len(p.days) != 1 {
		return p.days
	}
	out := make([]meals.DayDraft, len(meal.WeekOrder))
	for i := range out {
		out[i] = copyDay(p.days[0])
	}
	return out
}

func copyDay(d meals.DayDraft) meals.DayDraft {
	out := meals.DayDraft{Meals: make([]meals.MealDraft, len(d.Meals))}
	for i, m := range d.Meals {
		c := m
		c.Portions = slices.Clone(m.Portions)
		c.Alternatives = make([]meals.MealOptionDraft, len(m.Alternatives))
		for j, alt := range m.Alternatives {
			c.Alternatives[j] = meals.MealOptionDraft{Label: alt.Label, Portions: slices.Clone(alt.Portions)}
		}
		out.Meals[i] = c
	}
	return out
}

// draftOption is one option of a meal on its way to being saved.
type draftOption struct {
	label    string
	portions []meals.MealIngredientInput
	// mine is, per entry, the index in portions of a food still to be
	// created as a personal ingredient, and what to create.
	mine []minePortion
}

// mealPlanFromDraft converts a previewed draft into what meals.CreatePlan
// stores, or reports what still needs the person's attention.
func mealPlanFromDraft(d MealDraft) (importPlan, []string) {
	var problems []string
	settings, ok := planSettings(d)
	if !ok {
		problems = append(problems, "Choose a carb type for the plan.")
	}
	if !d.HasTarget {
		problems = append(problems, "Work out your macro target in the calculator first; meal plans are built on it.")
	}
	if len(d.Days) > meal.MaxDays {
		problems = append(problems, fmt.Sprintf("A plan holds at most %d days; remove %d.", meal.MaxDays, len(d.Days)-meal.MaxDays))
	}
	if d.EveryDay && len(d.Days) != 1 {
		problems = append(problems, fmt.Sprintf("A plan eaten every day has exactly one day; this one has %d.", len(d.Days)))
	}

	// Notes past what meals keeps are cut, not refused: the meals are the
	// import, and a long page of advice should not cost them.
	notes := clip(strings.TrimSpace(d.Notes), meals.MaxPlanNotesRunes)
	out := importPlan{input: meals.MealPlanInput{Name: d.Name, Settings: settings, Notes: notes}, everyDay: d.EveryDay}
	switch {
	case d.EveryDay && settings.Mode == meal.Advanced:
		out.input.Weekdays = slices.Clone(meal.WeekOrder)
	case d.EveryDay:
		out.input.DayCount = len(meal.WeekOrder)
	case settings.Mode == meal.Easy:
		out.input.DayCount = len(d.Days)
	}

	for di, day := range d.Days {
		label := day.Label
		if label == "" {
			label = fmt.Sprintf("day %d", di+1)
		}
		if settings.Mode == meal.Advanced && !d.EveryDay {
			if day.Weekday == nil {
				problems = append(problems, fmt.Sprintf("Choose which day of the week %s is.", label))
				continue
			}
			out.input.Weekdays = append(out.input.Weekdays, time.Weekday(*day.Weekday))
		}

		dd := meals.DayDraft{}
		for mi, m := range day.Meals {
			name := strings.TrimSpace(m.Name)
			if name == "" {
				name = fmt.Sprintf("Meal %d", mi+1)
			}
			var opts []draftOption
			for oi, opt := range m.Options() {
				where := label + ", " + name
				if len(m.Alternatives) > 0 {
					where += " (" + m.OptionName(oi) + ")"
				}
				o := draftOption{label: clip(strings.TrimSpace(opt.Label), meals.MaxOptionLabelRunes)}
				for _, f := range opt.Foods {
					if !f.Ready() {
						problems = append(problems, fmt.Sprintf("%s: %q isn't ready — %s", where, f.Food, strings.Join(f.Checks, " ")))
						continue
					}
					portion := meals.MealIngredientInput{QuantityGrams: *f.Grams, SourceText: f.SourceText, Estimated: f.Estimated, Optional: f.Optional}
					if f.IngredientID != nil {
						portion.IngredientID = *f.IngredientID
					} else {
						o.mine = append(o.mine, minePortion{portion: len(o.portions), ingredient: personalIngredient(f)})
					}
					o.portions = append(o.portions, portion)
				}
				// An option left with no food is dropped, and the next one
				// takes its place: a slot is never saved around an empty default.
				if len(o.portions) > 0 {
					opts = append(opts, o)
				}
			}
			if len(opts) == 0 {
				continue
			}

			md := meals.MealDraft{Name: name, OptionLabel: opts[0].label, Portions: opts[0].portions}
			for oi, o := range opts {
				if oi > 0 {
					// The meals service needs every alternative told apart by
					// a label; one the file left unnamed is named by its place.
					altLabel := o.label
					if altLabel == "" {
						altLabel = fmt.Sprintf("Option %d", oi+1)
					}
					md.Alternatives = append(md.Alternatives, meals.MealOptionDraft{Label: altLabel, Portions: o.portions})
				}
				for _, mp := range o.mine {
					mp.day, mp.meal, mp.option = len(out.days), len(dd.Meals), oi
					out.mine = append(out.mine, mp)
				}
			}
			dd.Meals = append(dd.Meals, md)
		}
		out.days = append(out.days, dd)
	}
	return out, problems
}

// personalIngredient scales the file's stated macros for this portion to the
// per-100 g profile every ingredient is stored as.
func personalIngredient(f FoodDraft) meals.IngredientInput {
	scale := 100 / *f.Grams
	p, c, fat := *f.StatedProteinG*scale, *f.StatedCarbG*scale, *f.StatedFatG*scale
	return meals.IngredientInput{
		Name:             f.Food,
		ServingSizeGrams: *f.Grams,
		Per100g:          meals.Macros{ProteinG: p, CarbG: c, FatG: fat, Calories: 4*p + 4*c + 9*fat},
	}
}

// clip cuts s to at most limit runes, the last of them an ellipsis when
// anything was cut, so a file's overlong text still fits what meals stores.
func clip(s string, limit int) string {
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	return strings.TrimSpace(string([]rune(s)[:limit-1])) + "…"
}

func appendOnce(list []string, s string) []string {
	for _, existing := range list {
		if existing == s {
			return list
		}
	}
	return append(list, s)
}
