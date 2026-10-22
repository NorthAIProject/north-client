package outreach_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai/prompts"
	"github.com/NorthAIProject/north-client/internal/firstweek"
	"github.com/NorthAIProject/north-client/internal/goals"
	"github.com/NorthAIProject/north-client/internal/notifications"
	"github.com/NorthAIProject/north-client/internal/nudges"
	"github.com/NorthAIProject/north-client/internal/outreach"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/users"
	"github.com/NorthAIProject/north-client/internal/workouts"
)

// The week runs Thursday 22 to Wednesday 28 October 2026 in Lisbon; day 0
// is the 21st. Europe leaves summer time early on Sunday the 25th, day 4.
var lisbon, _ = time.LoadLocation("Europe/Lisbon")

func weekDay(day, hour, minute int) time.Time {
	return time.Date(2026, 10, 21+day, hour, minute, 0, 0, lisbon)
}

// weekFixture is outreach and the nudge sweep over a real first week, with
// only the coach and the outside channels stubbed.
type weekFixture struct {
	ctx      context.Context
	clock    *time.Time
	user     users.User
	week     *firstweek.Service
	outreach *outreach.Service
	nudges   *nudges.Service
	prefs    *stubPrefs
	lighter  *stubLighter
	training *stubTraining
	writer   *stubWriter
	chat     *stubChat
}

func newWeekFixture(t *testing.T) *weekFixture {
	t.Helper()
	pool := testdb.New(t)
	ctx := context.Background()
	userSvc := users.NewService(users.NewRepository(pool))
	user, err := userSvc.Register(ctx, users.Registration{
		Email: "week-" + uuid.NewString()[:8] + "@north.test", PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName: "Ana", Timezone: "Europe/Lisbon",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if user, err = userSvc.MarkOnboarded(ctx, user.ID); err != nil {
		t.Fatalf("onboard: %v", err)
	}

	clock := weekDay(0, 18, 0)
	f := &weekFixture{
		ctx: ctx, clock: &clock, user: user,
		prefs: &stubPrefs{p: notifications.Defaults()}, lighter: &stubLighter{}, training: &stubTraining{},
		writer: &stubWriter{}, chat: &stubChat{},
	}
	now := func() time.Time { return *f.clock }
	goalSvc := goals.NewService(goals.NewRepository(pool))
	f.week = firstweek.NewService(firstweek.Options{
		Repository: firstweek.NewRepository(pool), Goals: goalSvc, Users: userSvc, Enabled: true,
	}).WithClock(now)
	f.nudges = nudges.NewService(nudges.NewRepository(pool), userSvc, nil, goalSvc).
		WithPrefs(f.prefs).
		WithFirstWeek(nudges.FirstWeekFunc(func(ctx context.Context, u users.User) (nudges.FirstWeekDay, bool, error) {
			wk, ok, err := f.week.Today(ctx, u)
			if err != nil || !ok || wk.Programme.Status != firstweek.StatusActive || !firstweek.InWeek(wk.Day) {
				return nudges.FirstWeekDay{}, false, err
			}
			return nudges.FirstWeekDay{Day: wk.Day, Template: wk.Programme.Template, Done: wk.Done}, true, nil
		})).
		WithClock(now)
	f.outreach = outreach.NewService(outreach.Options{
		Repository: outreach.NewRepository(pool), Prefs: f.prefs, Lighter: f.lighter, Training: f.training,
		FirstWeek: f.week, Writer: f.writer, Chats: &stubChats{}, Notifier: f.nudges, Chat: f.chat,
	}).WithClock(now)

	if _, err := f.week.Start(ctx, user, firstweek.TemplateSleep, uuid.Nil); err != nil {
		t.Fatalf("start: %v", err)
	}
	return f
}

func (f *weekFixture) at(t time.Time) { *f.clock = t }

func TestTheFirstWeekOwnsTheMorningMessage(t *testing.T) {
	f := newWeekFixture(t)
	// A low morning and a missed session: both would speak on their own.
	f.lighter.today = lowMorning()
	f.training.missed, f.training.ok = workouts.Missed{Title: "Upper B", Weekday: "Wednesday"}, true

	f.at(weekDay(2, 8, 5))
	sent, err := f.outreach.Evaluate(f.ctx, f.user)
	if err != nil || !sent {
		t.Fatalf("sent %v err %v, want the first-week message", sent, err)
	}
	if len(f.lighter.chosen) != 0 {
		t.Errorf("the coach changed training during the first week: %v", f.lighter.chosen)
	}
	o, ok, _ := f.outreach.Today(f.ctx, f.user)
	if !ok || o.Trigger != outreach.TriggerFirstWeek || o.Facts.FirstWeekDay != 2 || o.Facts.Template != firstweek.TemplateSleep {
		t.Fatalf("today = %+v", o)
	}
	s := f.writer.situations[0]
	for _, want := range []string{"day 2 of 7", "Sleep better", "Wind down 30 minutes", "Lower A", "Upper B", "not logged"} {
		if !strings.Contains(s, want) {
			t.Errorf("situation is missing %q: %s", want, s)
		}
	}
	if got := o.Actions(); len(got) != 1 || got[0] != outreach.ActionDone {
		t.Errorf("actions = %v, want done", got)
	}
	if nudges.PushCategory(nudges.KindCoachFirstWeek) != nudges.CategoryCoachFirstWeek {
		t.Error("coach_first_week must carry the COACH_FIRST_WEEK buttons")
	}

	// Done from the Telegram button records the day.
	reply, handled := f.outreach.AnswerFromChat(f.ctx, f.user, f.chat.sent[0].Options[0].Value)
	if !handled || reply == "" {
		t.Fatalf("chat answer handled %v reply %q", handled, reply)
	}
	wk, _, _ := f.week.Today(f.ctx, f.user)
	if !wk.Done || wk.Days[1].Via != firstweek.ViaTap {
		t.Errorf("day 2 after Done = %+v", wk.Days[1])
	}
}

func TestTheFirstWeekMorningWindow(t *testing.T) {
	cases := []struct {
		name string
		at   time.Time
		sent bool
	}{
		{"07:59 is too early", weekDay(1, 7, 59), false},
		{"10:59 still catches up", weekDay(1, 10, 59), true},
		{"11:00 is too late", weekDay(1, 11, 0), false},
		{"day 7 belongs to the review", weekDay(7, 8, 30), false},
		{"day 0 has no action yet", weekDay(0, 9, 0), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newWeekFixture(t)
			f.at(c.at)
			sent, err := f.outreach.Evaluate(f.ctx, f.user)
			if err != nil {
				t.Fatal(err)
			}
			if sent != c.sent {
				t.Errorf("sent = %v, want %v", sent, c.sent)
			}
		})
	}
}

func TestTheDailyBriefingIsTheMorningMessage(t *testing.T) {
	f := newWeekFixture(t)
	f.prefs.p.DailyBriefingAuto = true
	f.at(weekDay(3, 8, 0))
	if sent, err := f.outreach.Evaluate(f.ctx, f.user); err != nil || sent {
		t.Errorf("sent %v err %v, want nothing on a briefing morning", sent, err)
	}
}

// Seven days of hourly sweeps across the clock change: one morning message
// on days 1 to 6, an evening note only on days left undone, never more than
// two a day, and none of the old first-week notes.
func TestACompressedFirstWeek(t *testing.T) {
	f := newWeekFixture(t)
	doneOn := map[int]bool{2: true, 5: true}

	type day struct{ morning, evening, other int }
	got := map[int]*day{}
	seen := map[uuid.UUID]bool{}

	start := weekDay(1, 0, 0)
	end := weekDay(8, 0, 0)
	for tick := start; tick.Before(end); tick = tick.Add(time.Hour) {
		f.at(tick)
		local := tick.In(lisbon)
		n := local.Day() - 21
		if doneOn[n] && local.Hour() == 12 {
			if _, err := f.week.MarkDone(f.ctx, f.user); err != nil {
				t.Fatalf("day %d done: %v", n, err)
			}
		}
		if _, err := f.outreach.Evaluate(f.ctx, f.user); err != nil {
			t.Fatalf("%s outreach: %v", local, err)
		}
		if _, err := f.nudges.Evaluate(f.ctx, f.user); err != nil {
			t.Fatalf("%s nudges: %v", local, err)
		}

		open, err := f.nudges.ListOpen(f.ctx, f.user.ID, 100)
		if err != nil {
			t.Fatal(err)
		}
		for _, nd := range open {
			if seen[nd.ID] {
				continue
			}
			seen[nd.ID] = true
			if got[n] == nil {
				got[n] = &day{}
			}
			switch nd.Kind {
			case nudges.KindCoachFirstWeek:
				got[n].morning++
				if h := local.Hour(); h < 8 || h >= 11 {
					t.Errorf("day %d morning message at %s", n, local.Format("15:04"))
				}
			case nudges.KindFirstWeekEvening:
				got[n].evening++
			default:
				got[n].other++
				t.Errorf("day %d: unexpected %s note", n, nd.Kind)
			}
		}
	}

	for n := 1; n <= 7; n++ {
		d := got[n]
		if d == nil {
			d = &day{}
		}
		wantMorning := 0
		if n <= 6 {
			wantMorning = 1
		}
		wantEvening := 1
		if doneOn[n] {
			wantEvening = 0
		}
		if d.morning != wantMorning || d.evening != wantEvening {
			t.Errorf("day %d: morning %d evening %d, want %d and %d", n, d.morning, d.evening, wantMorning, wantEvening)
		}
		if total := d.morning + d.evening + d.other; total > 2 {
			t.Errorf("day %d: %d messages", n, total)
		}
	}
}

// Offline check of what the coach is asked to write, per template: the
// rendered outreach prompt names the day and exactly one action, and the
// situation gives the model nothing medical to repeat. The live eval tier
// does not reach the outreach prompt, so this is the guard.
func TestTheFirstWeekPromptPerTemplate(t *testing.T) {
	for _, tmpl := range firstweek.Templates {
		t.Run(tmpl, func(t *testing.T) {
			f := newWeekFixture(t)
			if _, err := f.week.Start(f.ctx, f.user, tmpl, uuid.Nil); err != nil {
				t.Fatalf("re-pick on day 0: %v", err)
			}
			f.at(weekDay(4, 9, 0))
			if sent, err := f.outreach.Evaluate(f.ctx, f.user); err != nil || !sent {
				t.Fatalf("sent %v err %v", sent, err)
			}
			situation := f.writer.situations[0]
			ask, err := prompts.Render(prompts.CoachOutreach, map[string]string{"Situation": situation})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(ask, "day 4 of 7") || strings.Count(ask, "Today's one action:") != 1 {
				t.Errorf("prompt does not name the day and one action:\n%s", ask)
			}
			if !strings.Contains(ask, "At most two short sentences") {
				t.Error("the outreach rules are missing from the prompt")
			}
			lower := strings.ToLower(situation)
			for _, word := range []string{"diagnos", "medic", "supplement", "doctor", "calorie deficit", "disease"} {
				if strings.Contains(lower, word) {
					t.Errorf("situation mentions %q: %s", word, situation)
				}
			}
		})
	}
}
