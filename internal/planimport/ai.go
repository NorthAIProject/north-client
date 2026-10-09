package planimport

import (
	"context"
	"encoding/json"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"sync"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/prompts"
	"github.com/NorthAIProject/north-client/internal/shared/aiattr"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	"github.com/NorthAIProject/north-client/internal/spend"
	"github.com/NorthAIProject/north-client/internal/users"
)

// Reader turns a document or a photo into rows. Spreadsheets and JSON never
// reach it.
//
// An interface with one production implementation, for the reason
// capture.Parser is one: the service is tested against a struct literal rather
// than a model.
//
// hint is the person's own request about the file ("only week 2", "skip the
// snacks"), or empty. It may narrow what is read, never change an amount.
type Reader interface {
	ReadWorkout(ctx context.Context, user users.User, src Source, hint string) (name string, rows []WorkoutRow, unparsed []string, err error)
	ReadMeal(ctx context.Context, user users.User, src Source, hint string) (MealReading, error)
}

// readTemperature is zero: this is transcription. Two reads of the same page
// should not disagree about how many sets it says.
var readTemperature float32 = 0

// readAttempts is how many times one provider is asked before the walk moves
// on, for the reason capture.parseAttempts gives.
const readAttempts = 2

// CatalogNames lists the shared ingredient catalog's names, which the meal
// reader offers the model as stand-ins for the foods it reads.
type CatalogNames func(ctx context.Context) ([]string, error)

// AIReader is the production Reader.
type AIReader struct {
	runner  *ai.Runner
	model   string
	catalog CatalogNames

	// The catalog is read once per process, on the first meal read that
	// manages to: it changes with a migration, not with use. A failed read
	// is not kept, so the next import tries again.
	catalogMu    sync.Mutex
	catalogNames []string
	catalogDone  bool
}

// NewAIReader builds the reader. model may be empty for the chain's default.
// catalog may be nil, and the meal reader then names foods without it.
func NewAIReader(runner *ai.Runner, model string, catalog CatalogNames) *AIReader {
	return &AIReader{runner: runner, model: model, catalog: catalog}
}

// The reply shapes. Flat and all-string, so the model copies what it sees and
// the cell parsers — the same ones a spreadsheet goes through — decide what
// the text means.
type modelReply struct {
	IsPlan        string   `json:"is_plan"`
	NotPlanReason string   `json:"not_plan_reason"`
	Name          string   `json:"name"`
	Unparsed      []string `json:"unparsed"`
}

type workoutReply struct {
	modelReply
	Rows []struct {
		Day, Exercise, Sets, Reps, Load, Rest, Notes, Confidence string
	} `json:"rows"`
}

type mealReply struct {
	modelReply
	Rows []struct {
		Day, Meal, Option, Food, Quantity, Unit, Protein, Carbs, Fat, Confidence string

		FoodEN        string `json:"food_en"`
		GramsEstimate string `json:"grams_estimate"`
	} `json:"rows"`
	Notes  string `json:"notes"`
	SameAs []struct {
		Meal   string `json:"meal"`
		SameAs string `json:"same_as"`
	} `json:"same_as"`
}

// replySchema is the shape every reading shares, with rows of the given
// fields. extra adds top-level fields beside rows; every one is required.
func replySchema(rowDescription string, fields map[string]string, order []string, extra map[string]*ai.Schema) *ai.Schema {
	props := map[string]*ai.Schema{}
	for _, f := range order {
		props[f] = ai.String(fields[f])
	}
	props["confidence"] = ai.Enum("low when you guessed which item a value belongs to or could not read it clearly", "high", "low")
	row := ai.Object(rowDescription, props, append(slices.Clip(order), "confidence")...)

	outer := map[string]*ai.Schema{
		"is_plan":         ai.Enum("whether the source is this kind of plan at all", "yes", "no"),
		"not_plan_reason": ai.String("when is_plan is no, what the source is instead; otherwise empty"),
		"name":            ai.String("the plan's title as written, or empty"),
		"rows":            ai.Array("one row per item, in source order; empty values where the source states nothing", row),
		"unparsed":        ai.Array("plan lines that became no row, in the source's own words", ai.String("the source's words")),
	}
	outerOrder := []string{"is_plan", "not_plan_reason", "name", "rows", "unparsed"}
	for _, key := range slices.Sorted(maps.Keys(extra)) {
		outer[key] = extra[key]
		outerOrder = append(outerOrder, key)
	}
	return ai.Object("the plan as written in the source", outer, outerOrder...)
}

var workoutSchema = replySchema("one exercise", map[string]string{
	"day":      "the day or session heading, as written, or empty",
	"exercise": "the exercise name, as written",
	"sets":     "the set count as written, or empty",
	"reps":     "the reps as written, or empty",
	"load":     "the load or weight as written, or empty",
	"rest":     "the rest period as written, or empty",
	"notes":    "how-to, form cues or instructions for this exercise, or empty",
}, []string{"day", "exercise", "sets", "reps", "load", "rest", "notes"}, nil)

// mealSchema lists every row field in order, so each stays required: an
// empty string is the answer for "the source says nothing".
var mealSchema = replySchema("one food", map[string]string{
	"day":            "the day heading, as written, or empty when the plan has no days",
	"meal":           "the meal heading, as written, or empty",
	"option":         "which of the meal's interchangeable options this food belongs to, as labelled (\"Opção 2\"), or empty when the meal has one",
	"food":           "the food name in the source's language, with words broken by letter-spacing rejoined",
	"food_en":        "the catalog name that fits this food, else a plain grocery-style English name",
	"quantity":       "the quantity as written, or empty",
	"unit":           "the unit as written, or empty",
	"grams_estimate": "estimated grams when quantity and unit are not already grams, as a number; otherwise empty",
	"protein":        "grams of protein if the source states it for this food, or empty",
	"carbs":          "grams of carbohydrate if the source states it for this food, or empty",
	"fat":            "grams of fat if the source states it for this food, or empty",
}, []string{"day", "meal", "option", "food", "food_en", "quantity", "unit", "grams_estimate", "protein", "carbs", "fat"}, map[string]*ai.Schema{
	"notes": ai.String("the plan's advice, recipes and general guidance, cleaned of letter-spacing, in the source's language, as markdown; empty when there is none"),
	"same_as": ai.Array("meals the source says are the same as another meal, instead of repeating their foods",
		ai.Object("one meal eaten the same as another", map[string]*ai.Schema{
			"meal":    ai.String("the meal that repeats another, as its heading is written"),
			"same_as": ai.String("the meal it is the same as, as its heading is written"),
		}, "meal", "same_as")),
})

func (r *AIReader) ReadWorkout(ctx context.Context, user users.User, src Source, hint string) (string, []WorkoutRow, []string, error) {
	system, err := prompts.Render(prompts.PlanImportWorkout, map[string]any{"Kind": string(src.Kind), "Image": src.Kind == KindImage})
	if err != nil {
		return "", nil, nil, apperr.Wrap(err, "render the workout import prompt")
	}
	var reply workoutReply
	if err := r.read(ctx, user, src, hint, system, workoutSchema, &reply); err != nil {
		return "", nil, nil, err
	}
	if err := notAPlan(reply.modelReply, "workout"); err != nil {
		return "", nil, nil, err
	}

	rows := make([]WorkoutRow, 0, len(reply.Rows))
	for _, row := range reply.Rows {
		rows = append(rows, WorkoutRow{
			Day: row.Day, Exercise: row.Exercise, Sets: row.Sets, Reps: row.Reps,
			Load: row.Load, Rest: row.Rest, Notes: row.Notes,
			Uncertain: strings.EqualFold(row.Confidence, "low"),
		})
	}
	return reply.Name, rows, reply.Unparsed, nil
}

func (r *AIReader) ReadMeal(ctx context.Context, user users.User, src Source, hint string) (MealReading, error) {
	system, err := prompts.Render(prompts.PlanImportMeal, map[string]any{
		"Kind": string(src.Kind), "Image": src.Kind == KindImage, "Catalog": r.catalogFor(ctx),
	})
	if err != nil {
		return MealReading{}, apperr.Wrap(err, "render the meal import prompt")
	}
	var reply mealReply
	if err := r.read(ctx, user, src, hint, system, mealSchema, &reply); err != nil {
		return MealReading{}, err
	}
	if err := notAPlan(reply.modelReply, "meal"); err != nil {
		return MealReading{}, err
	}

	out := MealReading{Name: reply.Name, Unparsed: reply.Unparsed, Notes: reply.Notes, Rows: make([]MealRow, 0, len(reply.Rows))}
	for _, row := range reply.Rows {
		out.Rows = append(out.Rows, MealRow{
			Day: row.Day, Meal: row.Meal, Option: row.Option, Food: row.Food, FoodEN: row.FoodEN,
			Quantity: row.Quantity, Unit: row.Unit, GramsEstimate: row.GramsEstimate,
			Protein: row.Protein, Carbs: row.Carbs, Fat: row.Fat,
			Uncertain: strings.EqualFold(row.Confidence, "low"),
		})
	}
	for _, same := range reply.SameAs {
		out.SameAs = append(out.SameAs, SameMeal{Meal: same.Meal, SameAs: same.SameAs})
	}
	return out, nil
}

// catalogFor is the catalog's names for the meal prompt, or nil when there is
// no catalog to offer. A failed read is logged and the read goes ahead
// without it: the names only steer the model's stand-ins.
func (r *AIReader) catalogFor(ctx context.Context) []string {
	if r.catalog == nil {
		return nil
	}
	r.catalogMu.Lock()
	defer r.catalogMu.Unlock()
	if r.catalogDone {
		return r.catalogNames
	}
	names, err := r.catalog(ctx)
	if err != nil {
		middleware.FromContext(ctx).Warn("could not load the ingredient catalog; reading the meal plan without it", slog.Any("error", err))
		return nil
	}
	r.catalogNames, r.catalogDone = names, true
	return names
}

func notAPlan(reply modelReply, noun string) error {
	if strings.EqualFold(reply.IsPlan, "yes") {
		return nil
	}
	msg := "This file doesn't look like a " + noun + " plan, so nothing was imported."
	if reason := strings.TrimSpace(reply.NotPlanReason); reason != "" {
		msg = "This file doesn't look like a " + noun + " plan (" + strings.TrimSuffix(reason, ".") + "), so nothing was imported."
	}
	return refuse(ReasonNotAPlan, msg)
}

// read runs one transcription through the provider chain and decodes it.
//
// The source and the person's request travel in the user message, never in
// the system prompt: both are text from outside, and the system prompt stays
// the same for every file so its prefix can be cached.
func (r *AIReader) read(ctx context.Context, user users.User, src Source, hint, system string, schema *ai.Schema, out any) error {
	var parts []ai.Part
	if src.Kind == KindImage {
		parts = []ai.Part{{InlineData: src.Image, MIMEType: src.MIME}}
	} else {
		parts = []ai.Part{ai.TextPart("<source>\n" + src.Text + "\n</source>")}
	}
	if hint = strings.TrimSpace(hint); hint != "" {
		parts = append(parts, ai.TextPart("<request>\n"+hint+"\n</request>"))
	}

	ctx = aiattr.WithUser(ctx, user.ID, spend.SurfacePlanImport)

	_, err := r.runner.Run(ctx, ai.RunOptions{Tier: string(user.Tier)}, func(client ai.Client) error {
		messages := []ai.Message{{Role: ai.RoleUser, Parts: parts}}

		for attempt := 1; attempt <= readAttempts; attempt++ {
			resp, genErr := client.Generate(ctx, ai.Request{
				Model:          r.model,
				System:         system,
				Messages:       messages,
				ResponseSchema: schema,
				Temperature:    &readTemperature,
			})
			if genErr != nil {
				return apperr.Wrap(genErr, "read the plan")
			}

			decErr := json.Unmarshal(ai.JSONReply(resp.Text), out)
			if decErr == nil {
				return nil
			}
			if attempt == readAttempts {
				return apperr.Wrap(decErr, "the reply was not valid JSON for the required shape")
			}
			messages = append(messages,
				ai.ModelText(resp.Text),
				ai.UserText("That was not valid JSON matching the schema. Return the plan again, correctly."),
			)
		}
		return nil
	})
	if err != nil {
		if apperr.Is(err, apperr.ErrValidation) || ReasonOf(err) != "" {
			return err
		}
		// Every provider refused or failed. For an image that is most often a
		// model that cannot see; say what the person can do about it.
		if src.Kind == KindImage {
			return &FileError{Reason: ReasonCannotRead, Message: "This photo couldn't be read right now. Try a JPG or PNG, or import a CSV or spreadsheet instead."}
		}
		return err
	}
	return nil
}
