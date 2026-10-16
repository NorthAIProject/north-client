package planimport

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/meals"
	"github.com/NorthAIProject/north-client/internal/meals/meal"
	"github.com/NorthAIProject/north-client/internal/quota"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/users"
)

// ErrGenerateQuotaUsed is GenerateMealForCoach's refusal once the person has
// built every meal plan their window allows.
var ErrGenerateQuotaUsed = errors.New("you have built all the meal plans you can for now")

// DietNames lists a person's standing diet preferences by name.
type DietNames func(ctx context.Context, userID uuid.UUID) ([]string, error)

// underTarget is how far under its target a day may land and still count as
// fitting: a plan that leaves a fifth of the day's protein uneaten has not
// been built to the target, it has been built under it.
const underTarget = 0.85

// GenerateMeal builds a meal plan from what the person asked for and returns
// it as a draft for the same review an import goes through. It saves the
// intake, never the plan: the plan is saved by CommitMeal once they have
// looked at it.
//
// The intake is saved first, even though drafting may fail, for the reason
// workouts gives: what they told us is worth keeping, and it fills the form
// on their next try.
func (s *Service) GenerateMeal(ctx context.Context, user users.User, in meals.MealPlanIntake) (MealDraft, error) {
	if s.planner == nil {
		return MealDraft{}, apperr.Wrap(apperr.ErrUnavailable, "meal plan generation is not configured")
	}
	in, err := meals.ValidateMealPlanIntake(in)
	if err != nil {
		return MealDraft{}, err
	}

	active, err := s.mealPlans.ActiveTarget(ctx, user.ID)
	if err != nil {
		return MealDraft{}, err
	}
	if active == nil {
		return MealDraft{}, apperr.FieldErrors{}.Add("macro_target", "Work out your macro target in the calculator first; meal plans are built on it.")
	}

	if err = s.begin(user.ID); err != nil {
		return MealDraft{}, err
	}
	defer s.end(user.ID)

	if in, err = s.mealPlans.SaveIntake(ctx, user.ID, in); err != nil {
		return MealDraft{}, err
	}

	req := MealPlanRequest{
		Intake: in,
		Target: meal.ResolveDayTarget(*active, meal.PlanSettings{Type: in.PlanType, Mode: meal.Easy}, meal.DayOverride{}),
	}
	if s.diets != nil {
		// Preferences steer the plan; without them it is still a plan.
		if names, err := s.diets(ctx, user.ID); err == nil {
			req.Diets = names
		}
	}

	return s.planner.PlanMeal(ctx, user, req, func(ctx context.Context, d MealDraft) (MealDraft, []string) {
		if problems := checkGenerated(d, in); len(problems) > 0 {
			return d, problems
		}
		d = s.PreviewMeal(ctx, user.ID, d)
		return d, checkFit(d)
	})
}

// checkGenerated is what can be wrong with a drafted plan before any food is
// looked up: its shape, and the foods the person ruled out. Problems are
// English, for the model.
func checkGenerated(d MealDraft, in meals.MealPlanIntake) []string {
	var problems []string
	if strings.TrimSpace(d.Name) == "" {
		problems = append(problems, "the plan has no name")
	}
	if len(d.Days) != in.Days {
		problems = append(problems, fmt.Sprintf("the plan has %d days but they asked for exactly %d", len(d.Days), in.Days))
	}

	for di, day := range d.Days {
		where := dayName(day, di)
		counted := 0
		for _, m := range day.Meals {
			if !m.Optional {
				counted++
			}
		}
		if counted != in.MealsPerDay {
			problems = append(problems, fmt.Sprintf("%s has %d meals but they asked for exactly %d a day", where, counted, in.MealsPerDay))
		}
		for _, m := range day.Meals {
			if len(m.Foods) == 0 {
				problems = append(problems, fmt.Sprintf("%s, %s has no foods", where, m.Name))
			}
			for _, opt := range m.Options() {
				for _, f := range opt.Foods {
					if f.Grams == nil || *f.Grams <= 0 {
						problems = append(problems, fmt.Sprintf("%q in %s, %s has no weight in grams", f.Food, where, m.Name))
					}
					if excluded, ok := excludedBy(f.Food, in.Exclusions); ok {
						problems = append(problems, fmt.Sprintf("%q in %s, %s is %s, which they do not eat", f.Food, where, m.Name, excluded))
					}
				}
			}
		}
	}
	return problems
}

// checkFit is what is wrong with a previewed plan: foods the catalog doesn't
// know, so their macros can't be counted, and days off their target.
func checkFit(d MealDraft) []string {
	var problems []string
	for di, day := range d.Days {
		where := dayName(day, di)
		for _, m := range day.Meals {
			for _, opt := range m.Options() {
				for _, f := range opt.Foods {
					if f.IngredientID == nil {
						problems = append(problems, fmt.Sprintf("%q in %s, %s is not a catalog food; use a catalog name exactly as written", f.Food, where, m.Name))
					}
				}
			}
		}
		if day.Totals == nil || day.Target == nil {
			continue
		}
		t, g := *day.Totals, *day.Target
		for _, m := range []struct {
			name          string
			total, target float64
		}{{"protein", t.ProteinG, g.ProteinG}, {"carbs", t.CarbG, g.CarbG}, {"fat", t.FatG, g.FatG}} {
			switch {
			case m.total > m.target+0.5:
				problems = append(problems, fmt.Sprintf("%s has %.0f g of %s, over its target of %.0f g by %.0f g",
					where, m.total, m.name, m.target, m.total-m.target))
			case m.target >= 20 && m.total < m.target*underTarget:
				problems = append(problems, fmt.Sprintf("%s has %.0f g of %s, well under its target of %.0f g",
					where, m.total, m.name, m.target))
			}
		}
	}
	return problems
}

func dayName(day MealDayDraft, i int) string {
	if strings.TrimSpace(day.Label) != "" {
		return day.Label
	}
	return fmt.Sprintf("Day %d", i+1)
}

// exclusionTerm is a word that names an excluded food, and the words that,
// beside it, mean it is something else: almond milk is not dairy.
type exclusionTerm struct {
	word   string
	unless []string
}

// plantNotDairy are what turn "milk", "butter" or "cream" into something
// without dairy in it.
var plantNotDairy = []string{"almond", "peanut", "cashew", "coconut", "oat", "soy", "rice", "nut", "seed", "sunflower", "cocoa", "apple", "hazelnut", "sesame", "tahini"}

// exclusionGroups are the common categories people exclude, and the foods
// that belong to them. English only: an exclusion in another language is
// still in the prompt, and only an exact word match is checked here.
var exclusionGroups = map[string][]exclusionTerm{
	"dairy": {
		{word: "milk", unless: plantNotDairy},
		{word: "butter", unless: plantNotDairy},
		{word: "cream", unless: plantNotDairy},
		{word: "cheese"},
		{word: "yogurt"},
		{word: "yoghurt"},
		{word: "whey"},
		{word: "casein"},
		{word: "kefir"},
		{word: "ricotta"},
		{word: "mozzarella"},
		{word: "parmesan"},
		{word: "cheddar"},
		{word: "feta"},
		{word: "cottage"},
		{word: "ghee"},
	},
	"lactose": {{word: "milk", unless: plantNotDairy}, {word: "cream", unless: plantNotDairy}, {word: "yogurt"}, {word: "yoghurt"}, {word: "cheese"}, {word: "whey"}},
	"gluten": {
		{word: "wheat", unless: []string{"buckwheat"}},
		{word: "bread"},
		{word: "pasta"},
		{word: "couscous"},
		{word: "barley"},
		{word: "rye"},
		{word: "seitan"},
		{word: "bagel"},
		{word: "baguette"},
		{word: "croissant"},
		{word: "noodle", unless: []string{"rice"}},
		{word: "spaghetti"},
		{word: "bulgur"},
	},
	"meat": {
		{word: "beef"},
		{word: "pork"},
		{word: "chicken"},
		{word: "turkey"},
		{word: "lamb"},
		{word: "ham"},
		{word: "bacon"},
		{word: "sausage"},
		{word: "veal"},
		{word: "steak"},
		{word: "duck"},
		{word: "chorizo"},
		{word: "salami"},
	},
	"red meat": {{word: "beef"}, {word: "pork"}, {word: "lamb"}, {word: "veal"}, {word: "steak"}, {word: "ham"}, {word: "bacon"}},
	"pork":     {{word: "pork"}, {word: "ham"}, {word: "bacon"}, {word: "chorizo"}, {word: "salami"}, {word: "prosciutto"}},
	"fish": {
		{word: "salmon"},
		{word: "tuna"},
		{word: "cod"},
		{word: "sardine"},
		{word: "mackerel"},
		{word: "trout"},
		{word: "anchovy"},
		{word: "anchovies"},
		{word: "tilapia"},
		{word: "hake"},
		{word: "haddock"},
		{word: "herring"},
	},
	"shellfish": {{word: "shrimp"}, {word: "prawn"}, {word: "crab"}, {word: "lobster"}, {word: "mussel"}, {word: "clam"}, {word: "oyster"}, {word: "scallop"}, {word: "squid"}, {word: "octopus"}},
	"nuts": {
		{word: "almond"},
		{word: "walnut"},
		{word: "cashew"},
		{word: "pecan"},
		{word: "pistachio"},
		{word: "hazelnut"},
		{word: "macadamia"},
		{word: "peanut"},
		{word: "nut", unless: []string{"coconut", "nutmeg"}},
	},
	"soy": {{word: "soy"}, {word: "soya"}, {word: "tofu"}, {word: "tempeh"}, {word: "edamame"}, {word: "miso"}},
	"egg": {{word: "egg", unless: []string{"eggplant"}}, {word: "mayonnaise"}},
}

func init() {
	exclusionGroups["seafood"] = append(append([]exclusionTerm{}, exclusionGroups["fish"]...), exclusionGroups["shellfish"]...)
	exclusionGroups["tree nuts"] = exclusionGroups["nuts"]
	exclusionGroups["eggs"] = exclusionGroups["egg"]
}

// excludedBy reports which of the person's exclusions a food breaks: the
// exclusion itself named in the food ("peanut" in "Peanut butter"), or a food
// of an excluded group ("Greek yogurt" for "dairy").
func excludedBy(food string, exclusions []string) (string, bool) {
	words := foodWords(food)
	for _, e := range exclusions {
		key := strings.ToLower(strings.TrimSpace(e))
		if phrase := foodWords(e); len(phrase) > 0 && containsPhrase(words, phrase) {
			return e, true
		}
		for _, term := range exclusionGroups[key] {
			if containsPhrase(words, []string{stem(term.word)}) && !containsAny(words, term.unless) {
				return e, true
			}
		}
	}
	return "", false
}

// foodWords is a name's words, lower case and singular enough to compare:
// "Peanuts" and "peanut" are one word here.
func foodWords(s string) []string {
	fields := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) })
	for i, f := range fields {
		fields[i] = stem(f)
	}
	return fields
}

func stem(w string) string {
	switch {
	case len(w) > 4 && strings.HasSuffix(w, "ies"):
		return strings.TrimSuffix(w, "ies") + "y"
	case len(w) > 3 && strings.HasSuffix(w, "s") && !strings.HasSuffix(w, "ss"):
		return strings.TrimSuffix(w, "s")
	}
	return w
}

func containsPhrase(words, phrase []string) bool {
	for i := 0; i+len(phrase) <= len(words); i++ {
		match := true
		for j, p := range phrase {
			if words[i+j] != p {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func containsAny(words, others []string) bool {
	for _, a := range others {
		if containsPhrase(words, []string{stem(a)}) {
			return true
		}
	}
	return false
}

// MealIntakeFor is the person's last answers, to fill the form with, or nil.
func (s *Service) MealIntakeFor(ctx context.Context, userID uuid.UUID) (*meals.MealPlanIntake, error) {
	return s.mealPlans.LatestIntake(ctx, userID)
}

// GenerateMealForCoach builds a meal plan and saves it in one step, for the
// coach's tool, whose approval card stands in for the review screen. A plan
// only comes out of GenerateMeal once every food is matched and every day
// fits, so it saves as drafted.
//
// It runs in English, as ImportForCoach does: what it reports is read by the
// coach, not shown to the person.
func (s *Service) GenerateMealForCoach(ctx context.Context, user users.User, in meals.MealPlanIntake) (meals.MealPlan, error) {
	ctx = i18n.WithLocale(ctx, i18n.DefaultLocale)
	in, err := meals.ValidateMealPlanIntake(in)
	if err != nil {
		return meals.MealPlan{}, err
	}
	if err = s.spend(ctx, user, quota.MealPlanGenerate, ErrGenerateQuotaUsed); err != nil {
		return meals.MealPlan{}, err
	}
	d, err := s.GenerateMeal(ctx, user, in)
	if err != nil {
		return meals.MealPlan{}, err
	}
	saved, _, err := s.CommitMeal(ctx, user, d, false)
	return saved, err
}
