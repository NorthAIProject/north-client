package planimport

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/prompts"
	"github.com/NorthAIProject/north-client/internal/shared/aiattr"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/spend"
	"github.com/NorthAIProject/north-client/internal/users"
)

// Reader turns a document or a photo into rows. Spreadsheets and JSON never
// reach it.
//
// An interface with one production implementation, for the reason
// capture.Parser is one: the service is tested against a struct literal rather
// than a model.
type Reader interface {
	ReadWorkout(ctx context.Context, user users.User, src Source) (name string, rows []WorkoutRow, unparsed []string, err error)
	ReadMeal(ctx context.Context, user users.User, src Source) (name string, rows []MealRow, unparsed []string, err error)
}

// readTemperature is zero: this is transcription. Two reads of the same page
// should not disagree about how many sets it says.
var readTemperature float32 = 0

// readAttempts is how many times one provider is asked before the walk moves
// on, for the reason capture.parseAttempts gives.
const readAttempts = 2

// AIReader is the production Reader.
type AIReader struct {
	runner *ai.Runner
	model  string
}

// NewAIReader builds the reader. model may be empty for the chain's default.
func NewAIReader(runner *ai.Runner, model string) *AIReader {
	return &AIReader{runner: runner, model: model}
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
		Day, Meal, Food, Quantity, Unit, Protein, Carbs, Fat, Confidence string
	} `json:"rows"`
}

func replySchema(rowDescription string, fields map[string]string, order []string) *ai.Schema {
	props := map[string]*ai.Schema{}
	for _, f := range order {
		props[f] = ai.String(fields[f])
	}
	props["confidence"] = ai.Enum("low when you guessed which item a value belongs to or could not read it clearly", "high", "low")
	row := ai.Object(rowDescription, props, append(order, "confidence")...)

	return ai.Object("the plan as written in the source", map[string]*ai.Schema{
		"is_plan":         ai.Enum("whether the source is this kind of plan at all", "yes", "no"),
		"not_plan_reason": ai.String("when is_plan is no, what the source is instead; otherwise empty"),
		"name":            ai.String("the plan's title as written, or empty"),
		"rows":            ai.Array("one row per item, in source order; empty values where the source states nothing", row),
		"unparsed":        ai.Array("plan lines that became no row, in the source's own words", ai.String("the source's words")),
	}, "is_plan", "not_plan_reason", "name", "rows", "unparsed")
}

var workoutSchema = replySchema("one exercise", map[string]string{
	"day":      "the day or session heading, as written, or empty",
	"exercise": "the exercise name, as written",
	"sets":     "the set count as written, or empty",
	"reps":     "the reps as written, or empty",
	"load":     "the load or weight as written, or empty",
	"rest":     "the rest period as written, or empty",
	"notes":    "how-to, form cues or instructions for this exercise, or empty",
}, []string{"day", "exercise", "sets", "reps", "load", "rest", "notes"})

var mealSchema = replySchema("one food", map[string]string{
	"day":      "the day heading, as written, or empty",
	"meal":     "the meal heading, as written, or empty",
	"food":     "the food name, as written",
	"quantity": "the quantity as written, or empty",
	"unit":     "the unit as written, or empty",
	"protein":  "grams of protein if the source states it for this food, or empty",
	"carbs":    "grams of carbohydrate if the source states it for this food, or empty",
	"fat":      "grams of fat if the source states it for this food, or empty",
}, []string{"day", "meal", "food", "quantity", "unit", "protein", "carbs", "fat"})

func (r *AIReader) ReadWorkout(ctx context.Context, user users.User, src Source) (string, []WorkoutRow, []string, error) {
	var reply workoutReply
	if err := r.read(ctx, user, src, prompts.PlanImportWorkout, workoutSchema, &reply); err != nil {
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

func (r *AIReader) ReadMeal(ctx context.Context, user users.User, src Source) (string, []MealRow, []string, error) {
	var reply mealReply
	if err := r.read(ctx, user, src, prompts.PlanImportMeal, mealSchema, &reply); err != nil {
		return "", nil, nil, err
	}
	if err := notAPlan(reply.modelReply, "meal"); err != nil {
		return "", nil, nil, err
	}

	rows := make([]MealRow, 0, len(reply.Rows))
	for _, row := range reply.Rows {
		rows = append(rows, MealRow{
			Day: row.Day, Meal: row.Meal, Food: row.Food, Quantity: row.Quantity, Unit: row.Unit,
			Protein: row.Protein, Carbs: row.Carbs, Fat: row.Fat,
			Uncertain: strings.EqualFold(row.Confidence, "low"),
		})
	}
	return reply.Name, rows, reply.Unparsed, nil
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
func (r *AIReader) read(ctx context.Context, user users.User, src Source, prompt string, schema *ai.Schema, out any) error {
	system, err := prompts.Render(prompt, map[string]any{"Kind": string(src.Kind), "Image": src.Kind == KindImage})
	if err != nil {
		return apperr.Wrap(err, "render the plan import prompt")
	}

	var parts []ai.Part
	if src.Kind == KindImage {
		parts = []ai.Part{{InlineData: src.Image, MIMEType: src.MIME}}
	} else {
		parts = []ai.Part{ai.TextPart("<source>\n" + src.Text + "\n</source>")}
	}

	ctx = aiattr.WithUser(ctx, user.ID, spend.SurfacePlanImport)

	_, err = r.runner.Run(ctx, ai.RunOptions{Tier: string(user.Tier)}, func(client ai.Client) error {
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
