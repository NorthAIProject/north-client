package capture

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/biometrics"
	"github.com/NorthAIProject/north-client/internal/checkins"
	"github.com/NorthAIProject/north-client/internal/habits"
	"github.com/NorthAIProject/north-client/internal/hydration"
	"github.com/NorthAIProject/north-client/internal/meals"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/sleep"
	"github.com/NorthAIProject/north-client/internal/users"
)

// searchLimit caps ingredient candidates offered for one food.
const searchLimit = 8

// Options are the services capture writes through. Every one is the slice that
// already owns the table; this package adds no persistence of its own.
type Options struct {
	Parser Parser

	Hydration   *hydration.Service
	Sleep       *sleep.Service
	Habits      *habits.Service
	Biometrics  *biometrics.Service
	FoodLog     *meals.FoodLogService
	Ingredients *meals.IngredientService
	CheckIns    *checkins.Service
}

// Service parses a sentence and, separately, writes what a person agreed to.
type Service struct {
	parser Parser

	hydration   *hydration.Service
	sleep       *sleep.Service
	habits      *habits.Service
	biometrics  *biometrics.Service
	foodLog     *meals.FoodLogService
	ingredients *meals.IngredientService
	checkIns    *checkins.Service
}

func NewService(opts Options) *Service {
	return &Service{
		parser:      opts.Parser,
		hydration:   opts.Hydration,
		sleep:       opts.Sleep,
		habits:      opts.Habits,
		biometrics:  opts.Biometrics,
		foodLog:     opts.FoodLog,
		ingredients: opts.Ingredients,
		checkIns:    opts.CheckIns,
	}
}

// Parse reads a sentence and resolves what it can against this account.
//
// It writes nothing. That is the whole point of the split: Parse costs a model
// call and changes no row, Commit changes rows and costs nothing, and the
// person stands between them having seen every value.
func (s *Service) Parse(ctx context.Context, user users.User, text string) (Draft, error) {
	known, err := s.habits.List(ctx, user, true)
	if err != nil {
		return Draft{}, apperr.Wrap(err, "load the habits they keep")
	}

	draft, err := s.parser.Parse(ctx, user, text, known)
	if err != nil {
		return Draft{}, err
	}

	for i := range draft.Items {
		s.resolve(ctx, user, known, &draft.Items[i])
	}
	return draft, nil
}

// resolve turns a name into a row, or explains why it could not.
//
// A name that matches nothing becomes a Problem rather than an error: the rest
// of the sentence is still worth logging, and telling somebody "no habit called
// squats" beside the words they typed is more use than refusing the whole line.
func (s *Service) resolve(ctx context.Context, user users.User, known []habits.Habit, item *Item) {
	switch item.Kind {
	case KindHabit:
		if len(known) == 0 {
			item.Problem = "You are not keeping any habits yet."
			return
		}
		match, candidates := habits.Match(known, item.Habit.Name)
		switch {
		case match.ID != uuid.Nil:
			item.Habit.ID = match.ID
			item.Habit.Name = match.Name
		case len(candidates) > 1:
			item.Problem = fmt.Sprintf("That could mean %s.", nameList(candidates))
		default:
			item.Problem = fmt.Sprintf("You are not keeping a habit called %q.", item.Habit.Name)
		}

	case KindFood:
		found, err := s.ingredients.Search(ctx, user.ID, item.Food.Query, searchLimit)
		if err != nil {
			item.Problem = "The ingredient catalog could not be searched."
			return
		}
		if len(found) == 0 {
			item.Problem = fmt.Sprintf("Nothing in the catalog matches %q.", item.Food.Query)
			return
		}
		match, ambiguous := meals.MatchIngredient(found, item.Food.Query)
		if len(ambiguous) > 0 {
			item.Problem = fmt.Sprintf("That could mean %s.", ingredientList(ambiguous))
			return
		}
		item.Food.IngredientID = match.ID
		item.Food.MatchedName = match.Name
	}
}

// Commit writes the items a person agreed to.
//
// Applied in a fixed order and never rolled back. There is no shared
// transaction across six slices and the semantics do not want one: a glass of
// water that was logged is still true even if the habit tick after it failed,
// and undoing it would be a second lie rather than a correction. Every outcome
// is reported instead, which is what makes a partial failure recoverable.
func (s *Service) Commit(ctx context.Context, user users.User, items []Item) (Receipt, error) {
	clean, err := ValidateAll(items)
	if err != nil {
		return Receipt{}, err
	}

	var receipt Receipt
	for _, item := range order(clean) {
		if !Writable(item) {
			receipt.Skipped = append(receipt.Skipped, item)
			continue
		}

		outcome := Outcome{Item: item}
		if err := s.write(ctx, user, item); err != nil {
			// The person's own words never reach this string, only the kind:
			// a capture box is where somebody types "mood 2, argued with my
			// partner", and that is journal-grade content.
			outcome.Error = userFacing(err)
		} else {
			outcome.Summary = Summary(item)
		}
		receipt.Outcomes = append(receipt.Outcomes, outcome)
	}

	if len(receipt.Outcomes) == 0 {
		return Receipt{}, apperr.Wrap(apperr.ErrValidation, "there is nothing to log")
	}
	return receipt, nil
}

// write sends one item to the slice that owns it.
func (s *Service) write(ctx context.Context, user users.User, item Item) error {
	switch item.Kind {
	case KindWater:
		_, err := s.hydration.Log(ctx, user, item.Water.AmountML)
		return err

	case KindSleep:
		in := sleep.Input{DurationMinutes: item.Sleep.Minutes}
		if item.Sleep.Quality > 0 {
			quality := item.Sleep.Quality
			in.Quality = &quality
		}
		// LogToday already files last night against today's date, so an
		// unstated date is the common path rather than a missing one.
		if item.Sleep.Date.IsZero() {
			_, err := s.sleep.LogToday(ctx, user, in)
			return err
		}
		_, err := s.sleep.LogFor(ctx, user, item.Sleep.Date, in)
		return err

	case KindHabit:
		return s.habits.Complete(ctx, user, item.Habit.ID)

	case KindWeight:
		_, err := s.biometrics.RecordWeight(ctx, user.ID, item.Weight.KG)
		return err

	case KindCheckIn:
		_, err := s.checkIns.UpsertToday(ctx, user, checkins.Input{
			Mood:   item.CheckIn.Mood,
			Energy: item.CheckIn.Energy,
			Notes:  item.CheckIn.Notes,
		})
		return err

	case KindFood:
		_, err := s.foodLog.LogIngredient(ctx, user.ID, meals.LogIngredientInput{
			IngredientID:  item.Food.IngredientID,
			QuantityGrams: item.Food.Grams,
		})
		return err

	default:
		return apperr.Wrap(apperr.ErrValidation, "nothing writes a %s", item.Kind)
	}
}

// order fixes the sequence a commit applies in, so the receipt reads in the
// same order as the preview and a test can assert it.
func order(items []Item) []Item {
	out := make([]Item, 0, len(items))
	for _, kind := range Kinds {
		for _, item := range items {
			if item.Kind == kind {
				out = append(out, item)
			}
		}
	}
	return out
}

// userFacing reduces a write failure to something safe to show.
//
// A validation failure carries a sentence somebody can act on and is passed
// through. Anything else is a fixed line: an unexpected error string is where a
// table name reaches a screen.
func userFacing(err error) string {
	if apperr.Is(err, apperr.ErrValidation) || apperr.Is(err, apperr.ErrNotFound) {
		return Sentence(err)
	}
	return "That did not save. Try again."
}

// sentinelSuffixes are the sentinel texts apperr.Wrap leaves on the end of an
// error message.
//
// Wrap composes "%s: %w", which is right for a log and wrong for a screen: the
// reason it exists is that errors.Is keeps matching after several layers, and
// nobody reading "record your height first: validation failed" needed the
// second half. This is the list of second halves.
var sentinelSuffixes = []string{
	apperr.ErrValidation.Error(),
	apperr.ErrNotFound.Error(),
	apperr.ErrConflict.Error(),
	apperr.ErrUnauthenticated.Error(),
	apperr.ErrForbidden.Error(),
	apperr.ErrUnavailable.Error(),
	apperr.ErrPaymentRequired.Error(),
}

// Sentence renders an error as one line a person can read: the message without
// the sentinel apperr appended, capitalised, ending in a full stop.
func Sentence(err error) string {
	if err == nil {
		return ""
	}

	text := err.Error()
	// Repeatedly, because an error wrapped twice carries the sentinel once but
	// may still end in a wrapped clause of its own.
	for {
		trimmed := text
		for _, suffix := range sentinelSuffixes {
			trimmed = strings.TrimSuffix(trimmed, suffix)
			trimmed = strings.TrimRight(trimmed, " :")
		}
		if trimmed == text {
			break
		}
		text = trimmed
	}

	text = strings.TrimSpace(text)
	if text == "" {
		// Every layer was a sentinel. Better a vague sentence than an empty
		// one, which renders as a blank row nobody can act on.
		return "That did not work."
	}

	text = capitalise(text)
	if !strings.HasSuffix(text, ".") && !strings.HasSuffix(text, "!") && !strings.HasSuffix(text, "?") {
		text += "."
	}
	return text
}

func capitalise(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func nameList(list []habits.Habit) string {
	out := make([]string, 0, len(list))
	for _, h := range list {
		out = append(out, h.Name)
	}
	return strings.Join(out, ", ")
}

func ingredientList(list []meals.Ingredient) string {
	out := make([]string, 0, len(list))
	for _, ing := range list {
		out = append(out, ing.Name)
	}
	return strings.Join(out, ", ")
}
