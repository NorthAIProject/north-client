package notifications

import (
	"context"

	"github.com/google/uuid"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

// Input is the settings form as submitted. Checkboxes arrive as booleans the
// handler has already read; the times arrive as the raw "HH:MM" strings.
type Input struct {
	NudgeMissedCheckIn bool
	NudgeGoalDeadline  bool
	CoachActivity      bool
	TrainingReminders  bool
	WeeklyReportAuto   bool
	DailyBriefingAuto  bool
	QuietHoursEnabled  bool
	QuietStart         string
	QuietEnd           string
}

// Validate normalises the window and rejects times the column would refuse.
//
// The quiet times are checked even when the window is switched off, because
// the form submits them either way and storing a value the CHECK constraint
// would bounce turns a typo into a 500 on the next save.
func Validate(in Input) (Input, error) {
	var errs apperr.FieldErrors

	in.QuietStart = normalizeHourMinute(in.QuietStart, defaultQuietStart, &errs, "quiet_start")
	in.QuietEnd = normalizeHourMinute(in.QuietEnd, defaultQuietEnd, &errs, "quiet_end")

	// A window that starts where it ends is not a window. Left as a field
	// error rather than silently disabled: somebody set both to the same time
	// on purpose and deserves to know it would have muted nothing.
	if in.QuietHoursEnabled && in.QuietStart == in.QuietEnd {
		errs = errs.Add("quiet_end", "Quiet hours must start and end at different times.")
	}

	return in, errs.OrNil()
}

func normalizeHourMinute(value, fallback string, errs *apperr.FieldErrors, field string) string {
	if value == "" {
		return fallback
	}
	if _, ok := parseHourMinute(value); !ok {
		*errs = errs.Add(field, "Use a time like 22:00.")
		return value
	}
	return value
}

const (
	defaultQuietStart = "22:00"
	defaultQuietEnd   = "07:00"
)

// defaults is what an account is treated as having before anyone has saved
// anything. They mirror the column defaults: the two nudge kinds stay on
// because that is what the sweep already does, and the weekly review stays
// opt-in because generating one spends a model call.
func defaults() Prefs {
	return Prefs{
		NudgeMissedCheckIn: true,
		NudgeGoalDeadline:  true,
		CoachActivity:      true,
		TrainingReminders:  true,
		WeeklyReportAuto:   false,
		DailyBriefingAuto:  false,
		QuietHoursEnabled:  false,
		QuietStart:         defaultQuietStart,
		QuietEnd:           defaultQuietEnd,
	}
}

// Defaults exposes the unconfigured settings, for callers that need them
// without a database round trip.
func Defaults() Prefs { return defaults() }

// Get returns saved preferences, or the defaults when the account has never
// saved any. A missing row is an unconfigured account, not a failure, so this
// never returns apperr.ErrNotFound.
func (s *Service) Get(ctx context.Context, userID uuid.UUID) (Prefs, error) {
	p, err := s.repo.Get(ctx, userID)
	if err != nil {
		if apperr.Is(err, apperr.ErrNotFound) {
			d := defaults()
			d.UserID = userID
			return d, nil
		}
		return Prefs{}, err
	}
	return p, nil
}

func (s *Service) Upsert(ctx context.Context, userID uuid.UUID, in Input) (Prefs, error) {
	clean, err := Validate(in)
	if err != nil {
		return Prefs{}, err
	}
	return s.repo.Upsert(ctx, userID, clean)
}

// PhotoSchedule is the photo check-in cadence, or the default if they have
// never set one.
func (s *Service) PhotoSchedule(ctx context.Context, userID uuid.UUID) (Schedule, error) {
	got, err := s.repo.GetSchedule(ctx, userID, KindPhoto)
	if err != nil {
		if apperr.Is(err, apperr.ErrNotFound) {
			return DefaultPhoto(userID), nil
		}
		return Schedule{}, err
	}
	return got, nil
}

func (s *Service) ListSchedules(ctx context.Context, userID uuid.UUID) ([]Schedule, error) {
	list, err := s.repo.ListSchedules(ctx, userID)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return []Schedule{DefaultPhoto(userID)}, nil
	}
	hasPhoto := false
	for _, item := range list {
		if item.Kind == KindPhoto {
			hasPhoto = true
			break
		}
	}
	if !hasPhoto {
		list = append([]Schedule{DefaultPhoto(userID)}, list...)
	}
	return list, nil
}

func (s *Service) UpsertSchedule(ctx context.Context, userID uuid.UUID, in ScheduleInput) (Schedule, error) {
	clean, err := ValidateSchedule(in)
	if err != nil {
		return Schedule{}, err
	}
	return s.repo.UpsertSchedule(ctx, userID, clean)
}
