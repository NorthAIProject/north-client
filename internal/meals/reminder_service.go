package meals

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

type MealReminderService struct {
	repo *Repository
}

func NewMealReminderService(repo *Repository) *MealReminderService {
	return &MealReminderService{repo: repo}
}

// ReminderInput is a reminder as submitted.
type ReminderInput struct {
	Label      string
	TimeOfDay  string
	DaysOfWeek []int
}

func ValidateReminder(in ReminderInput) (ReminderInput, error) {
	var errs apperr.FieldErrors

	in.Label = strings.TrimSpace(in.Label)
	if in.Label == "" {
		errs = errs.Add("label", "Give the reminder a name, like \"log lunch\".")
	}

	in.TimeOfDay = strings.TrimSpace(in.TimeOfDay)
	if _, err := time.Parse("15:04", in.TimeOfDay); err != nil {
		errs = errs.Add("time_of_day", "Enter a time as HH:MM.")
	}

	if len(in.DaysOfWeek) == 0 {
		in.DaysOfWeek = []int{0, 1, 2, 3, 4, 5, 6}
	} else {
		for _, d := range in.DaysOfWeek {
			if d < 0 || d > 6 {
				errs = errs.Add("days_of_week", "Days must be between 0 (Sunday) and 6 (Saturday).")
				break
			}
		}
	}

	return in, errs.OrNil()
}

func (s *MealReminderService) Create(ctx context.Context, userID uuid.UUID, in ReminderInput) (Reminder, error) {
	clean, err := ValidateReminder(in)
	if err != nil {
		return Reminder{}, err
	}
	return s.repo.CreateReminder(ctx, userID, clean.Label, clean.TimeOfDay, clean.DaysOfWeek)
}

func (s *MealReminderService) List(ctx context.Context, userID uuid.UUID) ([]Reminder, error) {
	return s.repo.ListReminders(ctx, userID)
}

func (s *MealReminderService) Update(ctx context.Context, id, userID uuid.UUID, in ReminderInput) (Reminder, error) {
	clean, err := ValidateReminder(in)
	if err != nil {
		return Reminder{}, err
	}
	return s.repo.UpdateReminder(ctx, id, userID, clean.Label, clean.TimeOfDay, clean.DaysOfWeek)
}

func (s *MealReminderService) Delete(ctx context.Context, id, userID uuid.UUID) error {
	return s.repo.DeleteReminder(ctx, id, userID)
}

func (s *MealReminderService) Toggle(ctx context.Context, id, userID uuid.UUID, enabled bool) (Reminder, error) {
	return s.repo.SetReminderEnabled(ctx, id, userID, enabled)
}

// DueNow returns the reminders that should fire as of `at`, which callers
// pass already converted to the user's local time (the same responsibility
// checkins.LocalDate places on its caller). Each returned reminder is
// immediately marked fired for that local date, so calling this twice in the
// same day returns it only once.
func (s *MealReminderService) DueNow(ctx context.Context, userID uuid.UUID, at time.Time) ([]Reminder, error) {
	asOfDate := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, at.Location())

	candidates, err := s.repo.NotYetFiredToday(ctx, userID, asOfDate)
	if err != nil {
		return nil, err
	}

	nowHHMM := at.Format("15:04")

	var due []Reminder
	for _, r := range candidates {
		if !r.DueOn(at.Weekday(), nowHHMM) {
			continue
		}
		if err := s.repo.MarkFired(ctx, r.ID, asOfDate); err != nil {
			return nil, err
		}
		due = append(due, r)
	}

	return due, nil
}
