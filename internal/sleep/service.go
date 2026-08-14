package sleep

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/users"
)

// contextNights bounds how many nights reach the coach, same reasoning as
// goals' contextGoals: a recent run, not a growing archive.
const contextNights = 7

// Mirrors the columns' CHECK constraints so a bad value becomes a field error
// rather than a constraint violation.
var clockPattern = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

// LocalDate is the calendar day for this user at the given instant.
func LocalDate(user users.User, at time.Time) time.Time {
	loc := user.Location()
	t := at.In(loc)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
}

// Input is a night as submitted.
type Input struct {
	DurationMinutes int
	Quality         *int
	Bedtime         string
	WakeTime        string
	Notes           string
}

func Validate(in Input) (Input, error) {
	var errs apperr.FieldErrors

	switch {
	case in.DurationMinutes <= 0:
		errs = errs.Add("duration_minutes", "How long did you sleep?")
	case in.DurationMinutes > 1440:
		errs = errs.Add("duration_minutes", "That is more than a day.")
	}

	if in.Quality != nil && (*in.Quality < 1 || *in.Quality > 5) {
		errs = errs.Add("quality", "Quality must be between 1 and 5.")
	}

	in.Bedtime = strings.TrimSpace(in.Bedtime)
	if in.Bedtime != "" && !clockPattern.MatchString(in.Bedtime) {
		errs = errs.Add("bedtime", "Use 24-hour HH:MM, like 23:15.")
	}

	in.WakeTime = strings.TrimSpace(in.WakeTime)
	if in.WakeTime != "" && !clockPattern.MatchString(in.WakeTime) {
		errs = errs.Add("wake_time", "Use 24-hour HH:MM, like 07:00.")
	}

	in.Notes = strings.TrimSpace(in.Notes)
	if len(in.Notes) > 2000 {
		errs = errs.Add("notes", "Keep this under 2000 characters.")
	}

	return in, errs.OrNil()
}

// LogToday records last night against today's date, creating or correcting
// the existing entry.
func (s *Service) LogToday(ctx context.Context, user users.User, in Input) (Log, error) {
	return s.LogFor(ctx, user, LocalDate(user, time.Now()), in)
}

// LogFor records a night against a specific local date, so a morning missed
// can be filled in later.
func (s *Service) LogFor(ctx context.Context, user users.User, date time.Time, in Input) (Log, error) {
	clean, err := Validate(in)
	if err != nil {
		return Log{}, err
	}

	return s.repo.Upsert(ctx, user.ID, date, Log{
		DurationMinutes: clean.DurationMinutes,
		Quality:         clean.Quality,
		Bedtime:         clean.Bedtime,
		WakeTime:        clean.WakeTime,
		Notes:           clean.Notes,
	})
}

// Today returns last night's entry, or false when nothing is logged yet.
// Absence is normal — most of the day happens before anyone logs anything.
func (s *Service) Today(ctx context.Context, user users.User) (Log, bool, error) {
	log, err := s.repo.ForDate(ctx, user.ID, LocalDate(user, time.Now()))
	if err != nil {
		if errors.Is(err, apperr.ErrNotFound) {
			return Log{}, false, nil
		}
		return Log{}, false, err
	}
	return log, true, nil
}

// ListBetween returns the nights logged inside a window, newest first.
func (s *Service) ListBetween(ctx context.Context, user users.User, rg timerange.Range) ([]Log, error) {
	return s.repo.ListBetween(ctx, user.ID, rg.Since, rg.Until)
}

func (s *Service) Recent(ctx context.Context, user users.User, limit int) ([]Log, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	return s.repo.Recent(ctx, user.ID, limit)
}

// RecentTrend averages the trailing nights that were actually recorded.
//
// It averages over nights present rather than nights elapsed: someone who
// logged three of the last seven has a 6.5h average across three nights, not
// a 2.8h average that silently counts the four they did not record.
func (s *Service) RecentTrend(ctx context.Context, user users.User, nights int) (Trend, error) {
	if nights <= 0 || nights > 90 {
		nights = contextNights
	}

	logs, err := s.repo.Recent(ctx, user.ID, nights)
	if err != nil {
		return Trend{}, err
	}
	if len(logs) == 0 {
		return Trend{}, nil
	}

	var minutes, quality, rated int
	for _, l := range logs {
		minutes += l.DurationMinutes
		if l.Quality != nil {
			quality += *l.Quality
			rated++
		}
	}

	trend := Trend{
		AverageMinutes: float64(minutes) / float64(len(logs)),
		QualityCount:   rated,
		Nights:         len(logs),
	}
	if rated > 0 {
		trend.AverageQuality = float64(quality) / float64(rated)
	}
	return trend, nil
}
