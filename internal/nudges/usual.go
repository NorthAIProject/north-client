package nudges

import (
	"context"
	"slices"
	"time"

	"github.com/NorthAIProject/north-client/internal/checkins/checkin"
	"github.com/NorthAIProject/north-client/internal/users"
)

// evalUsualTime is the daily reminder: one ask, a little after the time this
// person usually checks in, on a day they have not yet.
//
// Gain-framed on purpose. It is the first ask of the day for most people, and
// "day 9 is waiting" brings them back without the evening warning's "your
// streak ends at midnight", which is kept for the evening and stops at nine.
func (s *Service) evalUsualTime(ctx context.Context, user users.User, today, now time.Time) (int, error) {
	if s.checkins == nil {
		return 0, nil
	}
	due, ok, err := s.usualAskAt(ctx, user, today)
	if err != nil || !ok {
		return 0, err
	}
	local := now.In(user.Location())
	if local.Before(due) || !local.Before(due.Add(usualTimeWindow)) {
		return 0, nil
	}

	last, ok, err := s.checkins.LatestLocalDate(ctx, user.ID)
	if err != nil || !ok {
		return 0, err
	}
	if gap := daysBetween(last, today); gap == 0 || gap > usualTimeMaxGap {
		return 0, nil
	}
	if asked, askErr := s.askedForCheckInToday(ctx, user, today); askErr != nil || asked {
		return 0, askErr
	}

	run, err := s.checkins.RunAt(ctx, user, now)
	if err != nil {
		return 0, err
	}
	body := text(user, "nudges.usual.body")
	if run.Current > 0 {
		body = text(user, "nudges.usual.body.run", run.Current+1)
	}
	_, inserted, err := s.Raise(ctx, user, Draft{
		Kind:      KindUsualTime,
		DedupeKey: today.Format("2006-01-02"),
		Title:     text(user, "nudges.usual.title"),
		Body:      body,
		Href:      "/app/check-ins",
	})
	if err != nil || !inserted {
		return 0, err
	}
	return 1, nil
}

// usualAskAt is when today's reminder is due, in the person's zone, or false
// when there is not yet a habit to learn the time from.
func (s *Service) usualAskAt(ctx context.Context, user users.User, today time.Time) (time.Time, bool, error) {
	list, err := s.checkins.List(ctx, user.ID, usualTimeSamples)
	if err != nil {
		return time.Time{}, false, err
	}
	minute, ok := usualMinute(list, user.Location())
	if !ok {
		return time.Time{}, false, nil
	}
	// time.Date rather than Add: on a clock-change day the usual 08:00 is
	// still 08:00.
	usual := time.Date(today.Year(), today.Month(), today.Day(), 0, minute, 0, 0, user.Location())
	return usual.Add(usualTimeDelay), true, nil
}

// usualMinute is the median minute of the day check-ins are saved at.
//
// Only check-ins saved on the day they are for count: one filed late for
// yesterday says nothing about the habit. Days the reminder asked on do
// count. Answers to it come after the due time, above the median, so they
// cannot drag it later; leaving them out would keep only the days the person
// beat the reminder and slide the median earlier with every ask.
func usualMinute(list []checkin.CheckIn, loc *time.Location) (int, bool) {
	var minutes []int
	for _, c := range list {
		saved := c.CreatedAt.In(loc)
		if saved.Format("2006-01-02") != c.LocalDate.Format("2006-01-02") {
			continue
		}
		minutes = append(minutes, saved.Hour()*60+saved.Minute())
	}
	if len(minutes) < usualTimeMin {
		return 0, false
	}
	slices.Sort(minutes)
	return minutes[len(minutes)/2], true
}

// askedForCheckInToday reports whether any ask for today's check-in has
// already gone out. They share one slot a day.
func (s *Service) askedForCheckInToday(ctx context.Context, user users.User, today time.Time) (bool, error) {
	n, err := s.repo.CountForDay(ctx, user.ID, checkInAsks, today.Format("2006-01-02"))
	return n > 0, err
}

// usualAskStillDue reports whether today's reminder is yet to go out before
// the evening warning would end, so the warning can wait for it rather than
// race it with stronger words.
func (s *Service) usualAskStillDue(ctx context.Context, user users.User, today, now time.Time) (bool, error) {
	due, ok, err := s.usualAskAt(ctx, user, today)
	if err != nil || !ok {
		return false, err
	}
	end := time.Date(today.Year(), today.Month(), today.Day(), streakAtRiskEnd, 0, 0, 0, user.Location())
	return now.Before(due) && due.Before(end), nil
}
