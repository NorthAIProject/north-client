package insights

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/NorthAIProject/north-client/internal/coach"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/stats/stat"
	"github.com/NorthAIProject/north-client/internal/users"
)

// consistencyWeeks is how far back the consistency page looks: a quarter,
// in whole weeks, long enough for a habit and short enough to still be this
// season's.
const consistencyWeeks = 12

// ConsistencyData is how steadily someone shows up, plus the check-in streak
// the check-in screen already counts.
type ConsistencyData struct {
	stat.Consistency
	CheckInStreak int
}

// Consistency reads which days over the last consistencyWeeks weeks had a
// finished workout, lifted sets or a check-in. Every source is optional; a
// missing one simply contributes no days.
func (s *Service) Consistency(ctx context.Context, user users.User, now time.Time) (ConsistencyData, error) {
	loc := user.Location()
	today := timerange.StartOfDay(now.In(loc))
	monday := today.AddDate(0, 0, -((int(today.Weekday()) + 6) % 7))
	rg := timerange.Between(monday.AddDate(0, 0, -7*(consistencyWeeks-1)), today.AddDate(0, 0, 1))
	day := func(t time.Time) time.Time { return timerange.StartOfDay(t.In(loc)) }

	trained, checked := map[time.Time]bool{}, map[time.Time]bool{}
	var out ConsistencyData
	var trainedDays, setDays, checkDays []time.Time
	g, gctx := errgroup.WithContext(ctx)
	if s.activity != nil {
		g.Go(func() error {
			sessions, err := s.activity.ListBetween(gctx, user.ID, rg)
			for _, sess := range sessions {
				if sess.EndedAt != nil {
					trainedDays = append(trainedDays, day(*sess.EndedAt))
				}
			}
			return err
		})
	}
	if s.sets != nil {
		g.Go(func() error {
			sets, err := s.sets.Between(gctx, user, rg)
			for _, set := range sets {
				setDays = append(setDays, day(set.PerformedAt))
			}
			return err
		})
	}
	if s.checkins != nil {
		g.Go(func() error {
			rows, err := s.checkins.ListBetween(gctx, user.ID, rg)
			for _, c := range rows {
				checkDays = append(checkDays, day(c.LocalDate))
			}
			return err
		})
		g.Go(func() (err error) {
			out.CheckInStreak, err = s.checkins.StreakAt(gctx, user, now)
			return
		})
	}
	if err := g.Wait(); err != nil {
		return ConsistencyData{}, err
	}
	for _, d := range append(trainedDays, setDays...) {
		trained[d] = true
	}
	for _, d := range checkDays {
		checked[d] = true
	}
	out.Consistency = stat.ConsistencyOf(trained, checked, today, consistencyWeeks)
	return out, nil
}

// Sentence is the consistency as the coach reads it.
func (c ConsistencyData) Sentence() string {
	line := fmt.Sprintf("Consistency: active %d days this week", c.ThisWeek)
	if c.UsualPerWeek > 0 {
		line += fmt.Sprintf(" (usually %.1f)", c.UsualPerWeek)
	}
	line += fmt.Sprintf("; current streak %d days, longest %d and longest gap %d in the last %d weeks",
		c.CurrentStreak, c.LongestStreak, c.LongestGap, consistencyWeeks)
	if c.CheckInStreak > 0 {
		line += fmt.Sprintf("; check-in streak %d days", c.CheckInStreak)
	}
	return line + "."
}

// ConsistencyContextSource tells the coach how steadily the person shows up,
// in the words of the consistency page.
type ConsistencyContextSource struct {
	svc *Service
	now func() time.Time
}

// NewConsistencyContextSource builds the source. A nil now means the real
// clock.
func NewConsistencyContextSource(svc *Service, now func() time.Time) *ConsistencyContextSource {
	if now == nil {
		now = time.Now
	}
	return &ConsistencyContextSource{svc: svc, now: now}
}

func (s *ConsistencyContextSource) Name() string { return "consistency" }

func (s *ConsistencyContextSource) Collect(ctx context.Context, req coach.ContextRequest, into *coach.Context) error {
	c, err := s.svc.Consistency(ctx, req.User, s.now())
	if err != nil {
		return err
	}
	// Nothing in twelve weeks says nothing worth a line.
	if c.LongestStreak == 0 {
		return nil
	}
	into.DailySignals = append(into.DailySignals, c.Sentence())
	return nil
}

var _ coach.ContextSource = (*ConsistencyContextSource)(nil)
