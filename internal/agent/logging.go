package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/biometrics"
	"github.com/NorthAIProject/north-client/internal/habits"
	"github.com/NorthAIProject/north-client/internal/hydration"
	"github.com/NorthAIProject/north-client/internal/meals"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/sleep"
	"github.com/NorthAIProject/north-client/internal/users"
)

// The day's logs, by conversation.
//
// These close the oldest gap in the tool surface: the coach could set a goal
// and edit a training plan, but the six things a person actually records every
// day — water, sleep, a habit kept, a weight, what they ate — could only be
// entered through a form. The one place someone is already typing was the one
// place that could not log.
//
// None is ReadOnly, so each shows an approval card carrying the call and its
// arguments before it runs. That is right here: in a conversation the model
// chose the write, and the card is what closes the gap between its choice and
// the person's intent.
//
// Every one of them resolves by name rather than by id, for the reason
// addGoalUpdate documents: a model that can name a UUID is one prompt away
// from writing to a row that is not its user's.

func logWater(svc *hydration.Service, userSvc *users.Service) Capability {
	type args struct {
		AmountML int `json:"amount_ml"`
	}

	return Capability{
		Tool: ai.Tool{
			Name: "log_water",
			Description: "Record that the user drank water. Amounts are in millilitres: a glass is about 250, " +
				"a large bottle about 750. Call this once per drink.",
			Parameters: ai.Object("the drink", map[string]*ai.Schema{
				"amount_ml": ai.Integer("how much they drank, in millilitres, between 1 and 5000"),
			}, "amount_ml"),
		},
		// Every drink is its own entry, so calling this twice records two
		// drinks. That is the truth about the domain rather than a limitation:
		// a retry has to be the person's decision.
		Idempotent: false,
		Invoke: func(ctx context.Context, userID uuid.UUID, raw json.RawMessage) (string, error) {
			in, err := Decode[args](raw)
			if err != nil {
				return "", err
			}

			// The whole record: hydration files an entry against the person's
			// local day, which needs their timezone.
			user, err := userSvc.ByID(ctx, userID)
			if err != nil {
				return "", err
			}

			if _, logErr := svc.Log(ctx, user, in.AmountML); logErr != nil {
				return "", logErr
			}

			day, err := svc.Today(ctx, user)
			if err != nil {
				// The drink is recorded. A total we could not read back is not
				// worth reporting as a failure.
				return fmt.Sprintf("Logged %d ml of water.", in.AmountML), nil
			}
			return fmt.Sprintf("Logged %d ml of water. That is %d ml today.", in.AmountML, day.TotalML), nil
		},
	}
}

func logSleep(svc *sleep.Service, userSvc *users.Service) Capability {
	type args struct {
		Minutes int    `json:"minutes"`
		Quality int    `json:"quality"`
		Notes   string `json:"notes"`
	}

	return Capability{
		Tool: ai.Tool{
			Name: "log_sleep",
			Description: "Record last night's sleep, in minutes. Writing again for the same night replaces " +
				"that night's entry rather than adding a second one, so this is safe to call twice.",
			Parameters: ai.Object("last night", map[string]*ai.Schema{
				"minutes": ai.Integer("how long they slept, in minutes; 6 hours is 360"),
				"quality": ai.Integer("how well they slept, 1 (worst) to 5 (best); 0 if they did not say"),
				"notes":   ai.String("anything they said about the night; empty if nothing"),
			}, "minutes", "quality", "notes"),
		},
		// An upsert on the night's local date: a second call corrects the
		// first rather than adding to it.
		Idempotent: true,
		Invoke: func(ctx context.Context, userID uuid.UUID, raw json.RawMessage) (string, error) {
			in, err := Decode[args](raw)
			if err != nil {
				return "", err
			}

			user, err := userSvc.ByID(ctx, userID)
			if err != nil {
				return "", err
			}

			input := sleep.Input{DurationMinutes: in.Minutes, Notes: in.Notes}
			if in.Quality > 0 {
				quality := in.Quality
				input.Quality = &quality
			}

			if _, err := svc.LogToday(ctx, user, input); err != nil {
				return "", err
			}

			hours := float64(in.Minutes) / 60
			return fmt.Sprintf("Logged %.1f hours of sleep for last night.", hours), nil
		},
	}
}

func completeHabit(svc *habits.Service, userSvc *users.Service) Capability {
	type args struct {
		Name string `json:"name"`
	}

	return Capability{
		Tool: ai.Tool{
			Name: "complete_habit",
			Description: "Mark one of the user's habits as done today, naming it as they do. " +
				"This never creates a habit: if they name something they do not keep, tell them so " +
				"and point them at the habits section rather than inventing one.",
			Parameters: ai.Object("the habit they kept", map[string]*ai.Schema{
				"name": ai.String("the habit's name, such as 'read 20 pages'"),
			}, "name"),
		},
		// One completion per habit per local day, so a second call leaves the
		// same state behind.
		Idempotent: true,
		Invoke: func(ctx context.Context, userID uuid.UUID, raw json.RawMessage) (string, error) {
			in, err := Decode[args](raw)
			if err != nil {
				return "", err
			}

			user, err := userSvc.ByID(ctx, userID)
			if err != nil {
				return "", err
			}

			list, err := svc.List(ctx, user, true)
			if err != nil {
				return "", err
			}

			match, err := matchHabit(list, in.Name)
			if err != nil {
				return "", err
			}

			if err := svc.Complete(ctx, user, match.ID); err != nil {
				return "", err
			}
			return fmt.Sprintf("Marked %q as done today.", match.Name), nil
		},
	}
}

// matchHabit finds the one habit a name refers to, and says why it could not.
//
// The rule itself is habits.Match — the habits slice owns what its names mean,
// and the coach and quick capture have to agree about it. What belongs here is
// only the manner of the refusal: a model recovers in one turn when the error
// names the candidates, and not at all when it says "no".
func matchHabit(list []habits.Habit, name string) (habits.Habit, error) {
	if strings.TrimSpace(name) == "" {
		return habits.Habit{}, apperr.Wrap(apperr.ErrValidation, "name the habit to mark as done")
	}
	if len(list) == 0 {
		return habits.Habit{}, apperr.Wrap(apperr.ErrNotFound,
			"you are not keeping any habits yet, so there is nothing to tick off")
	}

	match, candidates := habits.Match(list, name)
	switch {
	case match.ID != uuid.Nil:
		return match, nil
	case len(candidates) > 1:
		return habits.Habit{}, apperr.Wrap(apperr.ErrValidation,
			"%q could mean any of %s; say which", name, names(candidates))
	default:
		return habits.Habit{}, apperr.Wrap(apperr.ErrNotFound,
			"no habit called %q; the ones you keep are %s", name, names(list))
	}
}

func names(list []habits.Habit) string {
	out := make([]string, 0, len(list))
	for _, h := range list {
		out = append(out, strconv.Quote(h.Name))
	}
	return strings.Join(out, ", ")
}

func recordWeight(svc *biometrics.Service) Capability {
	type args struct {
		Kg float64 `json:"kg"`
	}

	return Capability{
		Tool: ai.Tool{
			Name: "record_weight",
			Description: "Record the user's bodyweight in kilograms. Convert pounds yourself before calling. " +
				"This updates their current measurement and keeps the previous one as history.",
			Parameters: ai.Object("the reading", map[string]*ai.Schema{
				"kg": ai.Number("bodyweight in kilograms, between 20 and 400"),
			}, "kg"),
		},
		// Recording the same weight twice leaves the same current measurement,
		// so a retry is safe even though the history gains a row.
		Idempotent: true,
		Invoke: func(ctx context.Context, userID uuid.UUID, raw json.RawMessage) (string, error) {
			in, err := Decode[args](raw)
			if err != nil {
				return "", err
			}

			if _, err := svc.RecordWeight(ctx, userID, in.Kg); err != nil {
				return "", err
			}
			return fmt.Sprintf("Recorded %.1f kg.", in.Kg), nil
		},
	}
}

func logFood(foodLog *meals.FoodLogService, ingredients *meals.IngredientService) Capability {
	type args struct {
		Food  string  `json:"food"`
		Grams float64 `json:"grams"`
	}

	return Capability{
		Tool: ai.Tool{
			Name: "log_food",
			Description: "Record something the user ate, by name and weight in grams. " +
				"The name is looked up in the ingredient catalog, so use a plain one such as " +
				"'chicken breast' rather than a brand or a recipe.",
			Parameters: ai.Object("what they ate", map[string]*ai.Schema{
				"food":  ai.String("the ingredient's name"),
				"grams": ai.Number("how much they ate, in grams"),
			}, "food", "grams"),
		},
		Idempotent: false,
		Invoke: func(ctx context.Context, userID uuid.UUID, raw json.RawMessage) (string, error) {
			in, err := Decode[args](raw)
			if err != nil {
				return "", err
			}

			match, err := matchIngredient(ctx, ingredients, userID, in.Food)
			if err != nil {
				return "", err
			}

			if _, err := foodLog.LogIngredient(ctx, userID, meals.LogIngredientInput{
				IngredientID:  match.ID,
				QuantityGrams: in.Grams,
			}); err != nil {
				return "", err
			}
			return fmt.Sprintf("Logged %.0f g of %s.", in.Grams, match.Name), nil
		},
	}
}

// matchIngredient resolves a spoken food name to a catalog row.
//
// The rule is meals.MatchIngredient; what is here is the search that feeds it
// and the wording of the refusal.
func matchIngredient(ctx context.Context, svc *meals.IngredientService, userID uuid.UUID, name string) (meals.Ingredient, error) {
	query := strings.TrimSpace(name)
	if query == "" {
		return meals.Ingredient{}, apperr.Wrap(apperr.ErrValidation, "name the food to log")
	}

	found, err := svc.Search(ctx, userID, query, resultLimit)
	if err != nil {
		return meals.Ingredient{}, err
	}
	if len(found) == 0 {
		return meals.Ingredient{}, apperr.Wrap(apperr.ErrNotFound,
			"nothing in the ingredient catalog matches %q", name)
	}

	match, ambiguous := meals.MatchIngredient(found, query)
	if len(ambiguous) > 0 {
		options := make([]string, 0, len(ambiguous))
		for _, ing := range ambiguous {
			options = append(options, strconv.Quote(ing.Name))
		}
		return meals.Ingredient{}, apperr.Wrap(apperr.ErrValidation,
			"%q could mean any of %s; say which", name, strings.Join(options, ", "))
	}
	return match, nil
}
