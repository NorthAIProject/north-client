// Package recaps sums up what somebody did over the last 7 or 30 days — the
// "your week" card on the home screen — and posts that summary to the friends
// feed when they choose to share it.
//
// Nothing here is stored. A recap is read from the logs that already exist
// (sessions, sets, food, check-ins) each time it is asked for; only a shared
// recap leaves a row, as an achievement.
package recaps

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

	"github.com/NorthAIProject/north-client/internal/achievements/achievement"
	"github.com/NorthAIProject/north-client/internal/activity/activity"
	"github.com/NorthAIProject/north-client/internal/checkins/checkin"
	"github.com/NorthAIProject/north-client/internal/lifts/lift"
	"github.com/NorthAIProject/north-client/internal/meals/meal"
	"github.com/NorthAIProject/north-client/internal/recaps/recap"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/users"
)

// Sessions is the slice of activity.Service a recap reads: workouts finished
// inside a window.
type Sessions interface {
	ListBetween(ctx context.Context, userID uuid.UUID, rg timerange.Range) ([]activity.Session, error)
}

// Sets is the slice of lifts.Service a recap reads.
type Sets interface {
	Between(ctx context.Context, user users.User, rg timerange.Range) ([]lift.Set, error)
}

// Food is the slice of meals.FoodLogService a recap reads. Both dates are
// inclusive.
type Food interface {
	Range(ctx context.Context, userID uuid.UUID, from, to time.Time) ([]meal.FoodLogEntry, error)
}

// CheckIns is the slice of checkins.Service a recap reads.
type CheckIns interface {
	ListBetween(ctx context.Context, userID uuid.UUID, rg timerange.Range) ([]checkin.CheckIn, error)
	StreakAt(ctx context.Context, user users.User, now time.Time) (int, error)
}

// Achievements is the slice of achievements.Service a recap reads its
// highlights from and posts to.
type Achievements interface {
	OwnBetween(ctx context.Context, userID uuid.UUID, since, until time.Time) ([]achievement.Item, error)
	Post(ctx context.Context, userID uuid.UUID, category, kind, sourceKey, title, detail string, at time.Time) (achievement.Item, error)
	Sharing(ctx context.Context, userID uuid.UUID) (achievement.Sharing, error)
}

// Options are the sources. All are required: a recap missing one would show
// a zero that is not true.
type Options struct {
	Sessions     Sessions
	Sets         Sets
	Food         Food
	CheckIns     CheckIns
	Achievements Achievements
	// Now is the clock; nil means the real one.
	Now func() time.Time
}

// maxHighlights is how many achievements a recap lists. A recap is a glance;
// the feed holds the rest.
const maxHighlights = 5

type Service struct {
	sessions     Sessions
	sets         Sets
	food         Food
	checkins     CheckIns
	achievements Achievements
	now          func() time.Time
}

func NewService(opts Options) *Service {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return &Service{
		sessions:     opts.Sessions,
		sets:         opts.Sets,
		food:         opts.Food,
		checkins:     opts.CheckIns,
		achievements: opts.Achievements,
		now:          now,
	}
}

// ValidKey reports whether key names a recap window.
func ValidKey(key string) bool {
	return key == recap.KeyWeek || key == recap.KeyMonth
}

// Load builds the recap for the last 7 (week) or 30 (month) days, ending
// today in the person's zone.
func (s *Service) Load(ctx context.Context, user users.User, key string) (recap.Recap, error) {
	if !ValidKey(key) {
		return recap.Recap{}, apperr.FieldErrors{}.Add("range", "Use week or month.")
	}
	now := s.now()
	rg := timerange.ParseAt(key, user.Location(), now)
	prev := rg.Previous()
	// One read over both windows, split afterwards, rather than two of each.
	span := timerange.Between(prev.Since, rg.Until)

	var (
		sessions   []activity.Session
		sets       []lift.Set
		food       []meal.FoodLogEntry
		checkIns   []checkin.CheckIn
		streak     int
		highlights []achievement.Item
	)
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() (err error) {
		sessions, err = s.sessions.ListBetween(gctx, user.ID, span)
		return apperr.Wrap(err, "recap sessions")
	})
	g.Go(func() (err error) {
		sets, err = s.sets.Between(gctx, user, span)
		return apperr.Wrap(err, "recap sets")
	})
	g.Go(func() (err error) {
		food, err = s.food.Range(gctx, user.ID, span.Since, span.Until.AddDate(0, 0, -1))
		return apperr.Wrap(err, "recap food")
	})
	g.Go(func() (err error) {
		checkIns, err = s.checkins.ListBetween(gctx, user.ID, span)
		return apperr.Wrap(err, "recap check-ins")
	})
	g.Go(func() (err error) {
		streak, err = s.checkins.StreakAt(gctx, user, now)
		return apperr.Wrap(err, "recap streak")
	})
	g.Go(func() (err error) {
		highlights, err = s.achievements.OwnBetween(gctx, user.ID, rg.Since, rg.Until)
		return apperr.Wrap(err, "recap highlights")
	})
	if err := g.Wait(); err != nil {
		return recap.Recap{}, err
	}

	loc := rg.Location()
	byDay := map[string]*recap.Day{}
	for d := prev.Since; d.Before(rg.Until); d = d.AddDate(0, 0, 1) {
		byDay[dayKey(d)] = &recap.Day{Date: d}
	}
	count := func(key string, add func(*recap.Day)) {
		if d, ok := byDay[key]; ok {
			add(d)
		}
	}
	for _, sess := range sessions {
		if sess.EndedAt != nil {
			count(dayKey(sess.EndedAt.In(loc)), func(d *recap.Day) { d.Workouts++ })
		}
	}
	// Sets, food and check-ins carry the calendar date they were logged
	// for, so they are keyed by that date and not moved by the zone.
	for _, set := range sets {
		count(dayKey(set.LogDate), func(d *recap.Day) { d.Sets++ })
	}
	for _, f := range food {
		count(dayKey(f.LogDate), func(d *recap.Day) { d.FoodLogs++ })
	}
	for _, c := range checkIns {
		count(dayKey(c.LocalDate), func(d *recap.Day) { d.CheckIns++ })
	}

	out := recap.Recap{
		Key:           key,
		Since:         rg.Since,
		Until:         rg.Until.AddDate(0, 0, -1),
		CheckInStreak: streak,
	}
	for d := prev.Since; d.Before(rg.Until); d = d.AddDate(0, 0, 1) {
		day := *byDay[dayKey(d)]
		if d.Before(rg.Since) {
			addTo(&out.Prior, day)
			continue
		}
		addTo(&out.Totals, day)
		out.Days = append(out.Days, day)
	}
	for _, h := range highlights {
		if h.Kind == achievement.KindRecapShared || len(out.Highlights) == maxHighlights {
			continue
		}
		out.Highlights = append(out.Highlights, recap.Highlight{
			Kind: h.Kind, Title: h.Title, Detail: h.Detail, OccurredAt: h.OccurredAt,
		})
	}
	return out, nil
}

// Share posts the recap to the friends feed. Sharing the same window twice on
// the same day updates the one post with the latest numbers.
func (s *Service) Share(ctx context.Context, user users.User, key string) (recap.Shared, error) {
	r, err := s.Load(ctx, user, key)
	if err != nil {
		return recap.Shared{}, err
	}
	item, err := s.achievements.Post(ctx, user.ID, achievement.CategoryTraining, achievement.KindRecapShared,
		key+":"+dayKey(r.Until), Title(key), Detail(r.Totals), s.now())
	if err != nil {
		return recap.Shared{}, err
	}
	sharing, err := s.achievements.Sharing(ctx, user.ID)
	if err != nil {
		return recap.Shared{}, err
	}
	return recap.Shared{ID: item.ID, TrainingShared: sharing.Training}, nil
}

// Title names a shared recap in the feed.
func Title(key string) string {
	if key == recap.KeyMonth {
		return "Shared their last 30 days"
	}
	return "Shared their last 7 days"
}

// Detail is a shared recap's numbers in one line, leaving out what is zero:
// "5 active days · 3 workouts · 48 sets".
func Detail(t recap.Totals) string {
	parts := []string{plural(t.ActiveDays, "active day", "active days")}
	for _, p := range []struct {
		n         int
		one, many string
	}{
		{t.Workouts, "workout", "workouts"},
		{t.Sets, "set", "sets"},
		{t.FoodLogs, "food logged", "foods logged"},
		{t.CheckIns, "check-in", "check-ins"},
	} {
		if p.n > 0 {
			parts = append(parts, plural(p.n, p.one, p.many))
		}
	}
	return strings.Join(parts, " · ")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

func addTo(t *recap.Totals, d recap.Day) {
	t.Workouts += d.Workouts
	t.Sets += d.Sets
	t.FoodLogs += d.FoodLogs
	t.CheckIns += d.CheckIns
	if d.Active() {
		t.ActiveDays++
	}
}

// dayKey is a calendar date as written, ignoring the zone: a date column comes
// back as midnight UTC and a local midnight is already in the person's zone,
// and both mean the date they print.
func dayKey(t time.Time) string { return t.Format("2006-01-02") }
