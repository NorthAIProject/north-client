package meals

import (
	"context"
	"fmt"

	"github.com/NorthAIProject/north-client/internal/meals/meal"
	"github.com/NorthAIProject/north-client/internal/shared/i18n/fieldmsg"
)

// fieldMessages names the catalogue key for each field error the services
// write. They keep writing English because the coach edits plans and logs
// food through them too; the handlers say these in the reader's language.
var fieldMessages = []fieldmsg.Message{
	{English: "Give the ingredient a name.", Key: "nutrition.err.ingredient.name"},
	{English: "Keep the name under 200 characters.", Key: "nutrition.err.name.long"},
	{English: "Choose one of the listed categories.", Key: "nutrition.err.category"},
	{English: "Calories cannot be negative.", Key: "nutrition.err.calories"},
	{English: "Work out your macro target in the calculator first; meal plans are built on it.", Key: "nutrition.err.target"},
	{English: fmt.Sprintf("Keep the notes to %d characters.", MaxPlanNotesRunes), Key: "nutrition.err.notes.long", Args: []any{MaxPlanNotesRunes}},
	{English: "Easy plans take a number of days; choose weekdays in advanced mode.", Key: "nutrition.err.weekdays.easy"},
	{English: "Choose each weekday at most once.", Key: "nutrition.err.weekdays.once"},
	{English: "Choose at least one weekday.", Key: "nutrition.err.weekdays.none"},
	{English: fmt.Sprintf("Choose between 1 and %d days.", meal.MaxDays), Key: "nutrition.err.daycount", Args: []any{meal.MaxDays}},
	{English: "Give the plan a name.", Key: "nutrition.err.plan.name"},
	{English: "Choose easy or advanced.", Key: "nutrition.err.mode"},
	{English: "Choose a plan type.", Key: "nutrition.err.plantype"},
	{English: "A custom carb share is an advanced-mode choice.", Key: "nutrition.err.custom.easy"},
	{English: "Enter a share of your carb target between 0 and 100%.", Key: "nutrition.err.custom.pct"},
	{English: "Choose no, low, mid or high carb.", Key: "nutrition.err.carbtype"},
	{English: "Set the day's carbs by type or by grams, not both.", Key: "nutrition.err.carb.both"},
	gramsMessage("carbs"),
	gramsMessage("protein"),
	gramsMessage("fat"),
	{English: "Choose an ingredient.", Key: "nutrition.err.ingredient.choose"},
	{English: "Enter a quantity greater than zero.", Key: "nutrition.err.quantity"},
	{English: "There are more days of meals than days in the plan.", Key: "nutrition.err.days.more"},
	{English: "Switching to easy resets every day to the plan's default target.", Key: "nutrition.err.toeasy"},
	{English: fmt.Sprintf("A plan has at most %d days.", meal.MaxDays), Key: "nutrition.err.maxdays", Args: []any{meal.MaxDays}},
	{English: "Easy plans add the next day of the week; choose weekdays in advanced mode.", Key: "nutrition.err.adddays.easy"},
	{English: "Choose a weekday the plan does not have yet.", Key: "nutrition.err.weekday.taken"},
	{English: "Switch the plan to advanced to change a single day.", Key: "nutrition.err.day.advanced"},
	{English: "A plan needs at least one day.", Key: "nutrition.err.day.last"},
	{English: "Give the meal a name.", Key: "nutrition.err.meal.name"},
	{English: "Give the option a label.", Key: "nutrition.err.option.label"},
	{English: fmt.Sprintf("Keep the label to %d characters.", MaxOptionLabelRunes), Key: "nutrition.err.option.long", Args: []any{MaxOptionLabelRunes}},
	{English: "Choose at least one ingredient.", Key: "nutrition.err.ingredients.none"},
	{English: "Give the reminder a name, like \"log lunch\".", Key: "nutrition.err.reminder.label"},
	{English: "Enter a time as HH:MM.", Key: "nutrition.err.reminder.time"},
	{English: "Days must be between 0 (Sunday) and 6 (Saturday).", Key: "nutrition.err.reminder.days"},
	{English: "Choose cutting, maintenance or bulking.", Key: "nutrition.err.objective"},
	{English: "Choose between 1 and 8 meals a day.", Key: "nutrition.err.mealsperday"},
	{English: "List at most 30 foods to leave out.", Key: "nutrition.err.exclusions.many"},
	{English: "Keep each food to leave out under 60 characters.", Key: "nutrition.err.exclusions.long"},
	{English: "Keep this under 500 characters.", Key: "nutrition.err.intake.notes"},
}

// gramsMessage matches checkGrams' refusal for one macro. The limit is the
// person's own target, so the message is recognised by its shape, the limit
// read back out of it, and the whole compared with the catalogue's English.
func gramsMessage(macro string) fieldmsg.Message {
	key := "nutrition.err.grams." + macro
	return fieldmsg.Message{Key: key, Match: func(msg string) ([]any, bool) {
		var limit float64
		if _, err := fmt.Sscanf(msg, "Keep "+macro+" between 0 and your target of %f g.", &limit); err != nil {
			return nil, false
		}
		if msg != fieldmsg.English(key, limit) {
			return nil, false
		}
		return []any{limit}, true
	}}
}

// localize says err's field messages in ctx's language.
func localize(ctx context.Context, err error) error {
	return fieldmsg.Localize(ctx, err, fieldMessages)
}

// Localize says err's field messages from this package in ctx's language,
// for a caller outside it that shows a meals refusal to a person — plan
// import saving a plan through CreatePlan.
func Localize(ctx context.Context, err error) error { return localize(ctx, err) }
