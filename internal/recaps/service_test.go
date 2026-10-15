package recaps_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/achievements/achievement"
	"github.com/NorthAIProject/north-client/internal/activity/activity"
	"github.com/NorthAIProject/north-client/internal/checkins/checkin"
	"github.com/NorthAIProject/north-client/internal/lifts/lift"
	"github.com/NorthAIProject/north-client/internal/meals/meal"
	"github.com/NorthAIProject/north-client/internal/recaps"
	"github.com/NorthAIProject/north-client/internal/recaps/recap"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/users"
)

type fakeSessions []activity.Session

func (f fakeSessions) ListBetween(context.Context, uuid.UUID, timerange.Range) ([]activity.Session, error) {
	return f, nil
}

type fakeSets []lift.Set

func (f fakeSets) Between(context.Context, users.User, timerange.Range) ([]lift.Set, error) {
	return f, nil
}

type fakeFood []meal.FoodLogEntry

func (f fakeFood) Range(context.Context, uuid.UUID, time.Time, time.Time) ([]meal.FoodLogEntry, error) {
	return f, nil
}

type fakeCheckIns struct {
	rows   []checkin.CheckIn
	streak int
}

func (f fakeCheckIns) ListBetween(context.Context, uuid.UUID, timerange.Range) ([]checkin.CheckIn, error) {
	return f.rows, nil
}

func (f fakeCheckIns) StreakAt(context.Context, users.User, time.Time) (int, error) {
	return f.streak, nil
}

type posted struct {
	category, kind, sourceKey, title, detail string
}

type fakeAchievements struct {
	own      []achievement.Item
	training bool
	posts    []posted
	set      []achievement.Sharing
}

func (f *fakeAchievements) OwnBetween(context.Context, uuid.UUID, time.Time, time.Time) ([]achievement.Item, error) {
	return f.own, nil
}

func (f *fakeAchievements) Post(_ context.Context, _ uuid.UUID, category, kind, sourceKey, title, detail string, at time.Time) (achievement.Item, error) {
	f.posts = append(f.posts, posted{category, kind, sourceKey, title, detail})
	return achievement.Item{ID: uuid.MustParse("dddddddd-dddd-dddd-dddd-dddddddddddd"), OccurredAt: at}, nil
}

func (f *fakeAchievements) Sharing(context.Context, uuid.UUID) (achievement.Sharing, error) {
	return achievement.Sharing{Training: f.training, Goals: true}, nil
}

func (f *fakeAchievements) SetSharing(_ context.Context, _ uuid.UUID, in achievement.Sharing) (achievement.Sharing, error) {
	f.set = append(f.set, in)
	f.training = in.Training
	return in, nil
}

// Lisbon is UTC+1 in October, so a workout ending at 23:30 UTC belongs to the
// next local day.
var (
	lisbon = mustLoad("Europe/Lisbon")
	now    = time.Date(2026, 10, 15, 10, 0, 0, 0, lisbon)
	person = users.User{ID: uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"), Timezone: "Europe/Lisbon"}
)

func mustLoad(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

func date(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

func ended(t time.Time) activity.Session { return activity.Session{EndedAt: &t} }

func newService(ach *fakeAchievements) *recaps.Service {
	return recaps.NewService(recaps.Options{
		Sessions: fakeSessions{
			ended(time.Date(2026, 10, 14, 23, 30, 0, 0, time.UTC)), // 15 Oct in Lisbon
			ended(time.Date(2026, 10, 5, 18, 0, 0, 0, time.UTC)),   // prior week
			{}, // still running: no end, not counted
		},
		Sets: fakeSets{
			{LogDate: date(2026, 10, 10)},
			{LogDate: date(2026, 10, 10)},
			{LogDate: date(2026, 10, 10)},
			{LogDate: date(2026, 9, 1)}, // outside both windows
		},
		Food:         fakeFood{{LogDate: date(2026, 10, 12)}, {LogDate: date(2026, 10, 12)}},
		CheckIns:     fakeCheckIns{rows: []checkin.CheckIn{{LocalDate: date(2026, 10, 15)}}, streak: 4},
		Achievements: ach,
		Now:          func() time.Time { return now },
	})
}

func TestLoadWeek(t *testing.T) {
	t.Parallel()
	ach := &fakeAchievements{}
	r, err := newService(ach).Load(context.Background(), person, recap.KeyWeek)
	if err != nil {
		t.Fatal(err)
	}

	if got, want := r.Since.Format(time.DateOnly), "2026-10-09"; got != want {
		t.Errorf("since = %s, want %s", got, want)
	}
	if got, want := r.Until.Format(time.DateOnly), "2026-10-15"; got != want {
		t.Errorf("until = %s, want %s", got, want)
	}
	if len(r.Days) != 7 {
		t.Fatalf("days = %d, want 7", len(r.Days))
	}
	want := recap.Totals{ActiveDays: 3, Workouts: 1, Sets: 3, FoodLogs: 2, CheckIns: 1}
	if r.Totals != want {
		t.Errorf("totals = %+v, want %+v", r.Totals, want)
	}
	if prior := (recap.Totals{ActiveDays: 1, Workouts: 1}); r.Prior != prior {
		t.Errorf("prior = %+v, want %+v", r.Prior, prior)
	}
	last := r.Days[6]
	if last.Workouts != 1 || last.CheckIns != 1 {
		t.Errorf("15 Oct = %+v, want the late workout and the check-in", last)
	}
	if r.CheckInStreak != 4 {
		t.Errorf("streak = %d, want 4", r.CheckInStreak)
	}
}

func TestLoadMonthHasThirtyDays(t *testing.T) {
	t.Parallel()
	r, err := newService(&fakeAchievements{}).Load(context.Background(), person, recap.KeyMonth)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Days) != 30 {
		t.Fatalf("days = %d, want 30", len(r.Days))
	}
	// The prior-week workout is inside the month now.
	if r.Totals.Workouts != 2 || r.Totals.ActiveDays != 4 {
		t.Errorf("totals = %+v, want 2 workouts over 4 active days", r.Totals)
	}
}

func TestLoadRejectsOtherRanges(t *testing.T) {
	t.Parallel()
	_, err := newService(&fakeAchievements{}).Load(context.Background(), person, "year")
	var fe apperr.FieldErrors
	if !errors.As(err, &fe) {
		t.Fatalf("err = %v, want field errors", err)
	}
}

func TestHighlightsSkipSharedRecapsAndCap(t *testing.T) {
	t.Parallel()
	ach := &fakeAchievements{own: []achievement.Item{{Kind: achievement.KindRecapShared, Title: "Recap · last 7 days"}}}
	for range 7 {
		ach.own = append(ach.own, achievement.Item{Kind: achievement.KindWorkoutCompleted, Title: "Finished Legs"})
	}
	r, err := newService(ach).Load(context.Background(), person, recap.KeyWeek)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Highlights) != 5 {
		t.Fatalf("highlights = %d, want 5", len(r.Highlights))
	}
	for _, h := range r.Highlights {
		if h.Kind == achievement.KindRecapShared {
			t.Fatal("a shared recap is listed as a highlight of itself")
		}
	}
}

func TestSharePostsToTraining(t *testing.T) {
	t.Parallel()
	ach := &fakeAchievements{training: false}
	shared, err := newService(ach).Share(context.Background(), person, recap.KeyWeek, false)
	if err != nil {
		t.Fatal(err)
	}
	if shared.TrainingShared {
		t.Error("trainingShared = true with training not shared")
	}
	if len(ach.posts) != 1 {
		t.Fatalf("posts = %d, want 1", len(ach.posts))
	}
	got := ach.posts[0]
	want := posted{
		category:  achievement.CategoryTraining,
		kind:      achievement.KindRecapShared,
		sourceKey: "week:2026-10-15",
		title:     "Recap · last 7 days",
		detail:    "3 active days · 1 workout · 3 sets · 2 foods logged · 1 check-in",
	}
	if got != want {
		t.Errorf("post = %+v\nwant   %+v", got, want)
	}
}

func TestDetailLeavesOutZeros(t *testing.T) {
	t.Parallel()
	if got, want := recaps.Detail(recap.Totals{}), "0 active days"; got != want {
		t.Errorf("Detail(empty) = %q, want %q", got, want)
	}
	if got, want := recaps.Detail(recap.Totals{ActiveDays: 1, Sets: 1}), "1 active day · 1 set"; got != want {
		t.Errorf("Detail = %q, want %q", got, want)
	}
}

func TestShareCanTurnOnTraining(t *testing.T) {
	t.Parallel()
	ach := &fakeAchievements{}
	shared, err := newService(ach).Share(context.Background(), person, recap.KeyWeek, true)
	if err != nil {
		t.Fatal(err)
	}
	if !shared.TrainingShared {
		t.Error("trainingShared = false after asking to share training")
	}
	if len(ach.set) != 1 || !ach.set[0].Training || !ach.set[0].Goals {
		t.Errorf("sharing saved = %+v, want training on and goals kept", ach.set)
	}

	// Already on: nothing to save.
	ach.set = nil
	if _, err := newService(ach).Share(context.Background(), person, recap.KeyWeek, true); err != nil {
		t.Fatal(err)
	}
	if len(ach.set) != 0 {
		t.Errorf("sharing saved again = %+v, want no write", ach.set)
	}
}
