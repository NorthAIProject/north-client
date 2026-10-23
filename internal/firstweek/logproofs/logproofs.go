// Package logproofs maps each first-week template to the log that proves its
// day. Its own package so internal/firstweek imports none of the slices that
// own those logs, and so the web and worker binaries wire it the same way.
package logproofs

import (
	"context"
	"time"

	"github.com/NorthAIProject/north-client/internal/activity"
	"github.com/NorthAIProject/north-client/internal/checkins"
	"github.com/NorthAIProject/north-client/internal/firstweek"
	"github.com/NorthAIProject/north-client/internal/lifts"
	"github.com/NorthAIProject/north-client/internal/meals"
	"github.com/NorthAIProject/north-client/internal/mind"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/sleep"
	"github.com/NorthAIProject/north-client/internal/users"
)

// For maps each first-week template to the log that proves its
// day: the slices own the logs, firstweek only asks whether one exists.
func For(
	sleepSvc *sleep.Service, activitySvc *activity.Service, liftSvc *lifts.Service,
	foodLog *meals.FoodLogService, mindSvc *mind.Service, checkinSvc *checkins.Service,
) map[string]firstweek.Proof {
	return map[string]firstweek.Proof{
		firstweek.TemplateSleep: firstweek.Listed(sleepSvc.ListBetween),
		firstweek.TemplateTrain: firstweek.Either(
			firstweek.ListedByID(activitySvc.ListBetween),
			firstweek.Listed(liftSvc.Between),
		),
		firstweek.TemplateProtein: firstweek.ProofFunc(func(ctx context.Context, user users.User, day timerange.Range) (bool, error) {
			d := day.Since
			entries, err := foodLog.Day(ctx, user.ID, time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC))
			return len(entries) > 0, err
		}),
		firstweek.TemplateJournal: firstweek.ListedByID(mindSvc.ListBetween),
		firstweek.TemplateCustom:  firstweek.ListedByID(checkinSvc.ListBetween),
	}
}
