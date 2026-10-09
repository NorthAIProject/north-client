package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/documents"
	"github.com/NorthAIProject/north-client/internal/meals"
	"github.com/NorthAIProject/north-client/internal/meals/meal"
	"github.com/NorthAIProject/north-client/internal/media"
	"github.com/NorthAIProject/north-client/internal/planimport"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	"github.com/NorthAIProject/north-client/internal/users"
	"github.com/NorthAIProject/north-client/internal/workouts"
)

// Importing a plan from a file someone sent in chat.
//
// The import pages read a file into a draft the person corrects before
// anything is saved. Here the approval card stands in for that review: it
// shows which file, which kind of plan and what to skip, and approving it
// reads and saves the plan in one step (planimport.ImportForCoach). What the
// import could not save comes back in the result, so the coach can say so and
// refine the plan with edit_meal_plan.
//
// The file is named, never identified: the model sees the name the person sent
// it under ("<attachment name=…>", "[file: dieta.pdf]"), and the card shows it.

// maxImportInstructions caps the person's own words passed to the reader.
const maxImportInstructions = 500

// maxReportedLines caps the skipped and unread lines a result lists.
const maxReportedLines = 15

// maxNoteTitlePlanName keeps a notes title inside the documents' 200-byte
// limit whatever the plan name's script: 40 runes is at most 160 bytes.
const maxNoteTitlePlanName = 40

func importPlanFromAttachment(imports *planimport.Service, files *media.Service, userSvc *users.Service, docs *documents.Service) Capability {
	type args struct {
		Kind         string `json:"kind"`
		File         string `json:"file"`
		PlanType     string `json:"plan_type"`
		Instructions string `json:"instructions"`
	}

	planTypes := make([]string, len(meal.CarbBands))
	for i, b := range meal.CarbBands {
		planTypes[i] = string(b.Type)
	}

	return Capability{
		Tool: ai.Tool{
			Name: "import_plan_from_attachment",
			Description: "Save a meal plan or a training plan from a file or photo this person sent in this chat, as the file " +
				"says it, rather than retelling it with create_meal_plan or create_workout_plan. A meal plan is held to their " +
				"macro target, so they need one first; days the file leaves unnamed are filled from Monday, and a plan with no " +
				"weekdays is the same every day. A meal's alternatives in the file become its options; option 1 is the one the " +
				"day's totals count. Food lines that match nothing are left out and listed in the result: tell them which. " +
				"Refine the plan afterwards with edit_meal_plan.",
			Parameters: ai.Object("the file to import", map[string]*ai.Schema{
				"kind": ai.Enum("what the file is a plan of", planimport.CoachImportMeal, planimport.CoachImportWorkout),
				"file": ai.String("the file's name exactly as it appeared in the conversation (e.g. in <attachment name=…> or " +
					"[file: dieta.pdf] / [photo: plano.jpg])"),
				"plan_type": ai.Enum("a meal plan's carb level; mid_carb when they have not said; ignored for a workout",
					planTypes...),
				"instructions": ai.String("what to import or skip, in their words; optional"),
			}, "kind", "file"),
		},
		Invoke: func(ctx context.Context, userID uuid.UUID, raw json.RawMessage) (string, error) {
			in, err := Decode[args](raw)
			if err != nil {
				return "", err
			}
			name := strings.TrimSpace(in.File)
			if name == "" {
				return "", apperr.Wrap(apperr.ErrValidation,
					"name the file to import, exactly as it appeared in the conversation")
			}

			user, err := userSvc.ByID(ctx, userID)
			if err != nil {
				return "", err
			}
			file, data, err := files.LatestChatFile(ctx, userID, name)
			if errors.Is(err, media.ErrNoChatFile) {
				return "", apperr.Wrap(apperr.ErrNotFound,
					"I can't find a file named %q in this chat; ask them to attach it again", name)
			}
			if err != nil {
				return "", err
			}

			req := planimport.CoachImport{
				Kind: strings.TrimSpace(in.Kind),
				Hint: truncateRunes(strings.TrimSpace(in.Instructions), maxImportInstructions),
			}
			if req.Kind == planimport.CoachImportMeal {
				req.PlanType = strings.TrimSpace(in.PlanType)
			}
			res, err := imports.ImportForCoach(ctx, user, file.OriginalName, data, req)
			if err != nil {
				return "", explainImportError(err, file.OriginalName)
			}

			var b strings.Builder
			switch {
			case res.MealPlan != nil:
				describeImportedMealPlan(&b, *res.MealPlan, res)
				if docs != nil && strings.TrimSpace(res.Notes) != "" {
					b.WriteString("\n")
					b.WriteString(saveImportNotes(ctx, docs, userID, res.MealPlan.Name, res.Notes, file.OriginalName))
				}
				b.WriteString("\nRefine it with edit_meal_plan; give option to change an option other than the first.")
			case res.Workout != nil:
				describeImportedWorkout(&b, *res.Workout)
			}
			writeCapped(&b, "Lines of the file not read as part of the plan:", res.Unparsed)
			return b.String(), nil
		},
	}
}

// explainImportError turns an import refusal into what the model can tell the
// person. Anything that is not theirs to act on passes through unchanged.
func explainImportError(err error, filename string) error {
	if errors.Is(err, planimport.ErrQuotaUsed) {
		return apperr.Wrap(apperr.ErrValidation, "nothing was imported: %s", err.Error())
	}
	switch reason := planimport.ReasonOf(err); reason {
	case "":
	case planimport.ReasonBusy:
		return apperr.Wrap(apperr.ErrConflict,
			"another plan import of theirs is still running; nothing was imported, so try again once it has finished")
	default:
		return apperr.Wrap(apperr.ErrValidation, "%s could not be imported: %s", filename, err.Error())
	}

	var fields apperr.FieldErrors
	if !apperr.As(err, &fields) {
		return err
	}
	messages := fields.Messages()
	if _, ok := messages["macro_target"]; ok {
		return apperr.Wrap(apperr.ErrValidation,
			"nothing was imported: a meal plan is built on their macro target, and they have none yet; "+
				"have them set a nutrition target first (calculate_macros), then import again")
	}
	for _, field := range []string{"file", "kind", "plan_type"} {
		if msg, ok := messages[field]; ok {
			return apperr.Wrap(apperr.ErrValidation, "nothing was imported: %s", msg)
		}
	}
	return err
}

// describeImportedMealPlan says what a meal import saved: on which days, each
// meal's options on one day, and what it left out or let run over.
func describeImportedMealPlan(b *strings.Builder, plan meals.MealPlan, res planimport.CoachResult) {
	fmt.Fprintf(b, "Imported the meal plan %q (%s", plan.Name, plan.Settings.Type.Label())
	same := daysAlike(plan)
	switch {
	case len(plan.Days) == len(meal.WeekOrder) && same:
		b.WriteString(", every day).")
	default:
		names := make([]string, len(plan.Days))
		for i, d := range plan.Days {
			names[i] = d.Weekday.String()
		}
		fmt.Fprintf(b, ") on %s.", strings.Join(names, ", "))
	}

	if len(plan.Days) > 0 {
		day := plan.Days[0]
		if same {
			b.WriteString("\nEach day's meals:")
		} else {
			fmt.Fprintf(b, "\n%s's meals (other days differ; get_meal_plan shows each):", day.Weekday)
		}
		for _, m := range day.Meals {
			if n := len(m.Options()); n > 1 {
				fmt.Fprintf(b, "\n- %s: %d options; option 1 counted, %.0f kcal", m.Name, n, m.TotalMacros.Calories)
			} else {
				fmt.Fprintf(b, "\n- %s: 1 option, %.0f kcal", m.Name, m.TotalMacros.Calories)
			}
		}
	}

	writeCapped(b, "Over their target, saved anyway as they approved:", res.Over)
	writeCapped(b, "Food lines left out, each with why (tell them which):", res.Skipped)
}

// daysAlike reports whether every day of plan reads the same.
func daysAlike(plan meals.MealPlan) bool {
	return len(groupDays(plan, plan.Days, nil)) <= 1
}

func describeImportedWorkout(b *strings.Builder, stored workouts.StoredPlan) {
	fmt.Fprintf(b, "Imported the workout plan %q:", stored.Plan.Name)
	for _, day := range stored.Plan.Days {
		fmt.Fprintf(b, "\n- %s — %s (%d exercises)", day.Weekday, day.Focus, len(day.Exercises))
	}
	b.WriteString("\nRefine it with the workout editing tools; get_workout_plan shows every exercise.")
}

// saveImportNotes keeps the file's advice and recipes as a note, and says how
// that went. A note that could not be saved does not undo the plan.
func saveImportNotes(ctx context.Context, docs *documents.Service, userID uuid.UUID, planName, notes, filename string) string {
	title := "Plan notes — " + truncateRunes(planName, maxNoteTitlePlanName)
	body := strings.TrimSpace(notes) + "\n\nFrom " + filename
	if _, err := docs.CreateNote(ctx, userID, title, body); err != nil {
		middleware.FromContext(ctx).Warn("save imported plan notes", slog.Any("error", err))
		return "The plan is saved, but its advice and recipes could not be saved to their notes: " + userFacing(err)
	}
	return "Saved the plan's advice and recipes to their notes."
}

// writeCapped lists lines under heading, at most maxReportedLines of them.
func writeCapped(b *strings.Builder, heading string, lines []string) {
	if len(lines) == 0 {
		return
	}
	b.WriteString("\n" + heading)
	for i, line := range lines {
		if i == maxReportedLines {
			fmt.Fprintf(b, "\n- and %d more", len(lines)-maxReportedLines)
			break
		}
		b.WriteString("\n- " + line)
	}
}

func truncateRunes(s string, limit int) string {
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	return string(r[:limit])
}
