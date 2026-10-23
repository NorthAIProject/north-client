package nudges

import (
	"context"
	"time"

	"github.com/NorthAIProject/north-client/internal/notifications"
	"github.com/NorthAIProject/north-client/internal/shared/lifedomain"
	"github.com/NorthAIProject/north-client/internal/users"
)

// First-week notes stop after this many local days.
const firstWeekDays = 7

// FirstWeekDay is a running first week (internal/firstweek) as the evening
// note needs it: the programme day, 1 to 7, its template, and whether
// today's action is done.
type FirstWeekDay struct {
	Day      int
	Template string
	Done     bool
}

// firstWeekSource reports the person's running first week; ok is false when
// none is on one of its seven days. Declared here, with FirstWeekFunc, so
// this package does not import internal/firstweek.
type firstWeekSource interface {
	FirstWeekToday(ctx context.Context, user users.User) (FirstWeekDay, bool, error)
}

// FirstWeekFunc adapts a function to the first-week source.
type FirstWeekFunc func(ctx context.Context, user users.User) (FirstWeekDay, bool, error)

func (f FirstWeekFunc) FirstWeekToday(ctx context.Context, user users.User) (FirstWeekDay, bool, error) {
	return f(ctx, user)
}

func (s *Service) evalFirstWeek(ctx context.Context, user users.User, prefs notifications.Prefs, today, now time.Time) (int, error) {
	// A running programme owns the week: its evening note is the only
	// first-week note, and the morning one comes from internal/outreach.
	if s.firstWeek != nil {
		day, ok, err := s.firstWeek.FirstWeekToday(ctx, user)
		if err != nil {
			return 0, err
		}
		if ok {
			return s.evalFirstWeekEvening(ctx, user, prefs, day, today, now)
		}
	}
	if s.week == nil {
		return 0, nil
	}

	age := daysBetween(onboardedLocal(user, today), today)
	if age < 1 || age > firstWeekDays {
		return 0, nil
	}

	created := 0

	if age == 1 {
		n, err := s.evalFirstWeekCheck(ctx, user, today)
		if err != nil {
			return created, err
		}
		created += n
	}
	if age == 3 {
		n, err := s.evalFirstWeekEvidence(ctx, user, today)
		if err != nil {
			return created, err
		}
		created += n
	}
	if age == 7 {
		n, err := s.evalFirstWeekReview(ctx, user, today)
		if err != nil {
			return created, err
		}
		created += n
	}
	return created, nil
}

// evalFirstWeekEvening asks once, at the person's evening hour, for a
// first-week day whose action is not done yet. The window is the evening
// reflection's: a sweep that missed the hour catches up within it, and a day
// past it is left alone.
func (s *Service) evalFirstWeekEvening(ctx context.Context, user users.User, prefs notifications.Prefs, day FirstWeekDay, today, now time.Time) (int, error) {
	hour := now.In(user.Location()).Hour()
	if day.Done || hour < prefs.EveningHour || hour >= prefs.EveningHour+eveningReflectionWindow {
		return 0, nil
	}
	_, inserted, err := s.Raise(ctx, user, Draft{
		Kind:      KindFirstWeekEvening,
		DedupeKey: today.Format("2006-01-02"),
		Title:     text(user, "nudges.firstweek.evening.title", day.Day),
		Body:      text(user, "nudges.firstweek.evening.body", text(user, "firstweek."+day.Template+".action")),
		Href:      "/app",
		Push:      true,
	})
	if err != nil || !inserted {
		return 0, err
	}
	return 1, nil
}

func (s *Service) evalFirstWeekCheck(ctx context.Context, user users.User, today time.Time) (int, error) {
	if s.checkins != nil {
		last, ok, err := s.checkins.LatestLocalDate(ctx, user.ID)
		if err != nil {
			return 0, err
		}
		if ok && !last.Before(onboardedLocal(user, today)) {
			return 0, nil
		}
	}

	count, err := s.week.UserMessageCount(ctx, user.ID)
	if err != nil {
		return 0, err
	}
	// The seeded opening message is one user turn. A second means they came back.
	if count > 1 {
		return 0, nil
	}

	_, inserted, err := s.Raise(ctx, user, Draft{
		Kind:      KindFirstWeekCheck,
		DedupeKey: today.Format("2006-01-02"),
		Title:     text(user, "nudges.firstweek.check.title"),
		Body:      text(user, "nudges.firstweek.check.body"),
		Href:      "/app/chat",
	})
	if err != nil {
		return 0, err
	}
	if inserted {
		return 1, nil
	}
	return 0, nil
}

func (s *Service) evalFirstWeekEvidence(ctx context.Context, user users.User, today time.Time) (int, error) {
	focused, err := s.week.HasLifeFocus(ctx, user.ID, lifedomain.Fitness, lifedomain.Health)
	if err != nil || !focused {
		return 0, err
	}

	has, err := s.week.HasEvidence(ctx, user.ID)
	if err != nil || has {
		return 0, err
	}

	_, inserted, err := s.Raise(ctx, user, Draft{
		Kind:      KindFirstWeekEvidence,
		DedupeKey: today.Format("2006-01-02"),
		Title:     text(user, "nudges.firstweek.evidence.title"),
		Body:      text(user, "nudges.firstweek.evidence.body"),
		Href:      "/app/chat",
	})
	if err != nil {
		return 0, err
	}
	if inserted {
		return 1, nil
	}
	return 0, nil
}

func (s *Service) evalFirstWeekReview(ctx context.Context, user users.User, today time.Time) (int, error) {
	_, inserted, err := s.Raise(ctx, user, Draft{
		Kind:      KindFirstWeekReview,
		DedupeKey: today.Format("2006-01-02"),
		Title:     text(user, "nudges.firstweek.review.title"),
		Body:      text(user, "nudges.firstweek.review.body"),
		Href:      "/app/chat",
	})
	if err != nil {
		return 0, err
	}
	if inserted {
		return 1, nil
	}
	return 0, nil
}

// trainedToday reports whether a session already finished today's plan day.
// known is false when this service has no way to tell. Raising and resolving
// the training reminder both ask this, so a walk on leg day neither stops the
// reminder nor marks it done.
func (s *Service) trainedToday(ctx context.Context, user users.User, today time.Time) (done, known bool, err error) {
	tracker, ok := s.training.(interface {
		CompletedToday(ctx context.Context, user users.User, today time.Time) (bool, string, error)
	})
	if !ok {
		return false, false, nil
	}
	done, _, err = tracker.CompletedToday(ctx, user, today)
	return done, true, err
}

func (s *Service) evalWorkoutToday(ctx context.Context, user users.User, today time.Time) (int, error) {
	if s.training == nil {
		return 0, nil
	}

	title, href, due, err := s.training.DueToday(ctx, user, today)
	if err != nil || !due {
		return 0, err
	}

	done, _, err := s.trainedToday(ctx, user, today)
	if err != nil {
		return 0, err
	}
	// Already trained: nothing to nudge, and not a failure — returning
	// an error here would stop the rest of this person's nudges.
	if done {
		return 0, nil
	}
	if href == "" {
		href = "/app/fitness"
	}
	if title == "" {
		title = text(user, "nudges.workout.default")
	}

	_, inserted, err := s.Raise(ctx, user, Draft{
		Kind:      KindWorkoutToday,
		DedupeKey: today.Format("2006-01-02"),
		Title:     text(user, "nudges.workout.title"),
		Body:      title,
		Href:      href,
	})
	if err != nil {
		return 0, err
	}
	if inserted {
		return 1, nil
	}
	return 0, nil
}

func (s *Service) evalPhotoSchedule(ctx context.Context, user users.User, today time.Time) (int, error) {
	if s.schedules == nil || s.week == nil {
		return 0, nil
	}

	sched, err := s.schedules.PhotoSchedule(ctx, user.ID)
	if err != nil || !sched.Enabled || sched.EveryDays <= 0 {
		return 0, err
	}

	last, ok, err := s.week.LastEvidenceAt(ctx, user.ID)
	if err != nil {
		return 0, err
	}
	anchor := onboardedLocal(user, today)
	if ok && last.After(anchor) {
		anchor = localDate(user, last)
	}

	due := calendarDay(anchor.AddDate(0, 0, sched.EveryDays))
	if today.Before(due) {
		return 0, nil
	}

	if ok && !localDate(user, last).Before(due) {
		return 0, nil
	}

	cycle := due.Format("2006-01-02")
	created := 0

	_, inserted, err := s.Raise(ctx, user, Draft{
		Kind:      KindPhotoAsk,
		DedupeKey: cycle,
		Title:     text(user, "nudges.photo.ask.title"),
		Body:      text(user, "nudges.photo.ask.body"),
		Href:      "/app/chat",
	})
	if err != nil {
		return 0, err
	}
	if inserted {
		created++
	}

	if sched.ReminderDays <= 0 {
		return created, nil
	}
	remindOn := due.AddDate(0, 0, sched.ReminderDays)
	if today.Before(calendarDay(remindOn)) {
		return created, nil
	}

	_, inserted, err = s.Raise(ctx, user, Draft{
		Kind:      KindPhotoReminder,
		DedupeKey: cycle,
		Title:     text(user, "nudges.photo.reminder.title"),
		Body:      text(user, "nudges.photo.reminder.body"),
		Href:      "/app/chat",
	})
	if err != nil {
		return created, err
	}
	if inserted {
		created++
	}
	return created, nil
}
