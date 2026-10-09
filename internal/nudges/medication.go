package nudges

import (
	"context"
	"time"

	"github.com/NorthAIProject/north-client/internal/medications/medication"
	"github.com/NorthAIProject/north-client/internal/users"
)

// medicationSource answers which scheduled doses are due and unanswered right
// now: slots of medications set to remind, within two hours of their time,
// with nothing logged. medications.Service satisfies it.
type medicationSource interface {
	DueReminders(ctx context.Context, user users.User, now time.Time) ([]medication.Reminder, error)
}

// evalMedicationReminders raises one nudge per due dose. Logging the dose
// takes it off the due list, so a sweep after that raises nothing for it; the
// dedupe key keeps any one slot from being raised twice in a day.
//
// Raised as an Alarm, so it goes out in quiet hours too: the person picked the
// time, and holding a dose reminder until morning defeats it.
func (s *Service) evalMedicationReminders(ctx context.Context, user users.User, today, now time.Time) (int, error) {
	if s.meds == nil {
		return 0, nil
	}
	due, err := s.meds.DueReminders(ctx, user, now)
	if err != nil {
		return 0, err
	}

	created := 0
	for _, r := range due {
		_, inserted, raiseErr := s.Raise(ctx, user, Draft{
			Kind:      KindMedicationReminder,
			DedupeKey: today.Format("2006-01-02") + ":" + r.Medication.ID.String() + ":" + r.Slot,
			Title:     r.Medication.Label(),
			Body:      "Your " + r.Slot + " dose. Mark it taken or skipped when you can.",
			Href:      "/app/care",
			Alarm:     true,
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
