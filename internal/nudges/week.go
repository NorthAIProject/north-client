package nudges

import (
	"context"
	"time"

	"github.com/NorthAIProject/north-client/internal/shared/lifedomain"
	"github.com/NorthAIProject/north-client/internal/users"
)

// First-week notes stop after this many local days.
const firstWeekDays = 7

func (s *Service) evalFirstWeek(ctx context.Context, user users.User, today time.Time) (int, error) {
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
		Title:     "How did yesterday go?",
		Body:      "Thirty seconds. What happened, or send a photo of where you are now.",
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
		Title:     "Send one photo",
		Body:      "A lift, a meal, or how you look today. I will coach from what I see.",
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
		Title:     "Your first week",
		Body:      "Open the coach and tell me what stuck. I will keep what matters.",
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

func (s *Service) evalWorkoutToday(ctx context.Context, user users.User, today time.Time) (int, error) {
	if s.training == nil {
		return 0, nil
	}

	title, href, due, err := s.training.DueToday(ctx, user, today)
	if err != nil || !due {
		return 0, err
	}
	if href == "" {
		href = "/app/fitness"
	}
	if title == "" {
		title = "Today's session"
	}

	_, inserted, err := s.Raise(ctx, user, Draft{
		Kind:      KindWorkoutToday,
		DedupeKey: today.Format("2006-01-02"),
		Title:     "Start today's session",
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
		Title:     "Send a photo",
		Body:      "Your photo check-in is due. A lift, a meal, or how you look today.",
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
		Title:     "Still waiting on that photo",
		Body:      "A reminder: send one photo so I can coach from what I see.",
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
