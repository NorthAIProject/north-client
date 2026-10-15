package lifts

import (
	"context"

	"github.com/NorthAIProject/north-client/internal/shared/i18n/fieldmsg"
)

// fieldMessages names the catalogue key for each field error the service
// writes. The service keeps writing English because the coach logs sets
// through it too; the handlers say these in the reader's language.
var fieldMessages = []fieldmsg.Message{
	{English: "Name the exercise.", Key: "lifts.err.name"},
	{English: "Enter a set number between 1 and 50.", Key: "lifts.err.setno"},
	{English: "Enter a weight between 0 and 600 kg.", Key: "lifts.err.weight.range"},
	{English: "Enter between 1 and 100 reps.", Key: "lifts.err.reps.range"},
	{English: "Choose work, warm-up or drop set.", Key: "lifts.err.kind"},
	{English: "Enter reps in reserve between 0 and 10.", Key: "lifts.err.rir.range"},
	{English: fieldmsg.English("lifts.err.days", BodyMapMaxDays), Key: "lifts.err.days", Args: []any{BodyMapMaxDays}},
	{English: "That file is not a Hevy workout export. In Hevy: Profile → Settings → Export & Import Data → Export Workouts.", Key: "lifts.err.notexport"},
}

// localize says err's field messages in ctx's language.
func localize(ctx context.Context, err error) error {
	return fieldmsg.Localize(ctx, err, fieldMessages)
}
