package habits

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/habits/habit"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/lifedomain"
	"github.com/NorthAIProject/north-client/internal/users"
)

// adherenceWindow is the trailing window Stats reports over. A week is what a
// person can hold in their head, and matches how they talk about a habit
// ("I've been good this week").
const adherenceWindow = 7

// statsLookback bounds the completion history loaded to compute streaks.
// Longer than the adherence window because a streak can be much older than a
// week, but still bounded so one query stays one query.
const statsLookback = 400

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

// Input is a habit as submitted.
type Input struct {
	Name   string
	Domain string
	Days   []time.Weekday
	Active bool
}

func Validate(in Input) (Input, error) {
	var errs apperr.FieldErrors

	in.Name = strings.TrimSpace(in.Name)
	switch {
	case in.Name == "":
		errs = errs.Add("name", "Give the habit a name.")
	case len(in.Name) > 120:
		errs = errs.Add("name", "Keep this under 120 characters.")
	}

	in.Domain = strings.TrimSpace(in.Domain)
	if in.Domain == "" {
		in.Domain = lifedomain.Personal
	} else if !lifedomain.Valid(in.Domain) {
		errs = errs.Add("domain", "Pick one of the listed areas.")
	}

	// A habit due on no days can never be kept or missed, so it is a bug
	// rather than a preference. Duplicates are collapsed and the list sorted
	// so storage order never affects behaviour.
	in.Days = normalizeDays(in.Days)
	if len(in.Days) == 0 {
		errs = errs.Add("days", "Choose at least one day.")
	}

	return in, errs.OrNil()
}

func normalizeDays(days []time.Weekday) []time.Weekday {
	seen := make(map[time.Weekday]bool, len(days))
	out := make([]time.Weekday, 0, len(days))
	for _, d := range days {
		if d < time.Sunday || d > time.Saturday || seen[d] {
			continue
		}
		seen[d] = true
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func (s *Service) Create(ctx context.Context, user users.User, in Input) (Habit, error) {
	clean, err := Validate(in)
	if err != nil {
		return Habit{}, err
	}
	return s.repo.Create(ctx, user.ID, clean.Name, clean.Domain, clean.Days)
}

func (s *Service) Update(ctx context.Context, user users.User, id uuid.UUID, in Input) (Habit, error) {
	clean, err := Validate(in)
	if err != nil {
		return Habit{}, err
	}
	return s.repo.Update(ctx, id, user.ID, clean.Name, clean.Domain, clean.Days, clean.Active)
}

func (s *Service) Get(ctx context.Context, user users.User, id uuid.UUID) (Habit, error) {
	return s.repo.Get(ctx, id, user.ID)
}

func (s *Service) List(ctx context.Context, user users.User, activeOnly bool) ([]Habit, error) {
	return s.repo.List(ctx, user.ID, activeOnly)
}

func (s *Service) Delete(ctx context.Context, user users.User, id uuid.UUID) error {
	return s.repo.Delete(ctx, id, user.ID)
}

// Complete ticks a habit for today. Idempotent, so a double tap is harmless.
func (s *Service) Complete(ctx context.Context, user users.User, id uuid.UUID) error {
	if _, err := s.repo.Get(ctx, id, user.ID); err != nil {
		return err // also the ownership check
	}
	return s.repo.Complete(ctx, id, user.ID, LocalDate(user, time.Now()))
}

// Uncomplete unticks today, for the inevitable mis-tap.
func (s *Service) Uncomplete(ctx context.Context, user users.User, id uuid.UUID) error {
	if _, err := s.repo.Get(ctx, id, user.ID); err != nil {
		return err
	}
	return s.repo.Uncomplete(ctx, id, user.ID, LocalDate(user, time.Now()))
}

// Today lists the active habits with their streaks and adherence.
//
// One completions query serves every habit, so this stays two round trips
// regardless of how many habits someone keeps.
func (s *Service) Today(ctx context.Context, user users.User) ([]Stats, error) {
	habitList, err := s.repo.List(ctx, user.ID, true)
	if err != nil {
		return nil, err
	}
	if len(habitList) == 0 {
		return nil, nil
	}

	today := LocalDate(user, time.Now())

	completions, err := s.repo.CompletionsSince(ctx, user.ID, today.AddDate(0, 0, -statsLookback))
	if err != nil {
		return nil, err
	}

	out := make([]Stats, 0, len(habitList))
	for _, h := range habitList {
		done := habit.NewDateSet(completions[h.ID])
		kept, scheduled := habit.Adherence(h, done, today, adherenceWindow)

		out = append(out, Stats{
			Habit:          h,
			Streak:         habit.Streak(h, done, today),
			Kept:           kept,
			Scheduled:      scheduled,
			DoneToday:      done.Has(today),
			ScheduledToday: h.ScheduledOn(today),
		})
	}
	return out, nil
}
