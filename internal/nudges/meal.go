package nudges

import (
	"context"
	"time"

	"github.com/NorthAIProject/north-client/internal/users"
)

const (
	// A meal logged shortly before the reminder time already answers it:
	// lunch eaten at 12:40 needs no "log lunch" at 13:00.
	mealLoggedGrace = time.Hour

	// Past this, the reminder is history rather than a prompt. It keeps a
	// worker that was down all morning from sending breakfast at dinner.
	mealReminderWindow = 2 * time.Hour
)

// evalMealReminders raises each meal reminder the person set for today once
// its time has come, unless they already logged food around then. Logging a
// meal resolves the reminder, in the bell and in the chat.
func (s *Service) evalMealReminders(ctx context.Context, user users.User, today, now time.Time) (int, error) {
	if s.meals == nil {
		return 0, nil
	}
	reminders, err := s.meals.MealReminders(ctx, user.ID)
	if err != nil {
		return 0, err
	}

	local := now.In(user.Location())
	created := 0
	for _, r := range reminders {
		if !r.DueOn(local.Weekday(), local.Format("15:04")) {
			continue
		}
		clock, parseErr := time.Parse("15:04", r.TimeOfDay)
		if parseErr != nil {
			continue // validated on save; a bad row is not worth stopping the sweep
		}
		dueAt := time.Date(today.Year(), today.Month(), today.Day(), clock.Hour(), clock.Minute(), 0, 0, user.Location())
		if local.Sub(dueAt) > mealReminderWindow {
			continue
		}

		logged, logErr := s.meals.LoggedFoodSince(ctx, user.ID, dueAt.Add(-mealLoggedGrace))
		if logErr != nil {
			return created, logErr
		}
		if logged {
			continue
		}

		_, inserted, raiseErr := s.Raise(ctx, user, Draft{
			Kind:      KindMealReminder,
			DedupeKey: today.Format("2006-01-02") + ":" + r.ID.String(),
			Title:     r.Label,
			Body:      "Log what you ate when you get a moment.",
			Href:      "/app/nutrition/log",
		})
		if raiseErr != nil {
			return created, raiseErr
		}
		if inserted {
			created++
		}
	}
	return created, nil
}
