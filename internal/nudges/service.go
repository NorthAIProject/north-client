package nudges

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/analytics"
	"github.com/NorthAIProject/north-client/internal/goals"
	"github.com/NorthAIProject/north-client/internal/notifications"
	"github.com/NorthAIProject/north-client/internal/users"
)

const (
	listDefault = 20

	// Quiet for two full local days: a check-in on day D is first eligible
	// on D+3.
	missedCheckInDays = 2

	// Inclusive window of local days from today. Due today through today+7.
	goalDeadlineWindow = 7

	// A streak short enough that losing it is not a loss is not worth a
	// warning. Three is where the check-in page first celebrates it.
	streakAtRiskMin = 3

	// The local hour from which "your streak ends at midnight" may be raised.
	// Evening, so there is still time to act; before the default quiet hours
	// at 22:00, so the hourly sweep gets four chances.
	streakAtRiskHour = 18
)

type accounts interface {
	ByID(ctx context.Context, id uuid.UUID) (users.User, error)
	ListOnboarded(ctx context.Context, after uuid.UUID, limit int) ([]users.User, error)
}

type checkinDays interface {
	LatestLocalDate(ctx context.Context, userID uuid.UUID) (date time.Time, ok bool, err error)
	// StreakAt is the check-in streak as of now, with the same one-day grace
	// the check-in page shows: yesterday still counts until today is missed.
	StreakAt(ctx context.Context, user users.User, now time.Time) (int, error)
}

type activeGoals interface {
	ListActive(ctx context.Context, userID uuid.UUID) ([]goals.Goal, error)
}

// notificationPrefs is what this package needs from internal/notifications:
// permission to speak. Optional — a Service without it nudges as North always
// has, which is what the defaults say anyway.
type notificationPrefs interface {
	Get(ctx context.Context, userID uuid.UUID) (notifications.Prefs, error)
}

// fanout delivers a newly created nudge to a linked chat. Optional.
type fanout interface {
	Notify(ctx context.Context, userID uuid.UUID, text string) error
}

// pusher delivers a newly created nudge to the browsers this person subscribed
// (internal/push). Optional. It reports how many accepted the message so
// delivery is counted only where something actually arrived.
type pusher interface {
	Send(ctx context.Context, userID uuid.UUID, title, body, href string) (delivered int, err error)
}

// funnel reports deliveries and opens to the product funnel. Optional. These
// two events, per channel, are what say whether a nudge on the lock screen
// brings anybody back.
type funnel interface {
	NudgeDelivered(ctx context.Context, userID uuid.UUID, kind, channel string)
	NudgeOpened(ctx context.Context, userID uuid.UUID, kind, channel string)
}

// weekSource is what the first-week rules need besides check-ins.
type weekSource interface {
	UserMessageCount(ctx context.Context, userID uuid.UUID) (int, error)
	HasEvidence(ctx context.Context, userID uuid.UUID) (bool, error)
	LastEvidenceAt(ctx context.Context, userID uuid.UUID) (time.Time, bool, error)
	HasLifeFocus(ctx context.Context, userID uuid.UUID, areas ...string) (bool, error)
}

// schedules is the person's configured cadences.
type schedules interface {
	PhotoSchedule(ctx context.Context, userID uuid.UUID) (notifications.Schedule, error)
}

// trainingSource answers whether today is a plan day.
type trainingSource interface {
	DueToday(ctx context.Context, user users.User, today time.Time) (title, href string, due bool, err error)
}

type Service struct {
	repo      *Repository
	accounts  accounts
	checkins  checkinDays
	goals     activeGoals
	prefs     notificationPrefs
	fanout    fanout
	push      pusher
	funnel    funnel
	week      weekSource
	training  trainingSource
	schedules schedules
	now       func() time.Time
}

func NewService(repo *Repository, accounts accounts, checkins checkinDays, goals activeGoals) *Service {
	return &Service{
		repo:     repo,
		accounts: accounts,
		checkins: checkins,
		goals:    goals,
		now:      time.Now,
	}
}

// WithClock fixes now so tests can cross a local midnight without waiting.
func (s *Service) WithClock(now func() time.Time) *Service {
	s.now = now
	return s
}

// WithPrefs lets each account switch these nudges off, or ask for silence at
// certain hours. A builder method rather than a constructor argument because
// the rules work without it and every existing caller should keep compiling.
func (s *Service) WithPrefs(p notificationPrefs) *Service {
	s.prefs = p
	return s
}

func (s *Service) WithFanout(f fanout) *Service {
	s.fanout = f
	return s
}

// WithPush sends new nudges to the person's subscribed browsers as well.
func (s *Service) WithPush(p pusher) *Service {
	s.push = p
	return s
}

// WithFunnel reports deliveries and opens to the product funnel.
func (s *Service) WithFunnel(f funnel) *Service {
	s.funnel = f
	return s
}

func (s *Service) WithWeek(w weekSource) *Service {
	s.week = w
	return s
}

func (s *Service) WithTraining(t trainingSource) *Service {
	s.training = t
	return s
}

func (s *Service) WithSchedules(sc schedules) *Service {
	s.schedules = sc
	return s
}

// Raise inserts a nudge if the dedupe key is new, the person allowed this
// kind, and it is not quiet hours. A successful insert is fanned out to
// Telegram and to subscribed browsers, unless the kind is one they already
// received there.
func (s *Service) Raise(ctx context.Context, user users.User, d Draft) (Nudge, bool, error) {
	prefs, err := s.prefsFor(ctx, user)
	if err != nil {
		return Nudge{}, false, err
	}
	if !prefs.AllowsNudge(d.Kind) {
		return Nudge{}, false, nil
	}
	if prefs.InQuietHours(s.now().In(user.Location())) {
		return Nudge{}, false, nil
	}

	n, created, err := s.CreateIfAbsent(ctx, user.ID, d)
	if err != nil || !created {
		return n, created, err
	}

	s.deliver(ctx, user, n)
	return n, true, nil
}

// deliver takes a nudge that is already stored out to every channel that will
// carry it. Nothing here can fail the Raise: the note is in the bell, and a
// channel that did not work is logged and counted as not delivered.
func (s *Service) deliver(ctx context.Context, user users.User, n Nudge) {
	// The bell is the one channel that always has it, by virtue of the insert.
	s.delivered(ctx, user.ID, n.Kind, analytics.ChannelBell)

	if !fansOut(n.Kind) {
		return
	}

	if s.fanout != nil {
		text := n.Title
		if n.Body != "" {
			text = n.Title + "\n\n" + n.Body
		}
		if notifyErr := s.fanout.Notify(ctx, user.ID, text); notifyErr != nil {
			slog.Default().Warn("nudges: could not send to linked chat",
				"error", notifyErr,
				"user_id", user.ID,
				"kind", n.Kind)
		}
	}

	if s.push != nil {
		// The link goes through /open so the click is attributed to push
		// before the person lands on the page the nudge is about.
		count, pushErr := s.push.Send(ctx, user.ID, n.Title, n.Body, OpenPath(n.ID, analytics.ChannelPush))
		if pushErr != nil {
			slog.Default().Warn("nudges: could not send to browsers",
				"error", pushErr,
				"user_id", user.ID,
				"kind", n.Kind)
		}
		if count > 0 {
			s.delivered(ctx, user.ID, n.Kind, analytics.ChannelPush)
		}
	}
}

func (s *Service) delivered(ctx context.Context, userID uuid.UUID, kind, channel string) {
	if s.funnel != nil {
		s.funnel.NudgeDelivered(ctx, userID, kind, channel)
	}
}

// OpenPath is the link a channel outside the app hands a person: it marks the
// nudge read, records which channel brought them, and sends them on to the
// page the nudge is about.
func OpenPath(id uuid.UUID, channel string) string {
	return "/app/nudges/" + id.String() + "/open?from=" + url.QueryEscape(channel)
}

// Open marks a nudge read and records the channel that brought the person to
// it. It is MarkRead with attribution; the bell's own read button uses it too,
// so every open is counted on the same footing.
func (s *Service) Open(ctx context.Context, id, userID uuid.UUID, channel string) (Nudge, error) {
	n, err := s.repo.MarkRead(ctx, id, userID)
	if err != nil {
		return Nudge{}, err
	}
	if s.funnel != nil {
		s.funnel.NudgeOpened(ctx, userID, n.Kind, channel)
	}
	return n, nil
}

// Note is Raise when the caller only has a user id. Used by jobs.
func (s *Service) Note(ctx context.Context, userID uuid.UUID, kind, dedupe, title, body, href string) error {
	d := Draft{Kind: kind, DedupeKey: dedupe, Title: title, Body: body, Href: href}
	if s.accounts == nil {
		_, _, err := s.CreateIfAbsent(ctx, userID, d)
		return err
	}
	user, err := s.accounts.ByID(ctx, userID)
	if err != nil {
		return err
	}
	_, _, err = s.Raise(ctx, user, d)
	return err
}

// RaiseFromUser satisfies coach.Inbox.
func (s *Service) RaiseFromUser(ctx context.Context, user users.User, kind, dedupe, title, body, href string) error {
	_, _, err := s.Raise(ctx, user, Draft{Kind: kind, DedupeKey: dedupe, Title: title, Body: body, Href: href})
	return err
}

func fansOut(kind string) bool {
	switch kind {
	case KindCoachReply, KindBriefingReady:
		return false
	default:
		return true
	}
}

// CreateIfAbsent stores the draft when the dedupe key is new.
// created is false when the same (user, kind, key) already exists.
func (s *Service) CreateIfAbsent(ctx context.Context, userID uuid.UUID, d Draft) (Nudge, bool, error) {
	return s.repo.Insert(ctx, userID, d)
}

func (s *Service) ListOpen(ctx context.Context, userID uuid.UUID, limit int) ([]Nudge, error) {
	if limit <= 0 || limit > 100 {
		limit = listDefault
	}
	return s.repo.ListOpen(ctx, userID, limit)
}

func (s *Service) CountUnread(ctx context.Context, userID uuid.UUID) (int, error) {
	return s.repo.CountUnread(ctx, userID)
}

func (s *Service) MarkRead(ctx context.Context, id, userID uuid.UUID) (Nudge, error) {
	return s.repo.MarkRead(ctx, id, userID)
}

func (s *Service) Dismiss(ctx context.Context, id, userID uuid.UUID) (Nudge, error) {
	return s.repo.Dismiss(ctx, id, userID)
}

func (s *Service) ListOnboarded(ctx context.Context, after uuid.UUID, limit int) ([]users.User, error) {
	if s.accounts == nil {
		return nil, nil
	}
	return s.accounts.ListOnboarded(ctx, after, limit)
}

// Evaluate applies the v1 rules and inserts any new nudges, within whatever
// this person has agreed to hear.
func (s *Service) Evaluate(ctx context.Context, user users.User) (int, error) {
	if user.NeedsOnboarding() {
		return 0, nil
	}

	now := s.now()
	today := localDate(user, now)

	prefs, err := s.prefsFor(ctx, user)
	if err != nil {
		return 0, err
	}

	// Quiet hours defer rather than suppress: both dedupe keys below are per
	// local day, so whatever would have been raised now is raised unchanged by
	// the next sweep after the window closes.
	if prefs.InQuietHours(now.In(user.Location())) {
		return 0, nil
	}

	created := 0

	// First-week notes are the point of a new account. Missed-check-in and
	// deadlines stay silent for a couple of days so a brand-new person is
	// not told they are already behind.
	n, err := s.evalFirstWeek(ctx, user, today)
	if err != nil {
		return created, err
	}
	created += n

	n, err = s.evalWorkoutToday(ctx, user, today)
	if err != nil {
		return created, err
	}
	created += n

	n, err = s.evalPhotoSchedule(ctx, user, today)
	if err != nil {
		return created, err
	}
	created += n

	if daysBetween(onboardedLocal(user, today), today) < firstWeekDays {
		return created, nil
	}

	// The evening before a streak breaks, then the days after it did. The
	// two share a switch on the settings page, so the same permission gates
	// both.
	if prefs.AllowsNudge(KindStreakAtRisk) {
		n, err := s.evalStreakAtRisk(ctx, user, today, now)
		if err != nil {
			return created, err
		}
		created += n
	}

	if prefs.AllowsNudge(KindMissedCheckIn) {
		n, err := s.evalMissedCheckIn(ctx, user, today)
		if err != nil {
			return created, err
		}
		created += n
	}

	if prefs.AllowsNudge(KindGoalDeadline) {
		n, err := s.evalGoalDeadlines(ctx, user, today)
		if err != nil {
			return created, err
		}
		created += n
	}

	return created, nil
}

// prefsFor reads this account's notification settings, or the defaults when no
// preferences source is wired.
func (s *Service) prefsFor(ctx context.Context, user users.User) (notifications.Prefs, error) {
	if s.prefs == nil {
		return notifications.Defaults(), nil
	}
	return s.prefs.Get(ctx, user.ID)
}

// evalStreakAtRisk warns, once, on the evening a check-in streak would break.
//
// This is the gap the missed-check-in rule leaves: that one waits two quiet
// days and so always arrives after the streak is already gone. Loss is a
// stronger motive than absence, and it only works before the loss.
func (s *Service) evalStreakAtRisk(ctx context.Context, user users.User, today, now time.Time) (int, error) {
	if s.checkins == nil {
		return 0, nil
	}
	if now.In(user.Location()).Hour() < streakAtRiskHour {
		return 0, nil
	}

	last, ok, err := s.checkins.LatestLocalDate(ctx, user.ID)
	if err != nil {
		return 0, err
	}
	if ok && daysBetween(last, today) == 0 {
		// Already checked in today. Nothing is at risk.
		return 0, nil
	}

	streak, err := s.checkins.StreakAt(ctx, user, now)
	if err != nil {
		return 0, err
	}
	if streak < streakAtRiskMin {
		return 0, nil
	}

	_, inserted, err := s.Raise(ctx, user, Draft{
		Kind:      KindStreakAtRisk,
		DedupeKey: today.Format("2006-01-02"),
		Title:     fmt.Sprintf("Your %d-day streak ends at midnight", streak),
		Body:      "One check-in keeps it. Thirty seconds.",
		Href:      "/app/check-ins",
	})
	if err != nil {
		return 0, err
	}
	if inserted {
		return 1, nil
	}
	return 0, nil
}

func (s *Service) evalMissedCheckIn(ctx context.Context, user users.User, today time.Time) (int, error) {
	if s.checkins == nil {
		return 0, nil
	}

	last, ok, err := s.checkins.LatestLocalDate(ctx, user.ID)
	if err != nil {
		return 0, err
	}

	var body string
	if !ok {
		body = "You have not checked in since joining."
	} else {
		quiet := daysBetween(last, today)
		if quiet <= missedCheckInDays {
			return 0, nil
		}
		body = fmt.Sprintf("It has been %d days since your last check-in.", quiet)
	}

	_, inserted, err := s.Raise(ctx, user, Draft{
		Kind:      KindMissedCheckIn,
		DedupeKey: today.Format("2006-01-02"),
		Title:     "Check in with yourself",
		Body:      body,
		Href:      "/app/check-ins",
	})
	if err != nil {
		return 0, err
	}
	if inserted {
		return 1, nil
	}
	return 0, nil
}

func (s *Service) evalGoalDeadlines(ctx context.Context, user users.User, today time.Time) (int, error) {
	if s.goals == nil {
		return 0, nil
	}

	list, err := s.goals.ListActive(ctx, user.ID)
	if err != nil {
		return 0, err
	}

	created := 0
	for _, g := range list {
		if g.TargetDate.IsZero() {
			continue
		}
		due := calendarDay(g.TargetDate)
		until := daysBetween(today, due)
		if until < 0 || until > goalDeadlineWindow {
			continue
		}

		body := "Due today."
		if until > 0 {
			body = fmt.Sprintf("Due in %d days.", until)
		}

		_, inserted, err := s.Raise(ctx, user, Draft{
			Kind:      KindGoalDeadline,
			DedupeKey: g.ID.String() + ":" + due.Format("2006-01-02"),
			Title:     fmt.Sprintf("“%s” is due %s", g.Title, due.Format("Monday")),
			Body:      body,
			Href:      "/app/goals/" + g.ID.String(),
		})
		if err != nil {
			return created, err
		}
		if inserted {
			created++
		}
	}
	return created, nil
}

func localDate(user users.User, at time.Time) time.Time {
	loc := user.Location()
	t := at.In(loc)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
}

func calendarDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func onboardedLocal(user users.User, today time.Time) time.Time {
	if user.OnboardedAt == nil {
		return today
	}
	return localDate(user, *user.OnboardedAt)
}

// daysBetween counts calendar days from a to b, ignoring clock time.
func daysBetween(a, b time.Time) int {
	aa := calendarDay(a)
	bb := calendarDay(b)
	return int(bb.Sub(aa).Hours() / 24)
}
