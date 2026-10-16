package planimport

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/prompts"
	"github.com/NorthAIProject/north-client/internal/meals"
	"github.com/NorthAIProject/north-client/internal/meals/meal"
	"github.com/NorthAIProject/north-client/internal/shared/aiattr"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	"github.com/NorthAIProject/north-client/internal/spend"
	"github.com/NorthAIProject/north-client/internal/users"
)

// MealPlanRequest is everything a meal plan is built from.
type MealPlanRequest struct {
	Intake meals.MealPlanIntake
	// Target is what each day must fit: the calculator's target at the
	// intake's plan type.
	Target meal.Macros
	// Diets are the person's standing diet preferences, by name.
	Diets []string
}

// MealCheck measures a drafted plan against the request, returning what is
// wrong with it in words the model can act on, or nothing when it fits. The
// draft it returns is the checked one, with its foods resolved.
type MealCheck func(ctx context.Context, d MealDraft) (MealDraft, []string)

// MealPlanner drafts a meal plan. A drafted plan is one check has passed;
// one that never does is an error.
//
// An interface with one production implementation, as Reader is, so the
// service is tested without a model.
type MealPlanner interface {
	PlanMeal(ctx context.Context, user users.User, req MealPlanRequest, check MealCheck) (MealDraft, error)
}

// planAttempts is how many drafts one provider may make. One more than the
// workout generator allows: fitting a day under three macro targets is
// arithmetic, and a model told exactly how far over a day is usually fixes it
// on the next pass.
const planAttempts = 3

// planTemperature is low: this is constraint satisfaction, not writing.
var planTemperature float32 = 0.2

// CatalogFoods lists the shared catalog with its macros.
type CatalogFoods func(ctx context.Context) ([]meals.CatalogFood, error)

// AIMealPlanner is the production MealPlanner.
type AIMealPlanner struct {
	runner  *ai.Runner
	model   string
	catalog CatalogFoods

	// The catalog's prompt lines, built once per process on the first plan
	// that manages to read it, as AIReader keeps its names.
	catalogMu    sync.Mutex
	catalogLines []string
	catalogDone  bool
}

// NewAIMealPlanner builds the planner. model may be empty for the chain's
// default. catalog may be nil, and the planner then names foods without it.
func NewAIMealPlanner(runner *ai.Runner, model string, catalog CatalogFoods) *AIMealPlanner {
	return &AIMealPlanner{runner: runner, model: model, catalog: catalog}
}

// generatedPlan is the model's reply. Small on purpose: foods and grams, and
// the server works out everything else, as it does for an import.
type generatedPlan struct {
	Name      string         `json:"name"`
	Rationale string         `json:"rationale"`
	Days      []generatedDay `json:"days"`
}

type generatedDay struct {
	Label string          `json:"label"`
	Meals []generatedMeal `json:"meals"`
}

type generatedMeal struct {
	Name         string            `json:"name"`
	Foods        []generatedFood   `json:"foods"`
	Alternatives []generatedOption `json:"alternatives"`
}

type generatedOption struct {
	Label string          `json:"label"`
	Foods []generatedFood `json:"foods"`
}

type generatedFood struct {
	Food  string  `json:"food"`
	Grams float64 `json:"grams"`
}

func generatedFoodSchema() *ai.Schema {
	return ai.Array("the foods, in the order they are eaten",
		ai.Object("one food", map[string]*ai.Schema{
			"food":  ai.String("the catalog name, spelled exactly as in the catalog"),
			"grams": ai.Number("the portion's weight in grams"),
		}, "food", "grams"))
}

func mealPlanSchema() *ai.Schema {
	meal := ai.Object("one meal", map[string]*ai.Schema{
		"name":  ai.String("what the meal is called, e.g. Breakfast"),
		"foods": generatedFoodSchema(),
		"alternatives": ai.Array("at most one swap with similar macros; empty for none",
			ai.Object("a swap for the whole meal", map[string]*ai.Schema{
				"label": ai.String("a short name for the swap"),
				"foods": generatedFoodSchema(),
			}, "label", "foods")),
	}, "name", "foods", "alternatives")
	day := ai.Object("one day", map[string]*ai.Schema{
		"label": ai.String("a short name for the day, e.g. Day 1"),
		"meals": ai.Array("the day's meals, in the order they are eaten", meal),
	}, "label", "meals")
	return ai.Object("a meal plan", map[string]*ai.Schema{
		"name":      ai.String("a short name for the plan"),
		"rationale": ai.String("two or three sentences on how the plan serves their objective"),
		"days":      ai.Array("the plan's days", day),
	}, "name", "rationale", "days")
}

func (p *AIMealPlanner) PlanMeal(ctx context.Context, user users.User, req MealPlanRequest, check MealCheck) (MealDraft, error) {
	// The catalog is in the system prompt and the person in the user message,
	// so the long, identical prefix can be cached across everyone's plans.
	system, err := prompts.Render(prompts.MealPlan, map[string]any{"Catalog": p.catalogFor(ctx)})
	if err != nil {
		return MealDraft{}, apperr.Wrap(err, "render the meal plan prompt")
	}
	log := middleware.FromContext(ctx)
	ctx = aiattr.WithUser(ctx, user.ID, spend.SurfaceMealPlan)

	var (
		plan         MealDraft
		lastProblems []string
	)
	_, err = p.runner.Run(ctx, ai.RunOptions{Tier: string(user.Tier)}, func(client ai.Client) error {
		// Each provider gets its own correction dialogue, for the reason the
		// workout generator gives: a correction works because it quotes the
		// exact plan that broke.
		messages := []ai.Message{ai.UserText(mealPlanRequestText(req))}
		lastProblems = nil

		for attempt := 1; attempt <= planAttempts; attempt++ {
			resp, genErr := client.Generate(ctx, ai.Request{
				Model:          p.model,
				System:         system,
				Messages:       messages,
				ResponseSchema: mealPlanSchema(),
				Temperature:    &planTemperature,
			})
			if genErr != nil {
				return apperr.Wrap(genErr, "generate meal plan")
			}

			var reply generatedPlan
			if decErr := json.Unmarshal(ai.JSONReply(resp.Text), &reply); decErr != nil {
				lastProblems = []string{"the reply was not valid JSON for the required shape"}
				log.Warn("meal plan did not decode", slog.Int("attempt", attempt), slog.Any("error", decErr))
				messages = append(messages,
					ai.ModelText(resp.Text),
					ai.UserText("That was not valid JSON matching the schema. Return the plan again, correctly."),
				)
				continue
			}

			checked, problems := check(ctx, draftFromGenerated(reply, req.Intake))
			if len(problems) == 0 {
				plan = checked
				return nil
			}

			lastProblems = problems
			log.Warn("generated meal plan broke the request",
				slog.Int("attempt", attempt), slog.Any("problems", problems))
			messages = append(messages,
				ai.ModelText(resp.Text),
				ai.UserText("That plan breaks the constraints:\n- "+strings.Join(problems, "\n- ")+
					"\n\nReturn a corrected plan that satisfies every constraint."),
			)
		}

		// ErrUnavailable so the chain moves on to a model that may manage it.
		return apperr.Wrap(apperr.ErrUnavailable,
			"could not produce a meal plan that fits: %s", strings.Join(lastProblems, "; "))
	})
	if err != nil {
		return MealDraft{}, err
	}
	return plan, nil
}

// draftFromGenerated turns the model's reply into the draft an import makes,
// so it goes through the same preview, review and save.
//
// A one-day plan is eaten every day, as an import with no days is. Several
// days are an easy plan, Monday onwards.
func draftFromGenerated(g generatedPlan, in meals.MealPlanIntake) MealDraft {
	d := MealDraft{
		Name:     strings.TrimSpace(g.Name),
		PlanType: string(in.PlanType),
		Mode:     string(meal.Easy),
		Notes:    strings.TrimSpace(g.Rationale),
		EveryDay: in.Days == 1 && len(g.Days) == 1,
	}
	for _, gd := range g.Days {
		day := MealDayDraft{Label: strings.TrimSpace(gd.Label)}
		for _, gm := range gd.Meals {
			m := MealDraftMeal{Name: strings.TrimSpace(gm.Name), Foods: foodDrafts(gm.Foods)}
			for _, alt := range gm.Alternatives {
				m.Alternatives = append(m.Alternatives, MealDraftOption{Label: strings.TrimSpace(alt.Label), Foods: foodDrafts(alt.Foods)})
			}
			day.Meals = append(day.Meals, m)
		}
		d.Days = append(d.Days, day)
	}
	d.Normalize()
	return d
}

func foodDrafts(foods []generatedFood) []FoodDraft {
	out := make([]FoodDraft, 0, len(foods))
	for _, f := range foods {
		fd := FoodDraft{Food: strings.TrimSpace(f.Food), Unit: "g"}
		if f.Grams > 0 {
			grams := f.Grams
			fd.Quantity, fd.Grams = &grams, &grams
		}
		out = append(out, fd)
	}
	return out
}

// mealPlanRequestText is the person's request, as the user message.
func mealPlanRequestText(req MealPlanRequest) string {
	in := req.Intake
	var b strings.Builder
	fmt.Fprintf(&b, "Objective: %s.\n", in.Objective)
	fmt.Fprintf(&b, "Build exactly %d different day(s), each with exactly %d meals.\n", in.Days, in.MealsPerDay)
	fmt.Fprintf(&b, "Each day's target: protein %.0f g, carbs %.0f g, fat %.0f g (%.0f kcal). Never go over any of them.\n",
		req.Target.ProteinG, req.Target.CarbG, req.Target.FatG, req.Target.Calories)
	if len(in.Exclusions) > 0 {
		fmt.Fprintf(&b, "Never use these, or anything made from them: %s.\n", strings.Join(in.Exclusions, ", "))
	}
	if len(req.Diets) > 0 {
		fmt.Fprintf(&b, "Diet preferences: %s.\n", strings.Join(req.Diets, ", "))
	}
	if in.Notes != "" {
		fmt.Fprintf(&b, "<notes>\n%s\n</notes>\n", in.Notes)
	}
	return b.String()
}

// catalogFor is the catalog's prompt lines, or nil when there is none to
// offer. A failed read is logged, not kept, and the plan goes ahead without
// it: every food is still checked against the catalog on preview.
func (p *AIMealPlanner) catalogFor(ctx context.Context) []string {
	if p.catalog == nil {
		return nil
	}
	p.catalogMu.Lock()
	defer p.catalogMu.Unlock()
	if p.catalogDone {
		return p.catalogLines
	}
	foods, err := p.catalog(ctx)
	if err != nil {
		middleware.FromContext(ctx).Warn("could not load the ingredient catalog; planning meals without it", slog.Any("error", err))
		return nil
	}
	lines := make([]string, len(foods))
	for i, f := range foods {
		lines[i] = fmt.Sprintf("%s: %s / %s / %s", f.Name, grams(f.Per100g.ProteinG), grams(f.Per100g.CarbG), grams(f.Per100g.FatG))
	}
	p.catalogLines, p.catalogDone = lines, true
	return lines
}

// grams prints a per-100 g amount without a trailing ".0".
func grams(v float64) string {
	return strings.TrimSuffix(fmt.Sprintf("%.1f", v), ".0")
}
