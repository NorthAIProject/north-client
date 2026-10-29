package firstweek

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/analytics"
	"github.com/NorthAIProject/north-client/internal/goals"
	"github.com/NorthAIProject/north-client/internal/moments"
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

// Answers records a Done tap on the day's coach message, so a tap on the
// card or under a notification banner counts as answering it.
// outreach.Service satisfies it; it is set after both exist (WithAnswers),
// since outreach reads the week too.
type Answers interface {
	RecordFirstWeekDone(ctx context.Context, user users.User) error
}

// Options are the service's collaborators. Proofs and Users may be empty:
// without a proof a day is only done by a tap, and without users the sweep
// only ends weeks.
type Options struct {
	Repository Repository
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
	repo    Repository
	goals   Goals
	users   Users
	proofs  map[string]Proof
	funnel  *analytics.Funnel
	enabled bool
	answers Answers
	log     *slog.Logger
	now     func() time.Time
}

// Repository is the persistence Service needs. Service names it rather than
// the Postgres type so the database stays a detail of the composition root:
// NewRepository is what the process wires, and a test can hand Service
// anything that keeps first-week programmes and their completions.
type Repository interface {
	Begin(ctx context.Context, p Programme) (Programme, error)
	Get(ctx context.Context, userID uuid.UUID) (Programme, error)
	End(ctx context.Context, userID uuid.UUID, status string) (Programme, bool, error)
	ListActive(ctx context.Context, after uuid.UUID, limit int) ([]Programme, error)
	Complete(ctx context.Context, userID uuid.UUID, day int, localDate time.Time, via string) (Completion, bool, error)
	Completions(ctx context.Context, userID uuid.UUID) ([]Completion, error)
	SetReview(ctx context.Context, userID uuid.UUID, review Review) (Programme, error)
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

// WithAnswers attaches the coach's morning message, for TapDone.
func (s *Service) WithAnswers(a Answers) *Service { s.answers = a; return s }

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
	if _, lerr := time.LoadLocation(tz); tz == "" || lerr != nil {
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
	if err = s.notice(ctx, user, p); err != nil {
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

// TapDone is MarkDone from the person's own tap, on the card or under a
// notification banner. The day's coach message is answered too, so the
// answered rate counts the tap wherever it happened. The coach message's
// own Done button goes through outreach, which records its answer itself
// and calls MarkDone.
func (s *Service) TapDone(ctx context.Context, user users.User) (Today, error) {
	t, err := s.MarkDone(ctx, user)
	if err != nil {
		return Today{}, err
	}
	if s.answers != nil {
		if err := s.answers.RecordFirstWeekDone(ctx, user); err != nil {
			// The day is done; a message left unanswered only skews a rate.
			s.log.Warn("first week: could not answer the coach message", slog.String("user_id", user.ID.String()), slog.Any("error", err))
		}
	}
	return t, nil
}

// complete records a day and, the first time only, tells the funnel. The
// completion that makes days 1 to 3 all done on day 3 is the one that earns
// the "3 of 3 days" moment, whether it came from a tap or a log, so the
// moment is counted here, once, and not where the card renders.
func (s *Service) complete(ctx context.Context, p Programme, day int, via string) (bool, error) {
	_, inserted, err := s.repo.Complete(ctx, p.UserID, day, p.DateOf(day), via)
	if err != nil {
		return false, err
	}
	if !inserted {
		return false, nil
	}
	s.funnel.FirstWeekDayCompleted(ctx, p.UserID, day, via)
	if day <= 3 && p.Day(s.now()) == 3 {
		t, err := s.today(ctx, p)
		if err != nil {
			return true, err
		}
		if t.ThreeOfThree() {
			s.funnel.MomentShown(ctx, p.UserID, moments.KindFirstWeek)
		}
	}
	return true, nil
}

// Skip ends a running week at the person's request. Skipping one that is not
// running is a conflict, not a silent success: the client has a stale view.
func (s *Service) Skip(ctx context.Context, user users.User) (Today, error) {
	return s.end(ctx, user, StatusSkipped)
}

// Finish ends the week once its review is open. Before day 7 it is a
// conflict: the review is built from the whole week. A week finished
// without its review having been written gets the catalogue's, so a
// finished week always has one.
func (s *Service) Finish(ctx context.Context, user users.User) (Today, error) {
	p, err := s.repo.Get(ctx, user.ID)
	if err != nil {
		return Today{}, err
	}
	if p.Status == StatusActive && p.Day(s.now()) < Days {
		return Today{}, apperr.Wrap(apperr.ErrConflict, "the first week is not over yet")
	}
	if p.Status == StatusActive {
		if _, err = s.ensureReview(ctx, user, p); err != nil {
			return Today{}, err
		}
	}
	t, err := s.end(ctx, user, StatusFinished)
	if err != nil {
		return Today{}, err
	}
	s.funnel.FirstWeekFinished(ctx, user.ID)
	return t, nil
}

// OpenReview is the review page: the week's review, written if it was not
// yet, and the week finished on the way. Opening it again after the week
// finished shows the same review and counts nothing twice. Before day 7 it
// is a conflict, and for a week that was skipped or abandoned there is no
// review to show.
func (s *Service) OpenReview(ctx context.Context, user users.User) (Today, Review, error) {
	p, err := s.repo.Get(ctx, user.ID)
	if err != nil {
		return Today{}, Review{}, err
	}
	switch {
	case p.Status == StatusActive && p.Day(s.now()) < Days:
		return Today{}, Review{}, apperr.Wrap(apperr.ErrConflict, "the first week is not over yet")
	case p.Status == StatusFinished && p.Review != nil:
		t, terr := s.today(ctx, p)
		return t, *p.Review, terr
	case p.Status != StatusActive && p.Status != StatusFinished:
		return Today{}, Review{}, apperr.Wrap(apperr.ErrNotFound, "this first week has no review")
	}
	review, err := s.ensureReview(ctx, user, p)
	if err != nil {
		return Today{}, Review{}, err
	}
	if p.Status != StatusActive {
		t, terr := s.today(ctx, p)
		return t, review, terr
	}
	t, err := s.Finish(ctx, user)
	if apperr.Is(err, apperr.ErrConflict) {
		// Finished by another tab or the app in the meantime.
		p, err = s.repo.Get(ctx, user.ID)
		if err != nil {
			return Today{}, Review{}, err
		}
		t, err = s.today(ctx, p)
	}
	return t, review, err
}

// Tally counts the week for its review, without the text: the days done
// and how, and how many days the template's log turned up.
func (s *Service) Tally(ctx context.Context, user users.User) (Review, error) {
	p, err := s.repo.Get(ctx, user.ID)
	if err != nil {
		return Review{}, err
	}
	return s.tally(ctx, user, p)
}

func (s *Service) tally(ctx context.Context, user users.User, p Programme) (Review, error) {
	done, err := s.repo.Completions(ctx, p.UserID)
	if err != nil {
		return Review{}, err
	}
	r := Review{DaysDone: len(done)}
	for _, c := range done {
		if c.Via == ViaTap {
			r.DaysByTap++
		} else {
			r.DaysByLog++
		}
	}
	proof := s.proofs[p.Template]
	if proof == nil {
		return r, nil
	}
	last := min(p.Day(s.now()), Days)
	logged := 0
	for d := 1; d <= last; d++ {
		ok, err := proof.LoggedOn(ctx, user, p.DayRange(d))
		if err != nil {
			// The count is colour for the review, not its reason.
			s.log.Warn("first week: review could not read logs", slog.String("user_id", user.ID.String()), slog.Any("error", err))
			return r, nil
		}
		if ok {
			logged++
		}
	}
	r.LoggedDays = &logged
	return r, nil
}

// SaveReview stores the review the coach wrote on day 7. If one is already
// there, it is kept and returned.
func (s *Service) SaveReview(ctx context.Context, userID uuid.UUID, r Review) (Review, error) {
	if r.CreatedAt.IsZero() {
		r.CreatedAt = s.now()
	}
	p, err := s.repo.SetReview(ctx, userID, r)
	if err != nil {
		return Review{}, err
	}
	if p.Review == nil {
		return r, nil
	}
	return *p.Review, nil
}

// ensureReview is the week's review, writing the catalogue's when the coach
// has not: the coach's morning message did not go out (quiet hours, check-ins
// off, the daily briefing) or the review was opened before it.
func (s *Service) ensureReview(ctx context.Context, user users.User, p Programme) (Review, error) {
	if p.Review != nil {
		return *p.Review, nil
	}
	r, err := s.tally(ctx, user, p)
	if err != nil {
		return Review{}, err
	}
	r.Text = ReviewFallback(i18n.WithLocale(ctx, string(user.Locale)), r.DaysDone)
	r.WrittenBy = ReviewByTemplate
	return s.SaveReview(ctx, user.ID, r)
}

// ReviewFallback is the catalogue's review: how many days, and an
// invitation to keep going. Also what outreach sends when the coach could
// not write the day-7 message.
func ReviewFallback(ctx context.Context, daysDone int) string {
	switch {
	case daysDone >= Days:
		return i18n.T(ctx, "firstweek.review.fallback.all")
	case daysDone == 0:
		return i18n.T(ctx, "firstweek.review.fallback.none")
	default:
		return i18n.Tf(ctx, "firstweek.review.fallback.some", daysDone)
	}
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
