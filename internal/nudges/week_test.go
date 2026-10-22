package nudges_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/notifications"
	"github.com/NorthAIProject/north-client/internal/nudges"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/users"
)

type stubWeek struct {
	count  int
	photo  bool
	focus  bool
	notify []string
}

func (s *stubWeek) UserMessageCount(context.Context, uuid.UUID) (int, error) { return s.count, nil }
func (s *stubWeek) HasEvidence(context.Context, uuid.UUID) (bool, error)     { return s.photo, nil }
func (s *stubWeek) LastEvidenceAt(context.Context, uuid.UUID) (time.Time, bool, error) {
	return time.Time{}, false, nil
}

func (s *stubWeek) HasLifeFocus(context.Context, uuid.UUID, ...string) (bool, error) {
	return s.focus, nil
}

func (s *stubWeek) Notify(_ context.Context, _ uuid.UUID, text string) error {
	s.notify = append(s.notify, text)
	return nil
}

func TestFirstWeekCheckOnDayOneWhenTheyWentQuiet(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	user := mustOnboard(t, pool, seedUser(t, pool, "week-day1@north.test"), time.Date(2026, 8, 15, 9, 0, 0, 0, time.UTC))

	week := &stubWeek{count: 1, focus: true}
	svc := evalService(pool, now).WithWeek(week).WithFanout(week)

	n, err := svc.Evaluate(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("created = %d, want 1", n)
	}
	list, err := svc.ListOpen(ctx, user.ID, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Kind != nudges.KindFirstWeekCheck {
		t.Fatalf("open = %#v", list)
	}
	if len(week.notify) != 1 {
		t.Fatalf("telegram fan-out = %d, want 1", len(week.notify))
	}
}

func TestFirstWeekCheckSkipsIfTheyAlreadyTalked(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	user := mustOnboard(t, pool, seedUser(t, pool, "week-talked@north.test"), time.Date(2026, 8, 15, 9, 0, 0, 0, time.UTC))

	n, err := evalService(pool, now).WithWeek(&stubWeek{count: 3}).Evaluate(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("created = %d, want 0", n)
	}
}

func TestFirstWeekEvidenceOnDayThreeWithoutAPhoto(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	user := mustOnboard(t, pool, seedUser(t, pool, "week-photo@north.test"), time.Date(2026, 8, 15, 9, 0, 0, 0, time.UTC))

	n, err := evalService(pool, now).WithWeek(&stubWeek{count: 4, photo: false, focus: true}).Evaluate(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("created = %d, want 1", n)
	}
	list, err := evalService(pool, now).ListOpen(ctx, user.ID, 20)
	if err != nil {
		t.Fatal(err)
	}
	if list[0].Kind != nudges.KindFirstWeekEvidence {
		t.Fatalf("kind = %q", list[0].Kind)
	}
}

func TestFirstWeekEvidenceSkipsIfTheySentAPhoto(t *testing.T) {
	pool := testdb.New(t)
	now := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	user := mustOnboard(t, pool, seedUser(t, pool, "week-has-photo@north.test"), time.Date(2026, 8, 15, 9, 0, 0, 0, time.UTC))

	n, err := evalService(pool, now).WithWeek(&stubWeek{count: 4, photo: true, focus: true}).Evaluate(context.Background(), user)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("created = %d, want 0", n)
	}
}

type stubSchedule struct {
	enabled  bool
	every    int
	reminder int
}

func (s stubSchedule) PhotoSchedule(context.Context, uuid.UUID) (notifications.Schedule, error) {
	return notifications.Schedule{
		Kind:         notifications.KindPhoto,
		Enabled:      s.enabled,
		EveryDays:    s.every,
		ReminderDays: s.reminder,
	}, nil
}

func TestPhotoAskFiresWhenTheCadenceElapses(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	user := mustOnboard(t, pool, seedUser(t, pool, "photo-ask@north.test"), time.Date(2026, 8, 15, 9, 0, 0, 0, time.UTC))
	writeCheckIn(t, pool, user.ID, time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC))

	svc := evalService(pool, now).
		WithWeek(&stubWeek{count: 4}).
		WithSchedules(stubSchedule{enabled: true, every: 14, reminder: 2})

	n, err := svc.Evaluate(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("created = %d, want 1", n)
	}
	list, err := svc.ListOpen(ctx, user.ID, 20)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range list {
		if item.Kind == nudges.KindPhotoAsk {
			found = true
		}
	}
	if !found {
		t.Fatalf("no photo ask in %#v", list)
	}
}

func TestPhotoReminderFiresAfterTheGrace(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	user := mustOnboard(t, pool, seedUser(t, pool, "photo-remind@north.test"), time.Date(2026, 8, 15, 9, 0, 0, 0, time.UTC))
	writeCheckIn(t, pool, user.ID, time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC))

	svc := evalService(pool, now).
		WithWeek(&stubWeek{count: 4}).
		WithSchedules(stubSchedule{enabled: true, every: 14, reminder: 2})

	n, err := svc.Evaluate(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("created = %d, want 2 (ask + reminder)", n)
	}
}

func TestPhotoAskStaysOffWhenDisabled(t *testing.T) {
	pool := testdb.New(t)
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	user := mustOnboard(t, pool, seedUser(t, pool, "photo-off@north.test"), time.Date(2026, 8, 15, 9, 0, 0, 0, time.UTC))
	writeCheckIn(t, pool, user.ID, time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC))

	n, err := evalService(pool, now).
		WithWeek(&stubWeek{count: 4}).
		WithSchedules(stubSchedule{enabled: false, every: 14}).
		Evaluate(context.Background(), user)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("created = %d, want 0", n)
	}
}

func TestCoachReplyDoesNotFanOutToTelegram(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "reply-bell@north.test")
	week := &stubWeek{}
	svc := nudges.NewService(nudges.NewRepository(pool), users.NewService(users.NewRepository(pool)), nil, nil).
		WithFanout(week)

	_, created, err := svc.Raise(ctx, user, nudges.Draft{
		Kind:      nudges.KindCoachReply,
		DedupeKey: "conv-1",
		Title:     "See what I said",
		Body:      "Knees look fine.",
		Href:      "/app/chat/x",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("expected a bell row")
	}
	if len(week.notify) != 0 {
		t.Fatalf("coach_reply must not echo back to Telegram, got %v", week.notify)
	}
}

// runningWeek stands in for internal/firstweek: a programme on day, with
// today's action done or not.
func runningWeek(day int, done bool) nudges.FirstWeekFunc {
	return func(context.Context, users.User) (nudges.FirstWeekDay, bool, error) {
		return nudges.FirstWeekDay{Day: day, Template: "sleep", Done: done}, true, nil
	}
}

// A running programme replaces the fixed notes: the day-1 check that would
// fire for this person stays quiet.
func TestARunningFirstWeekReplacesTheFixedNotes(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	user := mustOnboard(t, pool, seedUser(t, pool, "week-programme@north.test"), time.Date(2026, 8, 15, 9, 0, 0, 0, time.UTC))

	n, err := evalService(pool, now).WithWeek(&stubWeek{count: 1, focus: true}).
		WithFirstWeek(runningWeek(1, false)).Evaluate(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("created = %d, want the day-1 check held back by the programme", n)
	}
}

func TestTheFirstWeekEveningNoteAsksOnlyForAnOpenDay(t *testing.T) {
	// seedUser lives in UTC; the default evening hour is 21.
	evening := time.Date(2026, 8, 16, 21, 10, 0, 0, time.UTC)
	cases := []struct {
		name string
		now  time.Time
		done bool
		want int
	}{
		{"open at the evening hour", evening, false, 1},
		{"already done", evening, true, 0},
		{"before the evening hour", evening.Add(-2 * time.Hour), false, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pool := testdb.New(t)
			ctx := context.Background()
			user := mustOnboard(t, pool, seedUser(t, pool, "week-evening@north.test"), time.Date(2026, 8, 15, 9, 0, 0, 0, time.UTC))
			svc := evalService(pool, c.now).WithFirstWeek(runningWeek(1, c.done))

			n, err := svc.Evaluate(ctx, user)
			if err != nil {
				t.Fatal(err)
			}
			if n != c.want {
				t.Fatalf("created = %d, want %d", n, c.want)
			}
			if c.want == 0 {
				return
			}
			list, _ := svc.ListOpen(ctx, user.ID, 20)
			if len(list) != 1 || list[0].Kind != nudges.KindFirstWeekEvening || list[0].Title != "Day 1 of 7: still open" {
				t.Fatalf("open = %#v", list)
			}
			if nudges.PushCategory(list[0].Kind) != nudges.CategoryCoachFirstWeek {
				t.Error("the evening note must carry the Done button")
			}
			if prefs := notifications.Defaults(); !prefs.AllowsNudge(nudges.KindFirstWeekEvening) {
				t.Error("the evening note must be on by default")
			}
		})
	}
}
