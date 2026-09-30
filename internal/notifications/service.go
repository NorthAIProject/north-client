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
	StatsDigestCadence string
	QuietHoursEnabled  bool
	QuietStart         string
	QuietEnd           string

	// The three below are pointers because a client that predates them
	// sends nothing, and nothing must mean "keep what is saved" rather than
	// "midnight, off". Upsert fills a nil one from the stored row.
	BriefingHour      *int
	EveningReflection *bool
	EveningHour       *int
}

// Validate normalises the window and rejects times the column would refuse.
//
// The quiet times are checked even when the window is switched off, because
// the form submits them either way and storing a value the CHECK constraint
// would bounce turns a typo into a 500 on the next save.
func Validate(in Input) (Input, error) {
	var errs apperr.FieldErrors

	in.StatsDigestCadence = normalizeCadence(in.StatsDigestCadence, &errs)

	in.QuietStart = normalizeHourMinute(in.QuietStart, defaultQuietStart, &errs, "quiet_start")
	in.QuietEnd = normalizeHourMinute(in.QuietEnd, defaultQuietEnd, &errs, "quiet_end")

	// A window that starts where it ends is not a window. Left as a field
	// error rather than silently disabled: somebody set both to the same time
	// on purpose and deserves to know it would have muted nothing.
	if in.QuietHoursEnabled && in.QuietStart == in.QuietEnd {
		errs = errs.Add("quiet_end", "Quiet hours must start and end at different times.")
	}
	if in.BriefingHour != nil && (*in.BriefingHour < 0 || *in.BriefingHour > 23) {
		errs = errs.Add("briefing_hour", "Choose an hour between 0 and 23.")
	}
	if in.EveningHour != nil && (*in.EveningHour < 0 || *in.EveningHour > 23) {
		errs = errs.Add("evening_hour", "Choose an hour between 0 and 23.")
	}

	return in, errs.OrNil()
}

// normalizeCadence fills in the default and refuses anything the column would
// bounce. A stale form from a build that offered a cadence this one no longer
// does would otherwise surface a constraint violation as a 500.
func normalizeCadence(value string, errs *apperr.FieldErrors) string {
	switch value {
	case "":
		return DefaultCadence
	case CadenceOff, CadenceDaily, CadenceWeekly, CadenceMonthly:
		return value
	default:
		*errs = errs.Add("stats_digest_cadence", "Choose how often you want your numbers.")
		return value
	}
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

	// DefaultBriefingHour and DefaultEveningHour mirror the column defaults.
	DefaultBriefingHour = 7
	DefaultEveningHour  = 21
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
		BriefingHour:       DefaultBriefingHour,
		EveningReflection:  false,
		EveningHour:        DefaultEveningHour,
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
	if clean.BriefingHour == nil || clean.EveningReflection == nil || clean.EveningHour == nil {
		current, err := s.Get(ctx, userID)
		if err != nil {
			return Prefs{}, err
		}
		clean = clean.Keeping(current)
	}
	return s.repo.Upsert(ctx, userID, clean)
}

// Keeping fills whichever of the timing fields the caller left out from p.
func (in Input) Keeping(p Prefs) Input {
	if in.BriefingHour == nil {
		in.BriefingHour = &p.BriefingHour
	}
	if in.EveningReflection == nil {
		in.EveningReflection = &p.EveningReflection
	}
	if in.EveningHour == nil {
		in.EveningHour = &p.EveningHour
	}
	return in
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
