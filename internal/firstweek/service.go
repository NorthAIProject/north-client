package firstweek

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/analytics"
	"github.com/NorthAIProject/north-client/internal/goals"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/users"
)

// Proof says whether a template's daily action was logged inside a local
// day. One per template; see proofs.go.
type Proof interface {
	LoggedOn(ctx context.Context, user users.User, day timerange.Range) (bool, error)
}

// Goals creates the week's goal when the start did not bring one.
// goals.Service satisfies it.
type Goals interface {
	Create(ctx context.Context, userID uuid.UUID, in goals.Input) (goals.Goal, error)
}

// Users loads the person a programme belongs to, for the sweep.
// users.Service satisfies it.
type Users interface {
	ByID(ctx context.Context, id uuid.UUID) (users.User, error)
}

// Options are the service's collaborators. Proofs and Users may be empty:
// without a proof a day is only done by a tap, and without users the sweep
// only ends weeks.
type Options struct {
	Repository *Repository
	Goals      Goals
	Users      Users
	Proofs     map[string]Proof
	Funnel     *analytics.Funnel
	// Enabled is the FIRST_WEEK_ENABLED switch. Off, nothing new starts;
	// weeks already running carry on to their end.
	Enabled bool
	Log     *slog.Logger
}

type Service struct {
	repo    *Repository
	goals   Goals
	users   Users
	proofs  map[string]Proof
	funnel  *analytics.Funnel
	enabled bool
	log     *slog.Logger
	now     func() time.Time
}

func NewService(o Options) *Service {
	s := &Service{
		repo: o.Repository, goals: o.Goals, users: o.Users, proofs: o.Proofs,
		funnel: o.Funnel, enabled: o.Enabled, log: o.Log, now: time.Now,
	}
	if s.log == nil {
		s.log = slog.Default()
	}
	return s
}

// WithClock fixes now, for tests.
func (s *Service) WithClock(now func() time.Time) *Service { s.now = now; return s }

// Enabled reports whether new weeks may start.
func (s *Service) Enabled() bool { return s != nil && s.enabled }

// Start begins the person's first week with a template.
//
// goalID is the goal the week is for. Onboarding passes the one the person
// just named; uuid.Nil asks for one: the running week's goal if there is one,
// else a new goal from the template.
//
// A week that has only reached day 0 may be started again, which is how a
// later template pick replaces the custom week onboarding began. A week
// already under way may not: that is a conflict, not a restart.
func (s *Service) Start(ctx context.Context, user users.User, template string, goalID uuid.UUID) (Programme, error) {
	if !s.Enabled() {
		return Programme{}, apperr.Wrap(apperr.ErrNotFound, "the first week is not available")
	}
	t, ok := NormalizeTemplate(template)
	if !ok {
		return Programme{}, apperr.FieldErrors{}.Add("template", "Choose one of the first-week options.").OrNil()
	}

	now := s.now()
	existing, err := s.repo.Get(ctx, user.ID)
	running := err == nil && existing.Status == StatusActive
	if err != nil && !apperr.Is(err, apperr.ErrNotFound) {
		return Programme{}, err
	}
	if running && existing.Day(now) > 0 {
		return Programme{}, apperr.Wrap(apperr.ErrConflict, "your first week is already under way")
	}

	goal, err := s.goalFor(ctx, user, t, goalID, existing, running)
	if err != nil {
		return Programme{}, err
	}

	tz := user.Timezone
	if _, err := time.LoadLocation(tz); tz == "" || err != nil {
		tz = "UTC"
	}
	p := Programme{UserID: user.ID, GoalID: goal, Template: t, Timezone: tz}
	local := now.In(p.Location())
	p.StartedOn = time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)

	started, err := s.repo.Begin(ctx, p)
	if err != nil {
		return Programme{}, err
	}
	// A day-0 re-pick is the same week, not a second start.
	if !running {
		s.funnel.FirstWeekStarted(ctx, user.ID, t)
	}
	return started, nil
}

func (s *Service) goalFor(ctx context.Context, user users.User, template string, goalID uuid.UUID, existing Programme, running bool) (*uuid.UUID, error) {
	if goalID != uuid.Nil {
		return &goalID, nil
	}
	if running && existing.GoalID != nil {
		return existing.GoalID, nil
	}
	if s.goals == nil {
		return nil, nil
	}
	g, err := s.goals.Create(ctx, user.ID, goals.Input{
		Title:    i18n.T(ctx, "firstweek."+template+".goal"),
		Category: category(template),
	})
	if err != nil {
		return nil, err
	}
	return &g.ID, nil
}

// Today is the person's week as of now; false when they have none.
//
// Reading is also where logs are noticed: today's and yesterday's proofs are
// checked for days not yet done, so a sleep entry logged in the morning, or a
// workout logged after midnight, counts without any hook in those packages.
func (s *Service) Today(ctx context.Context, user users.User) (Today, bool, error) {
	p, err := s.repo.Get(ctx, user.ID)
	if apperr.Is(err, apperr.ErrNotFound) {
		return Today{}, false, nil
	}
	if err != nil {
		return Today{}, false, err
	}
	if err := s.notice(ctx, user, p); err != nil {
		// A proof that cannot be read leaves the day open; it is not a
		// reason to hide the week.
		s.log.Warn("first week proof failed", slog.String("user_id", user.ID.String()), slog.Any("error", err))
	}
	t, err := s.today(ctx, p)
	return t, err == nil, err
}

// notice records today and yesterday as done when their proof finds a log.
func (s *Service) notice(ctx context.Context, user users.User, p Programme) error {
	if p.Status != StatusActive {
		return nil
	}
	proof := s.proofs[p.Template]
	if proof == nil {
		return nil
	}
	done, err := s.repo.Completions(ctx, user.ID)
	if err != nil {
		return err
	}
	have := make(map[int]bool, len(done))
	for _, c := range done {
		have[c.Day] = true
	}
	day := p.Day(s.now())
	for _, d := range []int{day - 1, day} {
		if !InWeek(d) || have[d] {
			continue
		}
		logged, err := proof.LoggedOn(ctx, user, p.DayRange(d))
		if err != nil {
			return err
		}
		if logged {
			if _, err := s.complete(ctx, p, d, ViaLog); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) today(ctx context.Context, p Programme) (Today, error) {
	done, err := s.repo.Completions(ctx, p.UserID)
	if err != nil {
		return Today{}, err
	}
	byDay := make(map[int]Completion, len(done))
	for _, c := range done {
		byDay[c.Day] = c
	}
	t := Today{Programme: p, Day: p.Day(s.now()), Days: make([]DayState, 0, Days)}
	for d := 1; d <= Days; d++ {
		c, ok := byDay[d]
		t.Days = append(t.Days, DayState{Day: d, Date: p.DateOf(d), Done: ok, Via: c.Via})
	}
	if InWeek(t.Day) {
		_, t.Done = byDay[t.Day]
	}
	return t, nil
}

// MarkDone records today's action as done by the person's word. Idempotent:
// a second tap, or a tap after the log was already found, changes nothing.
func (s *Service) MarkDone(ctx context.Context, user users.User) (Today, error) {
	p, err := s.repo.Get(ctx, user.ID)
	if err != nil {
		return Today{}, err
	}
	if p.Status != StatusActive {
		return Today{}, apperr.Wrap(apperr.ErrConflict, "your first week is not running")
	}
	day := p.Day(s.now())
	if !InWeek(day) {
		return Today{}, apperr.Wrap(apperr.ErrConflict, "there is no first-week action today")
	}
	if _, err := s.complete(ctx, p, day, ViaTap); err != nil {
		return Today{}, err
	}
	return s.today(ctx, p)
}

// complete records a day and, the first time only, tells the funnel.
func (s *Service) complete(ctx context.Context, p Programme, day int, via string) (bool, error) {
	_, inserted, err := s.repo.Complete(ctx, p.UserID, day, p.DateOf(day), via)
	if err != nil {
		return false, err
	}
	if inserted {
		s.funnel.FirstWeekDayCompleted(ctx, p.UserID, day, via)
	}
	return inserted, nil
}

// Skip ends a running week at the person's request. Skipping one that is not
// running is a conflict, not a silent success: the client has a stale view.
func (s *Service) Skip(ctx context.Context, user users.User) (Today, error) {
	return s.end(ctx, user, StatusSkipped)
}

// Finish ends the week once its review is open. Before day 7 it is a
// conflict: the review is built from the whole week.
func (s *Service) Finish(ctx context.Context, user users.User) (Today, error) {
	p, err := s.repo.Get(ctx, user.ID)
	if err != nil {
		return Today{}, err
	}
	if p.Status == StatusActive && p.Day(s.now()) < Days {
		return Today{}, apperr.Wrap(apperr.ErrConflict, "the first week is not over yet")
	}
	t, err := s.end(ctx, user, StatusFinished)
	if err != nil {
		return Today{}, err
	}
	s.funnel.FirstWeekFinished(ctx, user.ID)
	return t, nil
}

func (s *Service) end(ctx context.Context, user users.User, status string) (Today, error) {
	p, changed, err := s.repo.End(ctx, user.ID, status)
	if err != nil {
		return Today{}, err
	}
	if !changed {
		if _, err := s.repo.Get(ctx, user.ID); err != nil {
			return Today{}, err
		}
		return Today{}, apperr.Wrap(apperr.ErrConflict, "your first week is not running")
	}
	return s.today(ctx, p)
}

// Sweep visits every running week once: it notices logged days and calls a
// week abandoned once it is three days past its end with no review opened.
// Returns how many weeks it ended.
func (s *Service) Sweep(ctx context.Context) (int, error) {
	const pageSize = 100
	ended := 0
	var after uuid.UUID
	for {
		if err := ctx.Err(); err != nil {
			return ended, err
		}
		page, err := s.repo.ListActive(ctx, after, pageSize)
		if err != nil {
			return ended, err
		}
		for _, p := range page {
			after = p.UserID
			if p.Day(s.now()) > Days+abandonAfter {
				if _, changed, err := s.repo.End(ctx, p.UserID, StatusAbandoned); err != nil {
					s.log.Error("first week abandon failed", slog.String("user_id", p.UserID.String()), slog.Any("error", err))
				} else if changed {
					ended++
				}
				continue
			}
			if s.users == nil {
				continue
			}
			user, err := s.users.ByID(ctx, p.UserID)
			if err != nil {
				s.log.Error("first week user load failed", slog.String("user_id", p.UserID.String()), slog.Any("error", err))
				continue
			}
			if err := s.notice(ctx, user, p); err != nil {
				s.log.Warn("first week proof failed", slog.String("user_id", p.UserID.String()), slog.Any("error", err))
			}
		}
		if len(page) < pageSize {
			return ended, nil
		}
	}
}
