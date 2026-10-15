package weekly

import (
	"context"

	"github.com/NorthAIProject/north-client/internal/shared/i18n/fieldmsg"
)

// fieldMessages names the catalogue key for each field error the service
// writes in English, so the handlers can say it in the reader's language.
var fieldMessages = []fieldmsg.Message{
	{English: "Keep each priority under 120 characters.", Key: "weekly.err.priority.long"},
	{English: "Pick at most three priorities.", Key: "weekly.err.priority.many"},
	{English: "Choose hold, build or deload.", Key: "weekly.err.volume"},
}

// localize says err's field messages in ctx's language.
func localize(ctx context.Context, err error) error {
	return fieldmsg.Localize(ctx, err, fieldMessages)
}
