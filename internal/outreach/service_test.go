package outreach_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/conversations"
	"github.com/NorthAIProject/north-client/internal/insights"
	"github.com/NorthAIProject/north-client/internal/insights/score"
	"github.com/NorthAIProject/north-client/internal/lighterday"
	"github.com/NorthAIProject/north-client/internal/messaging"
	"github.com/NorthAIProject/north-client/internal/notifications"
	"github.com/NorthAIProject/north-client/internal/nudges"
	"github.com/NorthAIProject/north-client/internal/outreach"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
	"github.com/NorthAIProject/north-client/internal/workouts"
)

// --- stubs: everything but the repository, which is real ---

type stubPrefs struct{ p notifications.Prefs }

func (s *stubPrefs) Get(context.Context, uuid.UUID) (notifications.Prefs, error) { return s.p, nil }

type stubLighter struct {
	mu     sync.Mutex
	today  lighterday.Today
	chosen []string // "choice/source"
}

func (s *stubLighter) Today(context.Context, users.User) (lighterday.Today, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.today, nil
}

func (s *stubLighter) ChooseAs(_ context.Context, _ users.User, choice, source string) (lighterday.Today, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.chosen = append(s.chosen, choice+"/"+source)
	s.today.Choice, s.today.Source = choice, source
	return s.today, nil
}

type stubTraining struct {
	missed workouts.Missed
	ok     bool
	moved  []string
}

func (s *stubTraining) MissedYesterday(context.Context, users.User, time.Time) (workouts.Missed, bool, error) {
	return s.missed, s.ok, nil
}

func (s *stubTraining) MoveMissed(_ context.Context, _ users.User, _ time.Time, to string) (workouts.WeekProgress, error) {
	s.moved = append(s.moved, to)
	return workouts.WeekProgress{}, nil
}

type stubWriter struct {
	err        error
	situations []string
}

func (s *stubWriter) RunOutreach(_ context.Context, _ users.User, situation string) (conversations.Message, error) {
	s.situations = append(s.situations, situation)
	if s.err != nil {
		return conversations.Message{}, s.err
	}
	// No id: message_id references a real chat message, which a stub has not
	// stored.
	return conversations.Message{Content: "Coach says: " + situation[:20]}, nil
}

type stubChats struct{ posted []string }

func (s *stubChats) PostProactive(_ context.Context, _, _ uuid.UUID, text, _ string) (conversations.Message, error) {
	s.posted = append(s.posted, text)
	return conversations.Message{Content: text}, nil
}

type stubNotifier struct{ drafts []nudges.Draft }

func (s *stubNotifier) Raise(_ context.Context, _ users.User, d nudges.Draft) (nudges.Nudge, bool, error) {
	s.drafts = append(s.drafts, d)
	return nudges.Nudge{}, true, nil
}

type stubChat struct {
	sent     []messaging.OutboundMessage
	resolved []string
}

func (s *stubChat) NotifyTaggedMessage(_ context.Context, _ uuid.UUID, _, _ string, msg messaging.OutboundMessage) error {
	s.sent = append(s.sent, msg)
	return nil
}

func (s *stubChat) ResolveTagged(_ context.Context, _ uuid.UUID, _, _, text string) error {
	s.resolved = append(s.resolved, text)
	return nil
}

// --- fixture ---

type fixture struct {
	svc      *outreach.Service
	user     users.User
	prefs    *stubPrefs
	lighter  *stubLighter
	training *stubTraining
	writer   *stubWriter
	chats    *stubChats
	notifier *stubNotifier
	chat     *stubChat
	clock    *time.Time
	ctx      context.Context
}

// 08:30 in Lisbon on Friday 16 October 2026.
var morning = time.Date(2026, 10, 16, 7, 30, 0, 0, time.UTC)

func newFixture(t *testing.T) *fixture {
	t.Helper()
	pool := testdb.New(t)
	user, err := users.NewService(users.NewRepository(pool)).Register(context.Background(), users.Registration{
		Email: "outreach@north.test", PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName: "Fernando", Timezone: "Europe/Lisbon",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	f := &fixture{
		user: user, ctx: context.Background(),
		prefs:    &stubPrefs{p: notifications.Defaults()},
		lighter:  &stubLighter{},
		training: &stubTraining{},
		writer:   &stubWriter{},
		chats:    &stubChats{},
		notifier: &stubNotifier{},
		chat:     &stubChat{},
	}
	clock := morning
	f.clock = &clock
	f.svc = outreach.NewService(outreach.Options{
		Repository: outreach.NewRepository(pool), Prefs: f.prefs, Lighter: f.lighter, Training: f.training,
		Writer: f.writer, Chats: f.chats, Notifier: f.notifier, Chat: f.chat,
	}).WithClock(func() time.Time { return *f.clock })
	return f
}

func lowMorning() lighterday.Today {
	return lighterday.Today{
		Recovery: insights.RecoveryData{Score: score.Score{HasData: true, Points: 40}},
		Session:  "Lower A", Due: true,
	}
}

// --- tests ---

func TestLowRecoveryLightensAndOffersUndo(t *testing.T) {
	f := newFixture(t)
	f.lighter.today = lowMorning()

	sent, err := f.svc.Evaluate(f.ctx, f.user)
	if err != nil || !sent {
		t.Fatalf("sent %v err %v, want a message", sent, err)
	}
	if len(f.lighter.chosen) != 1 || f.lighter.chosen[0] != "lighter/coach" {
		t.Errorf("chosen = %v, want the coach to lighten the day", f.lighter.chosen)
	}
	if s := f.writer.situations[0]; !strings.Contains(s, "already cut today's sets") || !strings.Contains(s, "Lower A") {
		t.Errorf("situation = %q", s)
	}
	o, ok, _ := f.svc.Today(f.ctx, f.user)
	if !ok || o.Trigger != outreach.TriggerLowRecovery || !o.Facts.Applied || o.Body == "" {
		t.Fatalf("today = %+v", o)
	}
	if got := o.Actions(); len(got) != 1 || got[0] != outreach.ActionUndo {
		t.Errorf("actions = %v, want undo", got)
	}

	// Push carries the coach's buttons; Telegram gets its own copy with them.
	d := f.notifier.drafts[0]
	if d.Kind != nudges.KindCoachLighter || !d.Push || d.Everywhere {
		t.Errorf("draft = %+v, want push only, as coach_lighter", d)
	}
	if nudges.PushCategory(d.Kind) != nudges.CategoryCoachLighter {
		t.Error("coach_lighter must carry the COACH_LIGHTER buttons")
	}
	if opts := f.chat.sent[0].Options; len(opts) != 1 || opts[0].Value != outreach.ChatPrefix+o.ID.String()+":undo" {
		t.Errorf("telegram options = %+v", opts)
	}

	// Undo, from the chat button.
	reply, handled := f.svc.AnswerFromChat(f.ctx, f.user, f.chat.sent[0].Options[0].Value)
	if !handled || reply == "" {
		t.Fatalf("chat answer handled %v reply %q", handled, reply)
	}
	if last := f.lighter.chosen[len(f.lighter.chosen)-1]; last != "keep/person" {
		t.Errorf("undo chose %q, want keep by the person", last)
	}
	if len(f.chat.resolved) != 1 {
		t.Error("the Telegram message should be edited with the outcome")
	}
	answered, _, _ := f.svc.Today(f.ctx, f.user)
	if answered.Action != outreach.ActionUndo || answered.ActedAt == nil {
		t.Errorf("answer not recorded: %+v", answered)
	}
	// A second tap changes nothing.
	if _, err := f.svc.ActToday(f.ctx, f.user, outreach.ActionUndo); !apperr.Is(err, apperr.ErrConflict) {
		t.Errorf("second answer err = %v, want conflict", err)
	}
}

func TestOneMessageADayAndOnlyInItsHours(t *testing.T) {
	f := newFixture(t)
	f.lighter.today = lowMorning()

	// 06:30 local: too early.
	early := time.Date(2026, 10, 16, 5, 30, 0, 0, time.UTC)
	*f.clock = early
	if sent, _ := f.svc.Evaluate(f.ctx, f.user); sent {
		t.Error("spoke before 07:00")
	}
	*f.clock = morning
	if sent, _ := f.svc.Evaluate(f.ctx, f.user); !sent {
		t.Fatal("did not speak at 08:30")
	}
	// A missed session the same day does not get a second message.
	f.training.missed, f.training.ok = workouts.Missed{Weekday: "Thursday", Title: "Upper B", TodayFree: true}, true
	f.lighter.today = lighterday.Today{}
	*f.clock = morning.Add(3 * time.Hour)
	if sent, _ := f.svc.Evaluate(f.ctx, f.user); sent {
		t.Error("a second message on the same day")
	}
	if len(f.writer.situations) != 1 {
		t.Errorf("the coach wrote %d messages today, want 1", len(f.writer.situations))
	}
}

func TestPrefsAndQuietHoursHoldTheCoachBack(t *testing.T) {
	f := newFixture(t)
	f.lighter.today = lowMorning()

	f.prefs.p.CoachCheckIns = false
	if sent, _ := f.svc.Evaluate(f.ctx, f.user); sent {
		t.Error("spoke with coach check-ins off")
	}
	f.prefs.p.CoachCheckIns = true
	f.prefs.p.QuietHoursEnabled, f.prefs.p.QuietStart, f.prefs.p.QuietEnd = true, "08:00", "10:00"
	if sent, _ := f.svc.Evaluate(f.ctx, f.user); sent {
		t.Error("spoke in quiet hours")
	}
	f.prefs.p.QuietHoursEnabled = false

	// Adjusting off: it only offers, and changes nothing.
	f.prefs.p.CoachAdjustsTraining = false
	if sent, _ := f.svc.Evaluate(f.ctx, f.user); !sent {
		t.Fatal("no offer")
	}
	if len(f.lighter.chosen) != 0 {
		t.Errorf("changed the session with adjusting off: %v", f.lighter.chosen)
	}
	o, _, _ := f.svc.Today(f.ctx, f.user)
	if got := o.Actions(); len(got) != 2 || got[0] != outreach.ActionLighter || got[1] != outreach.ActionKeep {
		t.Errorf("actions = %v, want lighter/keep", got)
	}
	if _, err := f.svc.ActToday(f.ctx, f.user, outreach.ActionLighter); err != nil {
		t.Fatalf("take it lighter: %v", err)
	}
	if f.lighter.chosen[0] != "lighter/person" {
		t.Errorf("chosen = %v", f.lighter.chosen)
	}
}

func TestMissedSessionOffersFreeDaysAndMovesIt(t *testing.T) {
	f := newFixture(t)
	f.training.missed = workouts.Missed{Weekday: "Thursday", Title: "Upper B", TodayFree: true, TomorrowFree: false}
	f.training.ok = true
	*f.clock = time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC) // 10:00 local

	if sent, err := f.svc.Evaluate(f.ctx, f.user); err != nil || !sent {
		t.Fatalf("sent %v err %v", sent, err)
	}
	o, _, _ := f.svc.Today(f.ctx, f.user)
	if got := o.Actions(); len(got) != 2 || got[0] != outreach.ActionToday || got[1] != outreach.ActionLetGo {
		t.Errorf("actions = %v, want today/let_go (tomorrow is not free)", got)
	}
	if f.notifier.drafts[0].Kind != nudges.KindCoachMissed {
		t.Errorf("kind = %q", f.notifier.drafts[0].Kind)
	}
	if _, err := f.svc.ActToday(f.ctx, f.user, outreach.ActionTomorrow); err == nil {
		t.Error("tomorrow was not offered and should be refused")
	}
	if _, err := f.svc.Act(f.ctx, f.user, o.ID, outreach.ActionToday); err != nil {
		t.Fatalf("move: %v", err)
	}
	if len(f.training.moved) != 1 || f.training.moved[0] != workouts.MoveToday {
		t.Errorf("moved = %v", f.training.moved)
	}
}

func TestFallbackWhenTheCoachCannotWrite(t *testing.T) {
	f := newFixture(t)
	f.lighter.today = lowMorning()
	f.writer.err = errors.New("every provider refused")

	if sent, err := f.svc.Evaluate(f.ctx, f.user); err != nil || !sent {
		t.Fatalf("sent %v err %v: the action must still land", sent, err)
	}
	if len(f.chats.posted) != 1 || !strings.Contains(f.chats.posted[0], "Lower A") {
		t.Errorf("fallback posted = %v", f.chats.posted)
	}
	o, _, _ := f.svc.Today(f.ctx, f.user)
	if o.Body != f.chats.posted[0] || len(f.notifier.drafts) != 1 {
		t.Errorf("fallback not delivered: body %q drafts %d", o.Body, len(f.notifier.drafts))
	}
}

func TestOtherChatTextIsNotAnAnswer(t *testing.T) {
	f := newFixture(t)
	if _, handled := f.svc.AnswerFromChat(f.ctx, f.user, "how was my week?"); handled {
		t.Error("ordinary text was taken as an outreach answer")
	}
	if reply, handled := f.svc.AnswerFromChat(f.ctx, f.user, "outreach:not-a-uuid:undo"); !handled || reply == "" {
		t.Error("a malformed button should be answered, not sent to the coach")
	}
}
