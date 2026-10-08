// Package weekly closes the week: a Sunday review of the week that happened,
// ending in a focus for the next one — up to three priorities, the order of
// active goals, and how hard to train. The focus reaches the goals list, the
// training plan and the coach; nothing else in the app has to ask for it.
package weekly

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/FACorreiaa/go-utils/pkg/util"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/goals"
	"github.com/NorthAIProject/north-client/internal/reports"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/users"
	weeklydb "github.com/NorthAIProject/north-client/internal/weekly/db"
	"github.com/NorthAIProject/north-client/internal/weekly/week"
	"github.com/NorthAIProject/north-client/internal/workouts/plan"
)

type (
	Focus  = week.Focus
	Review = week.Review
	Input  = week.Input
)

// Goals is the part of goals.Service the review uses.
type Goals interface {
	ListActive(ctx context.Context, userID uuid.UUID) ([]goals.Goal, error)
	Rank(ctx context.Context, userID uuid.UUID, ids []uuid.UUID) error
}

// Reports is the part of reports.Service the review uses.
type Reports interface {
	ListKind(ctx context.Context, userID uuid.UUID, kind reports.Kind, includeArchived bool) ([]reports.Report, error)
	EnsureWeekly(ctx context.Context, userID uuid.UUID, week reports.Week) (reports.Report, bool, error)
}

// Achievements records a finished review. achievements.Service satisfies it.
type Achievements interface {
	WeekReviewed(ctx context.Context, userID uuid.UUID, weekStart, at time.Time)
}

type Service struct {
	q            *weeklydb.Queries
	goals        Goals
	reports      Reports
	achievements Achievements
	now          func() time.Time
}

func NewService(pool *pgxpool.Pool, g Goals, r Reports) *Service {
	return &Service{q: weeklydb.New(pool), goals: g, reports: r, now: time.Now}
}

func (s *Service) WithAchievements(a Achievements) *Service { s.achievements = a; return s }

// WithClock fixes now, for tests.
func (s *Service) WithClock(now func() time.Time) *Service { s.now = now; return s }

// PlanningWeek is the Monday of the week a review done at now plans: from
// Friday on it is next week, before that it is this one, so a review done on
// a Monday morning still counts for the week it is in.
func PlanningWeek(now time.Time, loc *time.Location) time.Time {
	local := now.In(loc)
	monday := timerange.StartOfWeek(local)
	switch local.Weekday() {
	case time.Friday, time.Saturday, time.Sunday:
		return monday.AddDate(0, 0, 7)
	default:
		return monday
	}
}

// Review gathers the week that happened and the one being planned. It asks
// for the weekly report if there is none yet: on a Sunday evening the
// Monday-morning report has not been written, and the review is the reason to
// write it now.
func (s *Service) Review(ctx context.Context, user users.User) (Review, error) {
	planning := PlanningWeek(s.now(), user.Location())
	reviewing := planning.AddDate(0, 0, -7)
	out := Review{Reviewing: reviewing, Planning: planning}

	var err error
	if out.Report, err = s.reportFor(ctx, user, reviewing); err != nil {
		return Review{}, err
	}
	if out.Last, err = s.focus(ctx, user.ID, reviewing); err != nil {
		return Review{}, err
	}
	if out.Current, err = s.focus(ctx, user.ID, planning); err != nil {
		return Review{}, err
	}
	active, err := s.goals.ListActive(ctx, user.ID)
	if err != nil {
		return Review{}, err
	}
	out.Goals = make([]week.Goal, 0, len(active))
	for _, g := range active {
		out.Goals = append(out.Goals, week.Goal{ID: g.ID, Title: g.Title, Category: g.Category, Priority: g.Priority})
	}
	return out, nil
}

func (s *Service) reportFor(ctx context.Context, user users.User, monday time.Time) (*week.Report, error) {
	list, err := s.reports.ListKind(ctx, user.ID, reports.KindWeekly, true)
	if err != nil {
		return nil, err
	}
	for _, r := range list {
		if sameDay(r.PeriodStart.In(user.Location()), monday) {
			return &week.Report{ID: r.ID, Title: r.Title, Body: r.Body, Ready: r.Status == reports.StatusReady}, nil
		}
	}
	w := reports.WeekContaining(monday, user.Location())
	r, _, err := s.reports.EnsureWeekly(ctx, user.ID, w)
	if err != nil {
		return nil, err
	}
	return &week.Report{ID: r.ID, Title: r.Title, Body: r.Body, Ready: r.Status == reports.StatusReady}, nil
}

// SetFocus ends a review: the focus is stored for the week being planned,
// goals take the order given, and the review counts as an achievement.
func (s *Service) SetFocus(ctx context.Context, user users.User, in Input) (Focus, error) {
	in, err := validate(in)
	if err != nil {
		return Focus{}, err
	}
	if err = s.goals.Rank(ctx, user.ID, in.GoalOrder); err != nil {
		return Focus{}, err
	}
	planning := PlanningWeek(s.now(), user.Location())
	row, err := s.q.UpsertFocus(ctx, weeklydb.UpsertFocusParams{
		UserID: user.ID, WeekStart: date(planning), Priorities: in.Priorities, Volume: string(in.Volume),
	})
	if err != nil {
		return Focus{}, apperr.Wrap(err, "save weekly focus")
	}
	if s.achievements != nil {
		s.achievements.WeekReviewed(ctx, user.ID, planning, s.now())
	}
	return fromDB(row, user.Location()), nil
}

func validate(in Input) (Input, error) {
	var errs apperr.FieldErrors
	priorities := make([]string, 0, len(in.Priorities))
	for _, p := range in.Priorities {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if len([]rune(p)) > 120 {
			errs = errs.Add("priorities", "Keep each priority under 120 characters.")
			break
		}
		priorities = append(priorities, p)
	}
	if len(priorities) > week.MaxPriorities {
		errs = errs.Add("priorities", "Pick at most three priorities.")
	}
	if in.Volume == "" {
		in.Volume = plan.VolumeHold
	}
	if !in.Volume.Valid() {
		errs = errs.Add("volume", "Choose hold, build or deload.")
	}
	if len(errs) > 0 {
		return Input{}, errs
	}
	in.Priorities = priorities
	return in, nil
}

// Current is the focus for the week containing at, if one was set.
func (s *Service) Current(ctx context.Context, user users.User, at time.Time) (*Focus, error) {
	return s.focus(ctx, user.ID, timerange.StartOfWeek(at.In(user.Location())))
}

// Reviewed reports whether the week starting at monday already has a focus,
// so the Sunday nudge does not ask twice.
func (s *Service) Reviewed(ctx context.Context, user users.User, monday time.Time) (bool, error) {
	f, err := s.focus(ctx, user.ID, monday)
	return f != nil, err
}

// VolumeFor is the training volume chosen for the week containing at; hold
// when the week was never reviewed. It satisfies workouts.VolumeSource.
func (s *Service) VolumeFor(ctx context.Context, user users.User, at time.Time) (plan.Volume, error) {
	f, err := s.Current(ctx, user, at)
	if err != nil || f == nil {
		return plan.VolumeHold, err
	}
	return f.Volume, nil
}

func (s *Service) focus(ctx context.Context, userID uuid.UUID, monday time.Time) (*Focus, error) {
	row, err := s.q.GetFocus(ctx, weeklydb.GetFocusParams{UserID: userID, WeekStart: date(monday)})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, apperr.Wrap(err, "load weekly focus")
	}
	return util.Ptr(fromDB(row, monday.Location())), nil
}

func fromDB(row weeklydb.WeeklyFocus, loc *time.Location) Focus {
	d := row.WeekStart.Time
	return Focus{
		WeekStart:  time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, loc),
		Priorities: row.Priorities,
		Volume:     plan.Volume(row.Volume),
		ReviewedAt: row.ReviewedAt,
	}
}

func date(t time.Time) pgtype.Date {
	return pgtype.Date{Time: time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC), Valid: true}
}

func sameDay(a, b time.Time) bool {
	return a.Year() == b.Year() && a.YearDay() == b.YearDay()
}
