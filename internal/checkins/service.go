package checkins

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/goals"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

const (
	contextDays  = 14
	contextLimit = 10
	listDefault  = 30
	streakLook   = 400
)

// GoalLookup verifies optional goal links belong to the user.
type GoalLookup interface {
	Get(ctx context.Context, id, userID uuid.UUID) (goals.Goal, error)
}

type Service struct {
	repo  *Repository
	goals GoalLookup
}

func NewService(repo *Repository, goals GoalLookup) *Service {
	return &Service{repo: repo, goals: goals}
}

// Input is a check-in as submitted.
type Input struct {
	Mood, Energy  int
	Wins          string
	Challenges    string
	Notes         string
	RelatedGoalID *uuid.UUID
}

// Validate checks a check-in before it is stored.
func Validate(in Input) (Input, error) {
	var errs apperr.FieldErrors

	if in.Mood < 1 || in.Mood > 5 {
		errs = errs.Add("mood", "Pick a mood from 1 to 5.")
	}
	if in.Energy < 1 || in.Energy > 5 {
		errs = errs.Add("energy", "Pick an energy level from 1 to 5.")
	}

	in.Wins = strings.TrimSpace(in.Wins)
	if len(in.Wins) > 500 {
		errs = errs.Add("wins", "Keep wins under 500 characters.")
	}
	in.Challenges = strings.TrimSpace(in.Challenges)
	if len(in.Challenges) > 500 {
		errs = errs.Add("challenges", "Keep challenges under 500 characters.")
	}
	in.Notes = strings.TrimSpace(in.Notes)
	if len(in.Notes) > 1000 {
		errs = errs.Add("notes", "Keep notes under 1000 characters.")
	}

	return in, errs.OrNil()
}

// LocalDate is the calendar day for this user at the given instant.
func LocalDate(user users.User, at time.Time) time.Time {
	loc := user.Location()
	t := at.In(loc)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
}

// UpsertToday creates or updates today's check-in in the user's timezone.
func (s *Service) UpsertToday(ctx context.Context, user users.User, in Input) (CheckIn, error) {
	clean, err := Validate(in)
	if err != nil {
		return CheckIn{}, err
	}
	if err := s.checkGoal(ctx, user.ID, clean.RelatedGoalID); err != nil {
		return CheckIn{}, err
	}

	return s.repo.Upsert(ctx, user.ID, Write{
		LocalDate:     LocalDate(user, time.Now()),
		Mood:          clean.Mood,
		Energy:        clean.Energy,
		Wins:          clean.Wins,
		Challenges:    clean.Challenges,
		Notes:         clean.Notes,
		RelatedGoalID: clean.RelatedGoalID,
	})
}

func (s *Service) Get(ctx context.Context, id, userID uuid.UUID) (CheckIn, error) {
	return s.repo.Get(ctx, id, userID)
}

func (s *Service) Today(ctx context.Context, user users.User) (CheckIn, error) {
	return s.repo.GetByDate(ctx, user.ID, LocalDate(user, time.Now()))
}

func (s *Service) List(ctx context.Context, userID uuid.UUID, limit int) ([]CheckIn, error) {
	if limit <= 0 || limit > 100 {
		limit = listDefault
	}
	list, err := s.repo.List(ctx, userID, limit)
	if err != nil {
		return nil, err
	}
	return s.withGoalTitles(ctx, userID, list), nil
}

func (s *Service) Update(ctx context.Context, id, userID uuid.UUID, in Input) (CheckIn, error) {
	clean, err := Validate(in)
	if err != nil {
		return CheckIn{}, err
	}
	if err := s.checkGoal(ctx, userID, clean.RelatedGoalID); err != nil {
		return CheckIn{}, err
	}
	return s.repo.Update(ctx, id, userID, Write{
		Mood:          clean.Mood,
		Energy:        clean.Energy,
		Wins:          clean.Wins,
		Challenges:    clean.Challenges,
		Notes:         clean.Notes,
		RelatedGoalID: clean.RelatedGoalID,
	})
}

// Delete removes a check-in. It only affects rows the caller owns.
func (s *Service) Delete(ctx context.Context, id, userID uuid.UUID) error {
	return s.repo.Delete(ctx, id, userID)
}

// RecentForContext returns the last two weeks of check-ins for the coach.
func (s *Service) RecentForContext(ctx context.Context, user users.User) ([]CheckIn, error) {
	since := LocalDate(user, time.Now()).AddDate(0, 0, -(contextDays - 1))
	list, err := s.repo.ListSince(ctx, user.ID, since, contextLimit)
	if err != nil {
		return nil, err
	}
	return s.withGoalTitles(ctx, user.ID, list), nil
}

// withGoalTitles fills RelatedGoalTitle on each check-in that links to a
// goal. Lookups are best-effort: a goal that fails to resolve (deleted,
// lookup unavailable) leaves the title blank rather than failing the list.
func (s *Service) withGoalTitles(ctx context.Context, userID uuid.UUID, list []CheckIn) []CheckIn {
	if s.goals == nil {
		return list
	}
	titles := make(map[uuid.UUID]string)
	for i, c := range list {
		if c.RelatedGoalID == nil {
			continue
		}
		title, ok := titles[*c.RelatedGoalID]
		if !ok {
			g, err := s.goals.Get(ctx, *c.RelatedGoalID, userID)
			if err != nil {
				continue
			}
			title = g.Title
			titles[*c.RelatedGoalID] = title
		}
		list[i].RelatedGoalTitle = title
	}
	return list
}

// Streak is consecutive local days with a check-in, ending today or yesterday.
// Zero is not failure — it is "not started", and the UI should stay quiet.
func (s *Service) Streak(ctx context.Context, user users.User) (int, error) {
	dates, err := s.repo.Dates(ctx, user.ID, streakLook)
	if err != nil {
		return 0, err
	}
	if len(dates) == 0 {
		return 0, nil
	}

	today := LocalDate(user, time.Now())
	// Normalise stored dates to comparable date-only in the same location.
	have := make(map[string]bool, len(dates))
	for _, d := range dates {
		have[dateKey(d)] = true
	}

	start := today
	if !have[dateKey(today)] {
		// Allow the streak to end on yesterday if they have not checked in yet today.
		yesterday := today.AddDate(0, 0, -1)
		if !have[dateKey(yesterday)] {
			return 0, nil
		}
		start = yesterday
	}

	streak := 0
	for day := start; have[dateKey(day)]; day = day.AddDate(0, 0, -1) {
		streak++
	}
	return streak, nil
}

func (s *Service) checkGoal(ctx context.Context, userID uuid.UUID, goalID *uuid.UUID) error {
	if goalID == nil || s.goals == nil {
		return nil
	}
	if _, err := s.goals.Get(ctx, *goalID, userID); err != nil {
		if apperr.Is(err, apperr.ErrNotFound) {
			return apperr.FieldErrors{{Field: "related_goal_id", Message: "That goal is not available."}}
		}
		return err
	}
	return nil
}

func dateKey(t time.Time) string {
	return t.UTC().Format("2006-01-02")
}
