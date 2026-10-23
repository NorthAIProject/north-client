package firstweek_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/firstweek"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/users"
)

// A week finished with no coach review (check-ins off, quiet hours, the
// briefing took the morning) still ends with one: the catalogue's, counted
// from the week.
func TestFinishingWritesTheCataloguesReviewWhenTheCoachDidNot(t *testing.T) {
	f := setup(t, true)
	ctx := context.Background()
	if _, err := f.svc.Start(ctx, f.user, firstweek.TemplateSleep, uuid.Nil); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"2026-10-25", "2026-10-26", "2026-10-28"} {
		f.proof[d] = true
	}
	f.at(2, 9)
	if _, _, err := f.svc.Today(ctx, f.user); err != nil {
		t.Fatal(err)
	}
	f.at(3, 21)
	if _, err := f.svc.MarkDone(ctx, f.user); err != nil {
		t.Fatalf("day 3: %v", err)
	}
	f.at(5, 9)
	if _, _, err := f.svc.Today(ctx, f.user); err != nil {
		t.Fatal(err)
	}
	f.at(7, 20)
	if _, _, err := f.svc.Today(ctx, f.user); err != nil {
		t.Fatal(err)
	}

	today, err := f.svc.Finish(ctx, f.user)
	if err != nil {
		t.Fatalf("finish: %v", err)
	}
	r := today.Programme.Review
	if r == nil {
		t.Fatal("a finished week has no review")
	}
	// Days 1 and 2 were noticed from yesterday's log; day 3 was a tap; day
	// 4's log was noticed on day 5.
	if r.WrittenBy != firstweek.ReviewByTemplate || r.DaysDone != 4 || r.DaysByLog != 3 || r.DaysByTap != 1 {
		t.Errorf("review = %+v", *r)
	}
	if r.LoggedDays == nil || *r.LoggedDays != 3 {
		t.Errorf("logged days = %v, want 3", r.LoggedDays)
	}
	want := firstweek.ReviewFallback(i18n.WithLocale(ctx, "en"), 4)
	if r.Text != want {
		t.Errorf("text = %q, want %q", r.Text, want)
	}
}

// The coach's day-7 message is the review; whoever comes second keeps it.
// Opening it finishes the week once, and opening it again shows the same
// review and counts nothing.
func TestTheCoachsReviewIsKeptAndOpeningItFinishesTheWeekOnce(t *testing.T) {
	f := setup(t, true)
	ctx := context.Background()
	if _, err := f.svc.Start(ctx, f.user, firstweek.TemplateSleep, uuid.Nil); err != nil {
		t.Fatal(err)
	}
	f.at(7, 8)
	saved, err := f.svc.SaveReview(ctx, f.user.ID, firstweek.Review{Text: "A good week.", WrittenBy: firstweek.ReviewByCoach, DaysDone: 2})
	if err != nil || saved.Text != "A good week." {
		t.Fatalf("save = %+v, %v", saved, err)
	}
	again, err := f.svc.SaveReview(ctx, f.user.ID, firstweek.Review{Text: "Another one.", WrittenBy: firstweek.ReviewByCoach})
	if err != nil || again.Text != "A good week." {
		t.Errorf("second save = %+v, %v; want the first review kept", again, err)
	}

	f.at(7, 19)
	for i := range 2 {
		today, review, err := f.svc.OpenReview(ctx, f.user)
		if err != nil {
			t.Fatalf("open %d: %v", i, err)
		}
		if review.Text != "A good week." || today.Programme.Status != firstweek.StatusFinished {
			t.Errorf("open %d = %q, status %s", i, review.Text, today.Programme.Status)
		}
	}
	if n := len(f.events.named("first_week_finished")); n != 1 {
		t.Errorf("first_week_finished fired %d times, want 1", n)
	}
}

func TestThereIsNoReviewBeforeDaySevenOrForASkippedWeek(t *testing.T) {
	f := setup(t, true)
	ctx := context.Background()
	if _, err := f.svc.Start(ctx, f.user, firstweek.TemplateJournal, uuid.Nil); err != nil {
		t.Fatal(err)
	}
	f.at(6, 22)
	if _, _, err := f.svc.OpenReview(ctx, f.user); !apperr.Is(err, apperr.ErrConflict) {
		t.Errorf("review on day 6 = %v, want conflict", err)
	}
	if _, err := f.svc.Skip(ctx, f.user); err != nil {
		t.Fatal(err)
	}
	f.at(7, 9)
	if _, _, err := f.svc.OpenReview(ctx, f.user); !apperr.Is(err, apperr.ErrNotFound) {
		t.Errorf("review of a skipped week = %v, want not found", err)
	}
	if n := len(f.events.named("first_week_finished")); n != 0 {
		t.Errorf("first_week_finished fired %d times for a skipped week", n)
	}
}

// "3 of 3 days" is counted on the completion that earns it, a log noticed
// as much as a tap, and once.
func TestTheThreeOfThreeMomentIsCountedOnceHoweverItIsEarned(t *testing.T) {
	cases := []struct {
		name  string
		third func(f fixture) error
	}{
		{"by a tap", func(f fixture) error {
			_, err := f.svc.MarkDone(context.Background(), f.user)
			return err
		}},
		{"by a log", func(f fixture) error {
			f.proof["2026-10-27"] = true
			_, _, err := f.svc.Today(context.Background(), f.user)
			return err
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := setup(t, true)
			ctx := context.Background()
			if _, err := f.svc.Start(ctx, f.user, firstweek.TemplateSleep, uuid.Nil); err != nil {
				t.Fatal(err)
			}
			for day := 1; day <= 2; day++ {
				f.at(day, 21)
				if _, err := f.svc.MarkDone(ctx, f.user); err != nil {
					t.Fatalf("day %d: %v", day, err)
				}
			}
			f.at(3, 21)
			if n := len(f.events.named("moment_shown")); n != 0 {
				t.Fatalf("moment counted %d times before day 3 was done", n)
			}
			if err := c.third(f); err != nil {
				t.Fatal(err)
			}
			for range 2 {
				today, _, err := f.svc.Today(ctx, f.user)
				if err != nil || !today.ThreeOfThree() {
					t.Fatalf("three of three = %v, %v", today.ThreeOfThree(), err)
				}
			}
			got := f.events.named("moment_shown")
			if len(got) != 1 || got[0].Properties["kind"] != "first_week_three" {
				t.Errorf("moment_shown = %v, want one first_week_three", got)
			}
		})
	}
}

// answers records the coach-message answers TapDone sends.
type answers struct{ calls int }

func (a *answers) RecordFirstWeekDone(context.Context, users.User) error {
	a.calls++
	return nil
}

// A tap on the card or under a banner answers the coach's message too; the
// coach message's own button goes through outreach, which calls MarkDone and
// must not be answered twice.
func TestATapAnswersTheCoachsMessage(t *testing.T) {
	f := setup(t, true)
	ctx := context.Background()
	a := &answers{}
	f.svc.WithAnswers(a)
	if _, err := f.svc.Start(ctx, f.user, firstweek.TemplateCustom, uuid.Nil); err != nil {
		t.Fatal(err)
	}
	f.at(1, 9)
	if _, err := f.svc.MarkDone(ctx, f.user); err != nil {
		t.Fatal(err)
	}
	if a.calls != 0 {
		t.Errorf("MarkDone answered the coach message %d times", a.calls)
	}
	f.at(2, 9)
	today, err := f.svc.TapDone(ctx, f.user)
	if err != nil || !today.Done {
		t.Fatalf("tap done = %v, %v", today.Done, err)
	}
	if a.calls != 1 {
		t.Errorf("TapDone answered the coach message %d times, want 1", a.calls)
	}
}
