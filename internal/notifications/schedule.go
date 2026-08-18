package notifications

import (
	"fmt"

	"github.com/google/uuid"

	notificationsdb "github.com/NorthAIProject/north-client/internal/notifications/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// Schedule kinds. A new kind is a code change, not a migration.
const (
	KindPhoto = "photo"
)

// AllowedEveryDays is what the settings form and the MCP tool offer.
var AllowedEveryDays = []int{7, 14, 21, 28}

// AllowedReminderDays is 0 (no reminder) or a few days after the ask.
var AllowedReminderDays = []int{0, 2, 3, 7}

// Schedule is one recurring ask the person configured.
type Schedule struct {
	UserID       uuid.UUID
	Kind         string
	Enabled      bool
	EveryDays    int
	ReminderDays int
}

// DefaultPhoto is the photo check-in until someone changes it:
// every two weeks, remind two days later if nothing arrived.
func DefaultPhoto(userID uuid.UUID) Schedule {
	return Schedule{
		UserID:       userID,
		Kind:         KindPhoto,
		Enabled:      true,
		EveryDays:    14,
		ReminderDays: 2,
	}
}

// ScheduleInput is a write from settings or MCP.
type ScheduleInput struct {
	Kind         string
	Enabled      bool
	EveryDays    int
	ReminderDays int
}

func ValidateSchedule(in ScheduleInput) (ScheduleInput, error) {
	var errs apperr.FieldErrors

	switch in.Kind {
	case KindPhoto:
	case "":
		in.Kind = KindPhoto
	default:
		errs = errs.Add("kind", "Unknown alert. Use photo.")
	}

	if !containsInt(AllowedEveryDays, in.EveryDays) {
		errs = errs.Add("every_days", "Choose every 1, 2, 3, or 4 weeks.")
	}
	if !containsInt(AllowedReminderDays, in.ReminderDays) {
		errs = errs.Add("reminder_days", "Choose no reminder, or 2, 3, or 7 days later.")
	}

	return in, errs.OrNil()
}

func (s Schedule) Line() string {
	if !s.Enabled {
		return fmt.Sprintf("%s: off", s.Kind)
	}
	line := fmt.Sprintf("%s: every %d days", s.Kind, s.EveryDays)
	if s.ReminderDays > 0 {
		line += fmt.Sprintf(", remind after %d days if nothing arrives", s.ReminderDays)
	}
	return line
}

func scheduleFromDB(row notificationsdb.UserAlertSchedule) Schedule {
	return Schedule{
		UserID:       row.UserID,
		Kind:         row.Kind,
		Enabled:      row.Enabled,
		EveryDays:    int(row.EveryDays),
		ReminderDays: int(row.ReminderDays),
	}
}

func containsInt(have []int, n int) bool {
	for _, v := range have {
		if v == n {
			return true
		}
	}
	return false
}
