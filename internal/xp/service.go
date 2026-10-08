package xp

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/achievements"
	"github.com/NorthAIProject/north-client/internal/achievements/achievement"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/users"
	xpdb "github.com/NorthAIProject/north-client/internal/xp/db"
)

// StreakReader is the check-in streak, as the check-ins slice counts it.
type StreakReader interface {
	StreakAt(ctx context.Context, user users.User, now time.Time) (int, error)
}

type Service struct {
	q       *xpdb.Queries
	streaks StreakReader
}

func NewService(pool *pgxpool.Pool, streaks StreakReader) *Service {
	return &Service{q: xpdb.New(pool), streaks: streaks}
}

// beginning is the start of "all time": before anything was ever recorded.
var beginning = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

// window is a span of local days and the instants that bound it.
type window struct {
	fromDay, toDay string
	fromAt, toAt   time.Time
}

// weekOf is the viewer's week so far: Monday 00:00 local until now.
func weekOf(loc *time.Location, now time.Time) window {
	start := timerange.StartOfWeek(now.In(loc))
	return window{
		fromDay: start.Format(time.DateOnly), toDay: now.In(loc).AddDate(0, 0, 1).Format(time.DateOnly),
		fromAt: start, toAt: now,
	}
}

func allTime(loc *time.Location, now time.Time) window {
	return window{
		fromDay: beginning.Format(time.DateOnly), toDay: now.In(loc).AddDate(0, 0, 1).Format(time.DateOnly),
		fromAt: beginning, toAt: now,
	}
}

// earned counts each person's verified actions in a window, by kind.
func (s *Service) earned(ctx context.Context, ids []uuid.UUID, w window) (map[uuid.UUID]map[string]int, error) {
	rows, err := s.q.Earned(ctx, xpdb.EarnedParams{
		UserIds: ids, FromDay: w.fromDay, ToDay: w.toDay, FromAt: w.fromAt, ToAt: w.toAt,
		StreakFrom: StreakDayFrom, WorkoutMinSeconds: WorkoutMinMinutes * 60, WorkoutsPerDay: WorkoutsPaidPerDay,
		StreakMarks: streakMarks(), ChallengeMetKind: achievement.KindCrewChallengeMet,
	})
	if err != nil {
		return nil, apperr.Wrap(err, "earned")
	}
	out := make(map[uuid.UUID]map[string]int, len(ids))
	for _, r := range rows {
		if out[r.UserID] == nil {
			out[r.UserID] = map[string]int{}
		}
		out[r.UserID][r.Kind] = int(r.N)
	}
	return out, nil
}

// streakMarks are the streak lengths that pay a bonus: the same ones the
// friends feed records a moment for.
func streakMarks() []int32 {
	out := make([]int32, 0, len(achievements.StreakMarks))
	for _, m := range achievements.StreakMarks {
		out = append(out, int32(m))
	}
	return out
}

// pointsFor is what a count of one kind pays.
func pointsFor(kind string, n int) int {
	switch kind {
	case KindHabitKept:
		return n * PointsHabitKept
	case KindStreakDay:
		return n * PointsStreakDay
	case KindWorkout:
		return n * PointsWorkout
	case KindMilestone:
		return n * PointsMilestone
	case KindGoal:
		return n * PointsGoal
	case KindWeekReviewed:
		return n * PointsWeekReviewed
	case KindStreakMark:
		return n * PointsStreakMark
	case KindChallengeMet:
		return n * PointsChallengeMet
	}
	return 0
}

func total(counts map[string]int) int {
	sum := 0
	for kind, n := range counts {
		sum += pointsFor(kind, n)
	}
	return sum
}

// Summary is the person's XP this week, kind by kind, and their level.
func (s *Service) Summary(ctx context.Context, user users.User, now time.Time) (Summary, error) {
	loc := user.Location()
	ids := []uuid.UUID{user.ID}
	week, err := s.earned(ctx, ids, weekOf(loc, now))
	if err != nil {
		return Summary{}, err
	}
	all, err := s.earned(ctx, ids, allTime(loc, now))
	if err != nil {
		return Summary{}, err
	}
	out := Summary{Week: make([]Earned, 0, len(Kinds())), Total: total(all[user.ID])}
	for _, kind := range Kinds() {
		n := week[user.ID][kind]
		out.Week = append(out.Week, Earned{Kind: kind, Count: n, Points: pointsFor(kind, n)})
		out.WeekTotal += pointsFor(kind, n)
	}
	out.Level = LevelFor(out.Total)
	return out, nil
}

// categoryOf is the sharing switch that puts a person on a metric's board.
func categoryOf(metric string) (string, error) {
	switch metric {
	case MetricXP:
		return achievement.CategoryXP, nil
	case MetricStreak:
		return achievement.CategoryStreaks, nil
	case MetricWorkouts:
		return achievement.CategoryTraining, nil
	}
	return "", apperr.FieldErrors{}.Add("metric", "Use xp, streak or workouts.")
}

// Board ranks the viewer and the friends who share this metric with them.
// period applies to XP only: a streak is always the current one, and
// workouts are always this week's.
func (s *Service) Board(ctx context.Context, viewer users.User, metric, period string, now time.Time) (Board, error) {
	category, err := categoryOf(metric)
	if err != nil {
		return Board{}, err
	}
	switch {
	case metric != MetricXP:
		period = PeriodWeek
		if metric == MetricStreak {
			period = ""
		}
	case period == "":
		period = PeriodWeek
	case period != PeriodWeek && period != PeriodAll:
		return Board{}, apperr.FieldErrors{}.Add("period", "Use week or all.")
	}

	people, err := s.q.Participants(ctx, xpdb.ParticipantsParams{Viewer: viewer.ID, Category: category})
	if err != nil {
		return Board{}, apperr.Wrap(err, "participants")
	}
	sharing, err := s.q.Shares(ctx, xpdb.SharesParams{UserID: viewer.ID, Category: category})
	if err != nil {
		return Board{}, apperr.Wrap(err, "sharing")
	}

	values, err := s.values(ctx, viewer, people, metric, period, now)
	if err != nil {
		return Board{}, err
	}
	entries := make([]Entry, 0, len(people))
	for _, p := range people {
		e := Entry{UserID: p.ID, DisplayName: p.DisplayName, Value: values[p.ID].value, Me: p.ID == viewer.ID}
		if p.Handle != nil {
			e.Handle = *p.Handle
		}
		if metric == MetricXP {
			l := LevelFor(values[p.ID].lifetime)
			e.Level = &l
		}
		entries = append(entries, e)
	}
	rank(entries)
	return Board{Metric: metric, Period: period, Entries: entries, Sharing: sharing}, nil
}

type score struct{ value, lifetime int }

// values is each participant's number on the board. Windows are the viewer's:
// everybody on one board is measured over the same instants.
func (s *Service) values(ctx context.Context, viewer users.User, people []xpdb.ParticipantsRow, metric, period string, now time.Time) (map[uuid.UUID]score, error) {
	loc := viewer.Location()
	ids := make([]uuid.UUID, 0, len(people))
	for _, p := range people {
		ids = append(ids, p.ID)
	}
	out := make(map[uuid.UUID]score, len(people))

	switch metric {
	case MetricXP:
		all, err := s.earned(ctx, ids, allTime(loc, now))
		if err != nil {
			return nil, err
		}
		week := all
		if period == PeriodWeek {
			if week, err = s.earned(ctx, ids, weekOf(loc, now)); err != nil {
				return nil, err
			}
		}
		for _, id := range ids {
			out[id] = score{value: total(week[id]), lifetime: total(all[id])}
		}
	case MetricWorkouts:
		w := weekOf(loc, now)
		rows, err := s.q.WorkoutsFinished(ctx, xpdb.WorkoutsFinishedParams{UserIds: ids, FromAt: w.fromAt, ToAt: w.toAt})
		if err != nil {
			return nil, apperr.Wrap(err, "workouts")
		}
		for _, r := range rows {
			out[r.UserID] = score{value: int(r.N)}
		}
	case MetricStreak:
		for _, p := range people {
			n, err := s.streaks.StreakAt(ctx, users.User{ID: p.ID, Timezone: p.Timezone}, now)
			if err != nil {
				return nil, fmt.Errorf("streak: %w", err)
			}
			out[p.ID] = score{value: n}
		}
	}
	return out, nil
}

// rank sorts highest first and numbers the places, equal values sharing one
// ("1, 1, 3"). Ties are listed by name so the order does not shuffle.
func rank(entries []Entry) {
	slices.SortStableFunc(entries, func(a, b Entry) int {
		if c := cmp.Compare(b.Value, a.Value); c != 0 {
			return c
		}
		return cmp.Compare(a.DisplayName, b.DisplayName)
	})
	for i := range entries {
		if i > 0 && entries[i].Value == entries[i-1].Value {
			entries[i].Rank = entries[i-1].Rank
			continue
		}
		entries[i].Rank = i + 1
	}
}
