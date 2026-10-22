package firstweek_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/posthog/posthog-go"

	"github.com/NorthAIProject/north-client/internal/analytics"
	"github.com/NorthAIProject/north-client/internal/firstweek"
	"github.com/NorthAIProject/north-client/internal/goals"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/users"
)

// events stands in for PostHog; only Enqueue is called.
type events struct {
	posthog.Client
	mu  sync.Mutex
	got []posthog.Capture
}

func (e *events) Enqueue(msg posthog.Message) error {
	if c, ok := msg.(posthog.Capture); ok {
		e.mu.Lock()
		e.got = append(e.got, c)
		e.mu.Unlock()
	}
	return nil
}

func (e *events) named(name string) []posthog.Capture {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []posthog.Capture
	for _, c := range e.got {
		if c.Event == name {
			out = append(out, c)
		}
	}
	return out
}

// clock is a settable now.
type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

// loggedDays is a proof that says yes for the listed local dates.
type loggedDays map[string]bool

func (l loggedDays) LoggedOn(_ context.Context, _ users.User, day timerange.Range) (bool, error) {
	return l[day.Since.Format(time.DateOnly)], nil
}

type fixture struct {
	pool   *pgxpool.Pool
	svc    *firstweek.Service
	user   users.User
	clock  *clock
	events *events
	proof  loggedDays
}

func setup(t *testing.T, enabled bool) fixture {
	t.Helper()
	pool := testdb.New(t)
	ctx := context.Background()
	userSvc := users.NewService(users.NewRepository(pool))
	user, err := userSvc.Register(ctx, users.Registration{
		Email:        "first-week-" + uuid.NewString()[:8] + "@north.test",
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Ana",
		Timezone:     "Europe/Lisbon",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	f := fixture{pool: pool, user: user, events: &events{}, proof: loggedDays{}}
	// Saturday evening before the clocks go back.
	f.clock = &clock{t: time.Date(2026, 10, 24, 19, 0, 0, 0, time.UTC)}
	f.svc = firstweek.NewService(firstweek.Options{
		Repository: firstweek.NewRepository(pool),
		Goals:      goals.NewService(goals.NewRepository(pool)),
		Users:      userSvc,
		Proofs:     map[string]firstweek.Proof{firstweek.TemplateSleep: f.proof},
		Funnel:     analytics.New(f.events),
		Enabled:    enabled,
	}).WithClock(f.clock.now)
	return f
}

// at moves the clock to a local time in Lisbon on programme day n (day 0 is
// 2026-10-24).
func (f fixture) at(day, hour int) {
	loc, _ := time.LoadLocation("Europe/Lisbon")
	f.clock.t = time.Date(2026, 10, 24+day, hour, 0, 0, 0, loc)
}

func TestStartingCreatesTheGoalAndBeginsOnDayZero(t *testing.T) {
	f := setup(t, true)
	ctx := context.Background()

	p, err := f.svc.Start(ctx, f.user, firstweek.TemplateSleep, uuid.Nil)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if p.Status != firstweek.StatusActive || p.Template != "sleep" || p.Timezone != "Europe/Lisbon" {
		t.Errorf("programme = %+v", p)
	}
	if got := p.StartedOn.Format(time.DateOnly); got != "2026-10-24" {
		t.Errorf("started on %s, want the local date 2026-10-24", got)
	}
	if p.GoalID == nil {
		t.Fatal("no goal was created for a week started without one")
	}
	g, err := goals.NewService(goals.NewRepository(f.pool)).Get(ctx, *p.GoalID, f.user.ID)
	if err != nil {
		t.Fatalf("load goal: %v", err)
	}
	if g.Title != "Sleep better this week" {
		t.Errorf("goal title = %q", g.Title)
	}

	today, ok, err := f.svc.Today(ctx, f.user)
	if err != nil || !ok {
		t.Fatalf("today: ok=%v err=%v", ok, err)
	}
	if today.Day != 0 || today.Done || len(today.Days) != firstweek.Days {
		t.Errorf("today = day %d done %v days %d", today.Day, today.Done, len(today.Days))
	}
	if n := len(f.events.named("first_week_started")); n != 1 {
		t.Errorf("first_week_started fired %d times, want 1", n)
	}
}

// Onboarding starts a custom week with the person's own goal; a template
// picked later the same day replaces it. Once day 1 has begun, the week is
// running and a second start is a conflict, not a silent reset.
func TestADayZeroWeekCanBeRepickedButARunningOneCannot(t *testing.T) {
	f := setup(t, true)
	ctx := context.Background()

	goal, err := goals.NewService(goals.NewRepository(f.pool)).Create(ctx, f.user.ID, goals.Input{Title: "Run a 10K", Category: "fitness"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.svc.Start(ctx, f.user, firstweek.TemplateCustom, goal.ID); err != nil {
		t.Fatalf("start custom: %v", err)
	}
	p, err := f.svc.Start(ctx, f.user, firstweek.TemplateTrain, uuid.Nil)
	if err != nil {
		t.Fatalf("re-pick: %v", err)
	}
	if p.Template != "train" || p.GoalID == nil || *p.GoalID != goal.ID {
		t.Errorf("re-pick = %s goal %v, want train keeping the person's goal", p.Template, p.GoalID)
	}
	if n := len(f.events.named("first_week_started")); n != 1 {
		t.Errorf("a day-0 re-pick fired first_week_started again (%d)", n)
	}

	f.at(1, 9)
	if _, err := f.svc.Start(ctx, f.user, firstweek.TemplateSleep, uuid.Nil); !apperr.Is(err, apperr.ErrConflict) {
		t.Errorf("start on day 1 = %v, want conflict", err)
	}
}

func TestNothingStartsWhileTheSwitchIsOff(t *testing.T) {
	f := setup(t, false)
	if _, err := f.svc.Start(context.Background(), f.user, firstweek.TemplateSleep, uuid.Nil); !apperr.Is(err, apperr.ErrNotFound) {
		t.Errorf("start with the switch off = %v, want not found", err)
	}
}

func TestAnUnknownTemplateIsAValidationError(t *testing.T) {
	f := setup(t, true)
	if _, err := f.svc.Start(context.Background(), f.user, "meditate", uuid.Nil); !apperr.Is(err, apperr.ErrValidation) {
		t.Errorf("unknown template = %v, want validation", err)
	}
}

// A tap and a found log can both claim the same day, from different
// channels, in either order. The day is done once and the funnel hears once.
func TestADayIsCompletedOnceHoweverItIsClaimed(t *testing.T) {
	f := setup(t, true)
	ctx := context.Background()
	if _, err := f.svc.Start(ctx, f.user, firstweek.TemplateSleep, uuid.Nil); err != nil {
		t.Fatal(err)
	}

	f.at(1, 8)
	f.proof["2026-10-25"] = true
	for range 2 {
		if _, _, err := f.svc.Today(ctx, f.user); err != nil {
			t.Fatalf("today: %v", err)
		}
	}
	today, err := f.svc.MarkDone(ctx, f.user)
	if err != nil {
		t.Fatalf("mark done: %v", err)
	}
	if !today.Done || today.Days[0].Via != firstweek.ViaLog {
		t.Errorf("day 1 = done %v via %q, want done via the log that came first", today.Done, today.Days[0].Via)
	}
	got := f.events.named("first_week_day_completed")
	if len(got) != 1 {
		t.Fatalf("first_week_day_completed fired %d times, want 1", len(got))
	}
	if got[0].Properties["day"] != 1 || got[0].Properties["via"] != "log" {
		t.Errorf("event properties = %v", got[0].Properties)
	}
}

// Missing a day leaves it open; it is not made up later, and the next day
// still counts. A log made after midnight still counts for the day before.
func TestSkippedDaysStayOpenAndLateLogsCountForYesterday(t *testing.T) {
	f := setup(t, true)
	ctx := context.Background()
	if _, err := f.svc.Start(ctx, f.user, firstweek.TemplateSleep, uuid.Nil); err != nil {
		t.Fatal(err)
	}

	f.at(1, 21)
	if _, err := f.svc.MarkDone(ctx, f.user); err != nil {
		t.Fatalf("day 1: %v", err)
	}
	// Day 2 passes with nothing. Day 3's log turns up just after midnight,
	// on day 4.
	f.proof["2026-10-27"] = true
	f.at(4, 0)
	today, _, err := f.svc.Today(ctx, f.user)
	if err != nil {
		t.Fatal(err)
	}
	if today.Day != 4 || today.Done {
		t.Errorf("today = day %d done %v, want day 4 not done", today.Day, today.Done)
	}
	done := map[int]bool{}
	for _, d := range today.Days {
		done[d.Day] = d.Done
	}
	if !done[1] || done[2] || !done[3] || today.DoneCount() != 2 {
		t.Errorf("days = %v (count %d), want 1 and 3 done, 2 open", done, today.DoneCount())
	}
}

func TestThereIsNoActionToMarkOutsideTheWeek(t *testing.T) {
	f := setup(t, true)
	ctx := context.Background()
	if _, err := f.svc.Start(ctx, f.user, firstweek.TemplateJournal, uuid.Nil); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.MarkDone(ctx, f.user); !apperr.Is(err, apperr.ErrConflict) {
		t.Errorf("mark done on day 0 = %v, want conflict", err)
	}
	f.at(8, 9)
	if _, err := f.svc.MarkDone(ctx, f.user); !apperr.Is(err, apperr.ErrConflict) {
		t.Errorf("mark done on day 8 = %v, want conflict", err)
	}
}

func TestTheWeekFinishesOnlyOnceItIsOver(t *testing.T) {
	f := setup(t, true)
	ctx := context.Background()
	if _, err := f.svc.Start(ctx, f.user, firstweek.TemplateTrain, uuid.Nil); err != nil {
		t.Fatal(err)
	}

	f.at(6, 20)
	if _, err := f.svc.Finish(ctx, f.user); !apperr.Is(err, apperr.ErrConflict) {
		t.Errorf("finish on day 6 = %v, want conflict", err)
	}
	f.at(7, 20)
	today, err := f.svc.Finish(ctx, f.user)
	if err != nil {
		t.Fatalf("finish on day 7: %v", err)
	}
	if today.Programme.Status != firstweek.StatusFinished {
		t.Errorf("status = %s", today.Programme.Status)
	}
	if _, err := f.svc.Finish(ctx, f.user); !apperr.Is(err, apperr.ErrConflict) {
		t.Errorf("second finish = %v, want conflict", err)
	}
	if n := len(f.events.named("first_week_finished")); n != 1 {
		t.Errorf("first_week_finished fired %d times, want 1", n)
	}
}

func TestSkippingEndsTheWeek(t *testing.T) {
	f := setup(t, true)
	ctx := context.Background()
	if _, err := f.svc.Start(ctx, f.user, firstweek.TemplateProtein, uuid.Nil); err != nil {
		t.Fatal(err)
	}
	f.at(2, 12)
	today, err := f.svc.Skip(ctx, f.user)
	if err != nil {
		t.Fatalf("skip: %v", err)
	}
	if today.Programme.Status != firstweek.StatusSkipped {
		t.Errorf("status = %s", today.Programme.Status)
	}
	if _, err := f.svc.MarkDone(ctx, f.user); !apperr.Is(err, apperr.ErrConflict) {
		t.Errorf("mark done after skip = %v, want conflict", err)
	}
}

// A week nobody closed stays active three days past its end, for a review
// opened late, and is then called abandoned. The sweep also notices logs for
// people who never open the app.
func TestTheSweepNoticesLogsAndAbandonsStaleWeeks(t *testing.T) {
	f := setup(t, true)
	ctx := context.Background()
	if _, err := f.svc.Start(ctx, f.user, firstweek.TemplateSleep, uuid.Nil); err != nil {
		t.Fatal(err)
	}

	f.proof["2026-10-26"] = true
	f.at(2, 10)
	if ended, err := f.svc.Sweep(ctx); err != nil || ended != 0 {
		t.Fatalf("sweep on day 2: ended %d err %v", ended, err)
	}
	if n := len(f.events.named("first_week_day_completed")); n != 1 {
		t.Errorf("the sweep recorded %d completions, want 1", n)
	}

	f.at(10, 23)
	if ended, _ := f.svc.Sweep(ctx); ended != 0 {
		t.Errorf("day 10 abandoned the week")
	}
	f.at(11, 1)
	if ended, err := f.svc.Sweep(ctx); err != nil || ended != 1 {
		t.Fatalf("sweep on day 11: ended %d err %v", ended, err)
	}
	today, _, err := f.svc.Today(ctx, f.user)
	if err != nil {
		t.Fatal(err)
	}
	if today.Programme.Status != firstweek.StatusAbandoned {
		t.Errorf("status = %s, want abandoned", today.Programme.Status)
	}
}

func TestSomebodyWithoutAWeekHasNone(t *testing.T) {
	f := setup(t, true)
	if _, ok, err := f.svc.Today(context.Background(), f.user); ok || err != nil {
		t.Errorf("today = ok %v err %v, want none", ok, err)
	}
	if _, err := f.svc.MarkDone(context.Background(), f.user); !apperr.Is(err, apperr.ErrNotFound) {
		t.Errorf("mark done = %v, want not found", err)
	}
}
