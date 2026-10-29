package nudges

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/analytics"
	"github.com/NorthAIProject/north-client/internal/checkins/checkin"
	"github.com/NorthAIProject/north-client/internal/decisions/decision"
	"github.com/NorthAIProject/north-client/internal/goals"
	"github.com/NorthAIProject/north-client/internal/meals"
	"github.com/NorthAIProject/north-client/internal/notifications"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/users"
)

const (
	listDefault = 20

	// Quiet for two full local days: a check-in on day D is first eligible
	// on D+3. After that first note the schedule in missedCheckInDue applies.
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
	// streakAtRiskEnd is the hour it stops. Loss-framed copy is not sent late
	// at night (docs/advanced-gamification.md); after nine, a gentler ask or
	// nothing.
	streakAtRiskEnd = 21

	// The daily reminder is learned from when the person actually checks in:
	// the median time of day of up to usualTimeSamples recent check-ins that
	// were saved on their own day, needing usualTimeMin of them.
	usualTimeSamples = 28
	usualTimeMin     = 5
	// usualTimeDelay is how long after the usual time it asks: late enough
	// that a person running a little behind has not been nagged.
	usualTimeDelay = 30 * time.Minute
	// usualTimeWindow is how long the ask may still go out after that. The
	// sweep is hourly, so it lands 30 to 90 minutes after the usual time.
	usualTimeWindow = 2 * time.Hour
	// usualTimeMaxGap is the longest silence, in local days since the last
	// check-in, it reminds through. Past it the missed-check-in note speaks.
	usualTimeMaxGap = 2
)

// checkInAsks are the nudges that ask for today's check-in. They share one
// slot a day: whichever goes first, the others stand down.
var checkInAsks = []string{KindUsualTime, KindStreakAtRisk, KindCrewCheckIn, KindEveningReflection}

type accounts interface {
	ByID(ctx context.Context, id uuid.UUID) (users.User, error)
	ListOnboarded(ctx context.Context, after uuid.UUID, limit int) ([]users.User, error)
}

type checkinDays interface {
	LatestLocalDate(ctx context.Context, userID uuid.UUID) (date time.Time, ok bool, err error)
	// StreakAt is the check-in streak as of now, with the same one-day grace
	// the check-in page shows: yesterday still counts until today is missed.
	StreakAt(ctx context.Context, user users.User, now time.Time) (int, error)
	// RunAt is the run behind the streak, with the rest days held.
	RunAt(ctx context.Context, user users.User, now time.Time) (checkin.Run, error)
	// List is the most recent check-ins, newest first, for when they were
	// saved.
	List(ctx context.Context, userID uuid.UUID, limit int) ([]checkin.CheckIn, error)
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

type taggedFanout interface {
	NotifyTagged(ctx context.Context, userID uuid.UUID, kind, dedupeKey string, text string) error
	ResolveTagged(ctx context.Context, userID uuid.UUID, kind, dedupeKey, updatedText string) error
}

// pusher delivers a newly created nudge to the browsers this person subscribed
// (internal/push). Optional. It reports how many accepted the message so
// delivery is counted only where something actually arrived.
type pusher interface {
	Send(ctx context.Context, userID uuid.UUID, title, body, href, category string) (delivered int, err error)
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

// mealSource answers which meal reminders a person set, and whether they
// logged food since a moment.
type mealSource interface {
	MealReminders(ctx context.Context, userID uuid.UUID) ([]meals.Reminder, error)
	LoggedFoodSince(ctx context.Context, userID uuid.UUID, since time.Time) (bool, error)
}

type Service struct {
	repo      Repository
	accounts  accounts
	checkins  checkinDays
	goals     activeGoals
	prefs     notificationPrefs
	fanout    fanout
	push      []pusher
	funnel    funnel
	week      weekSource
	training  trainingSource
	schedules schedules
	meals     mealSource
	crews     crewSource
	weekly    weeklySource
	decisions decisionSource
	meds      medicationSource
	progress  progressSource
	firstWeek firstWeekSource
	now       func() time.Time
}

// Repository is the persistence Service needs. Service names it rather than
// the Postgres type so the database stays a detail of the composition root:
// NewRepository is what the process wires, and a test can hand Service
// anything that keeps nudges.
type Repository interface {
	Insert(ctx context.Context, userID uuid.UUID, d Draft) (Nudge, bool, error)
	ListOpen(ctx context.Context, userID uuid.UUID, limit int) ([]Nudge, error)
	CountUnread(ctx context.Context, userID uuid.UUID) (int, error)
	MarkRead(ctx context.Context, id, userID uuid.UUID) (Nudge, error)
	Dismiss(ctx context.Context, id, userID uuid.UUID) (Nudge, error)
	ResolveForDay(ctx context.Context, userID uuid.UUID, kind, day string) ([]Nudge, error)
	CountForDay(ctx context.Context, userID uuid.UUID, kinds []string, day string) (int, error)
}

// crewSource names the crewmates who checked in today. crews.Service
// satisfies it.
type crewSource interface {
	CheckedInCrewmates(ctx context.Context, user users.User, now time.Time) ([]string, error)
}

type decisionSource interface {
	DueRevisit(ctx context.Context, userID uuid.UUID, now time.Time) (decision.Revisit, bool, error)
}

// WithDecisions turns on decision revisits.
func (s *Service) WithDecisions(d decisionSource) *Service {
	s.decisions = d
	return s
}

type weeklySource interface {
	Reviewed(ctx context.Context, user users.User, monday time.Time) (bool, error)
}

// WithWeekly turns on the Sunday-evening weekly review.
func (s *Service) WithWeekly(w weeklySource) *Service {
	s.weekly = w
	return s
}

// WithCrews turns on the evening crew note.
func (s *Service) WithCrews(c crewSource) *Service {
	s.crews = c
	return s
}

func NewService(repo Repository, accounts accounts, checkins checkinDays, goals activeGoals) *Service {
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

// WithPush sends new nudges through a push channel as well: browsers over
// Web Push, the iOS app over APNs. Each call adds one channel.
func (s *Service) WithPush(p pusher) *Service {
	s.push = append(s.push, p)
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

func (s *Service) WithMeals(m mealSource) *Service {
	s.meals = m
	return s
}

// WithFirstWeek hands the first-week notes to a running programme: its
// evening note replaces the three fixed ones while it runs.
func (s *Service) WithFirstWeek(f firstWeekSource) *Service {
	s.firstWeek = f
	return s
}

// WithMedications turns on medication reminders.
func (s *Service) WithMedications(m medicationSource) *Service {
	s.meds = m
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
	if !d.Alarm && prefs.InQuietHours(s.now().In(user.Location())) {
		return Nudge{}, false, nil
	}

	n, created, err := s.CreateIfAbsent(ctx, user.ID, d)
	if err != nil || !created {
		return n, created, err
	}

	telegram := fansOut(n.Kind) || d.Everywhere
	s.deliver(ctx, user, n, telegram, telegram || d.Push)
	return n, true, nil
}

// deliver takes a nudge that is already stored out to every channel that will
// carry it. Nothing here can fail the Raise: the note is in the bell, and a
// channel that did not work is logged and counted as not delivered.
func (s *Service) deliver(ctx context.Context, user users.User, n Nudge, telegram, push bool) {
	// The bell is the one channel that always has it, by virtue of the insert.
	s.delivered(ctx, user.ID, n.Kind, analytics.ChannelBell)

	if telegram && s.fanout != nil {
		text := n.Title
		if n.Body != "" {
			text = n.Title + "\n\n" + n.Body
		}
		var notifyErr error
		if tf, ok := s.fanout.(taggedFanout); ok {
			notifyErr = tf.NotifyTagged(ctx, user.ID, n.Kind, n.DedupeKey, text)
		} else {
			notifyErr = s.fanout.Notify(ctx, user.ID, text)
		}
		if notifyErr != nil {
			slog.Default().Warn("nudges: could not send to linked chat",
				"error", notifyErr,
				"user_id", user.ID,
				"kind", n.Kind)
		}
	}

	if push && len(s.push) > 0 {
		// The link goes through /open so the click is attributed to push
		// before the person lands on the page the nudge is about.
		href := OpenPath(n.ID, analytics.ChannelPush)
		total := 0
		for _, p := range s.push {
			count, pushErr := p.Send(ctx, user.ID, n.Title, n.Body, href, PushCategory(n.Kind))
			if pushErr != nil {
				slog.Default().Warn("nudges: could not send push",
					"error", pushErr,
					"user_id", user.ID,
					"kind", n.Kind)
			}
			total += count
		}
		if total > 0 {
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
	return s.note(ctx, userID, Draft{Kind: kind, DedupeKey: dedupe, Title: title, Body: body, Href: href})
}

// NoteWithPush is Note that also reaches the lock screen, for a message whose
// body its caller already sent to Telegram. The daily briefing is the case:
// its full text goes to the linked chat directly, so the nudge must not repeat
// it there, but a person with only the installed PWA would otherwise never
// hear that it arrived. The same switch and quiet hours as every other nudge
// decide whether it is raised at all.
func (s *Service) NoteWithPush(ctx context.Context, userID uuid.UUID, kind, dedupe, title, body, href string) error {
	return s.note(ctx, userID, Draft{Kind: kind, DedupeKey: dedupe, Title: title, Body: body, Href: href, Push: true})
}

func (s *Service) note(ctx context.Context, userID uuid.UUID, d Draft) error {
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

// RaiseProactive is RaiseFromUser for a message nobody has seen anywhere yet.
//
// coach_reply normally stays in the bell, because the reply it points at was
// already delivered where it was asked for (Telegram). A standing task's
// result was asked for by nobody, so it goes out on every channel — bell,
// Telegram and Web Push — under the same kind and the same switch.
func (s *Service) RaiseProactive(ctx context.Context, user users.User, kind, dedupe, title, body, href string) error {
	_, _, err := s.Raise(ctx, user, Draft{Kind: kind, DedupeKey: dedupe, Title: title, Body: body, Href: href, Everywhere: true})
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

// ResolveToday closes the nudges of kind raised for the person's local today
// and rewrites the chat message that carried each one, using text(title, body)
// of that nudge.
//
// "Today" is the person's date, not the server's: the sweep keys nudges by
// local date, and a UTC key misses every evening west of Greenwich. A nudge
// already dismissed from the bell is still resolved in the chat — dismissing
// the bell does not unsend the message that keeps asking.
func (s *Service) ResolveToday(ctx context.Context, userID uuid.UUID, kind string, text func(title, body string) string) error {
	if s.accounts == nil {
		return nil
	}
	user, err := s.accounts.ByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("resolve %s nudges: load user: %w", kind, err)
	}
	today := localDate(user, s.now())

	if kind == KindWorkoutToday {
		done, known, doneErr := s.trainedToday(ctx, user, today)
		if doneErr != nil {
			return fmt.Errorf("resolve %s nudges: %w", kind, doneErr)
		}
		if known && !done {
			return nil
		}
	}

	resolved, err := s.repo.ResolveForDay(ctx, userID, kind, today.Format("2006-01-02"))
	if err != nil {
		return err
	}

	tf, ok := s.fanout.(taggedFanout)
	if !ok {
		return nil
	}
	var errs []error
	for _, n := range resolved {
		if err := tf.ResolveTagged(ctx, userID, n.Kind, n.DedupeKey, text(n.Title, n.Body)); err != nil {
			errs = append(errs, fmt.Errorf("resolve %s %s in chat: %w", n.Kind, n.DedupeKey, err))
		}
	}
	return errors.Join(errs...)
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

	// Medication reminders come before the quiet-hours return below: they
	// are alarms the person set for a time, and a 22:30 dose deferred to the
	// morning is a missed dose, not a polite one.
	created, err := s.evalMedicationReminders(ctx, user, today, now)
	if err != nil {
		return created, err
	}

	// Quiet hours defer rather than suppress: both dedupe keys below are per
	// local day, so whatever would have been raised now is raised unchanged by
	// the next sweep after the window closes.
	if prefs.InQuietHours(now.In(user.Location())) {
		return created, nil
	}

	// First-week notes are the point of a new account. Missed-check-in and
	// deadlines stay silent for a couple of days so a brand-new person is
	// not told they are already behind.
	n, err := s.evalFirstWeek(ctx, user, prefs, today, now)
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

	n, err = s.evalProgressCheck(ctx, user, now)
	if err != nil {
		return created, err
	}
	created += n

	// Reminders the person set themselves, so no first-week silence.
	n, err = s.evalMealReminders(ctx, user, today, now)
	if err != nil {
		return created, err
	}
	created += n

	if daysBetween(onboardedLocal(user, today), today) < firstWeekDays {
		return created, nil
	}

	// The daily reminder goes first: of the asks for today's check-in, which
	// share one slot a day, it is the gentle one.
	if prefs.AllowsNudge(KindUsualTime) {
		n, err := s.evalUsualTime(ctx, user, today, now)
		if err != nil {
			return created, err
		}
		created += n
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

	if prefs.AllowsNudge(KindCrewCheckIn) {
		n, err := s.evalCrewCheckIn(ctx, user, today, now)
		if err != nil {
			return created, err
		}
		created += n
	}

	if prefs.AllowsNudge(KindDecisionRevisit) {
		n, err := s.evalDecisionRevisit(ctx, user, now)
		if err != nil {
			return created, err
		}
		created += n
	}

	if prefs.AllowsNudge(KindWeekReview) {
		n, err := s.evalWeekReview(ctx, user, prefs, today, now)
		if err != nil {
			return created, err
		}
		created += n
	}

	if prefs.AllowsNudge(KindEveningReflection) {
		n, err := s.evalEveningReflection(ctx, user, prefs, today, now)
		if err != nil {
			return created, err
		}
		created += n
	}

	return created, nil
}

// eveningReflectionWindow is how long after the chosen hour the reflection may
// still go out. A sweep that missed the hour catches up within it; one that
// finds the window closed (quiet hours ran past it, the worker was down)
// leaves the day alone rather than asking about it the next morning.
const eveningReflectionWindow = 3

// evalEveningReflection asks, once an evening, how the day went: a check-in if
// there is none yet today, otherwise a line in the journal.
//
// Asking for the check-in, it stays out of the way of any ask already made
// today — the reminder, the streak warning, the crew note — because two
// notifications about one missing check-in is one too many. Pointing at the
// journal after a check-in is not an ask, and the person opted into it.
func (s *Service) evalEveningReflection(ctx context.Context, user users.User, prefs notifications.Prefs, today, now time.Time) (int, error) {
	hour := now.In(user.Location()).Hour()
	if hour < prefs.EveningHour || hour >= prefs.EveningHour+eveningReflectionWindow {
		return 0, nil
	}
	// On Sunday the weekly review asks about the whole week, which covers the
	// day; one evening note is enough.
	if s.weeklyReviewOn(prefs) && now.In(user.Location()).Weekday() == time.Sunday {
		return 0, nil
	}

	checkedIn := false
	if s.checkins != nil {
		last, ok, err := s.checkins.LatestLocalDate(ctx, user.ID)
		if err != nil {
			return 0, err
		}
		checkedIn = ok && daysBetween(last, today) == 0
		if !checkedIn {
			asked, askErr := s.askedForCheckInToday(ctx, user, today)
			if askErr != nil || asked {
				return 0, askErr
			}
		}
	}

	draft := Draft{
		Kind:      KindEveningReflection,
		DedupeKey: today.Format("2006-01-02"),
		Title:     text(user, "nudges.evening.title"),
		Body:      text(user, "nudges.evening.body"),
		Href:      "/app/check-ins",
	}
	if checkedIn {
		draft.Body = text(user, "nudges.evening.body.journal")
		draft.Href = "/app/mind"
	}

	_, inserted, err := s.Raise(ctx, user, draft)
	if err != nil {
		return 0, err
	}
	if inserted {
		return 1, nil
	}
	return 0, nil
}

// decisionRevisitHour is the local hour revisits are asked in. One hour a
// day keeps it to one question a day at most, even with several due; the
// rest wait for the next morning.
const decisionRevisitHour = 10

// evalDecisionRevisit asks whether a decision held, 30 and 90 days after it
// was made, once per mark.
func (s *Service) evalDecisionRevisit(ctx context.Context, user users.User, now time.Time) (int, error) {
	if s.decisions == nil || now.In(user.Location()).Hour() != decisionRevisitHour {
		return 0, nil
	}
	due, ok, err := s.decisions.DueRevisit(ctx, user.ID, now)
	if err != nil || !ok {
		return 0, err
	}
	body := "nudges.decision.body.month"
	if due.Mark >= 90 {
		body = "nudges.decision.body.quarter"
	}
	_, inserted, err := s.Raise(ctx, user, Draft{
		Kind:      KindDecisionRevisit,
		DedupeKey: fmt.Sprintf("decision:%s:%d", due.ID, due.Mark),
		Title:     text(user, "nudges.decision.title"),
		Body:      text(user, body, due.Title),
		Href:      "/app/decisions/" + due.ID.String(),
	})
	if err != nil || !inserted {
		return 0, err
	}
	return 1, nil
}

func (s *Service) weeklyReviewOn(prefs notifications.Prefs) bool {
	return s.weekly != nil && prefs.AllowsNudge(KindWeekReview)
}

// evalWeekReview asks on Sunday evening, at the person's evening hour, for
// the weekly review of the week ending, unless next week already has a focus.
// It uses the evening reflection's window: a sweep that misses the hour
// catches up within it, and Monday morning is too late to plan Monday.
func (s *Service) evalWeekReview(ctx context.Context, user users.User, prefs notifications.Prefs, today, now time.Time) (int, error) {
	if s.weekly == nil {
		return 0, nil
	}
	local := now.In(user.Location())
	if local.Weekday() != time.Sunday || local.Hour() < prefs.EveningHour || local.Hour() >= prefs.EveningHour+eveningReflectionWindow {
		return 0, nil
	}
	monday := today.AddDate(0, 0, 1)
	reviewed, err := s.weekly.Reviewed(ctx, user, monday)
	if err != nil || reviewed {
		return 0, err
	}
	_, inserted, err := s.Raise(ctx, user, Draft{
		Kind:      KindWeekReview,
		DedupeKey: monday.Format("2006-01-02"),
		Title:     text(user, "nudges.week.title"),
		Body:      text(user, "nudges.week.body"),
		Href:      "/app/weekly",
	})
	if err != nil || !inserted {
		return 0, err
	}
	return 1, nil
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
//
// It is the one loss-framed message the product sends, so it is held to the
// evening before nine, stands down for any other ask already made today,
// waits for the daily reminder when that is still due, and is not sent at
// all while a rest day is held: a person with one in hand loses nothing at
// midnight.
func (s *Service) evalStreakAtRisk(ctx context.Context, user users.User, today, now time.Time) (int, error) {
	if s.checkins == nil {
		return 0, nil
	}
	if hour := now.In(user.Location()).Hour(); hour < streakAtRiskHour || hour >= streakAtRiskEnd {
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

	run, err := s.checkins.RunAt(ctx, user, now)
	if err != nil {
		return 0, err
	}
	streak := run.Current
	if streak < streakAtRiskMin || run.RestsHeld > 0 {
		return 0, nil
	}
	if asked, askErr := s.askedForCheckInToday(ctx, user, today); askErr != nil || asked {
		return 0, askErr
	}
	if pending, dueErr := s.usualAskStillDue(ctx, user, today, now); dueErr != nil || pending {
		return 0, dueErr
	}

	_, inserted, err := s.Raise(ctx, user, Draft{
		Kind:      KindStreakAtRisk,
		DedupeKey: today.Format("2006-01-02"),
		Title:     text(user, "nudges.streak.title", streak),
		Body:      text(user, "nudges.streak.body"),
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

	// Nobody who has never checked in has a last one to count from, so the
	// day they joined stands in: the schedule below then runs from there.
	var body string
	var quiet int
	if ok {
		quiet = daysBetween(last, today)
		body = text(user, "nudges.missed.body.days", quiet)
	} else {
		quiet = daysBetween(onboardedLocal(user, today), today)
		body = text(user, "nudges.missed.body.never")
	}
	if !missedCheckInDue(quiet) {
		return 0, nil
	}

	_, inserted, err := s.Raise(ctx, user, Draft{
		Kind:      KindMissedCheckIn,
		DedupeKey: today.Format("2006-01-02"),
		Title:     text(user, "nudges.missed.title"),
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

// missedCheckInDue reports whether a silence of this many local days is one
// to mention: the third day, the seventh, the fourteenth, then every seventh.
//
// Not daily. Production sent one person the same note sixteen mornings in a
// row, the number in it going up by one each time, and by the third morning a
// note like that is furniture. Absence does not become more true overnight;
// the days it is worth saying are the ones where the silence has changed
// shape — a slipped habit, a lost week, a lost fortnight.
//
// A day the sweep misses altogether (the worker down all day) skips to the
// next date rather than catching up. Dedupe is already per local day, so this
// only decides which days are candidates.
func missedCheckInDue(quiet int) bool {
	switch {
	case quiet <= missedCheckInDays:
		return false
	case quiet == missedCheckInDays+1, quiet == 7:
		return true
	case quiet >= 14:
		return (quiet-14)%7 == 0
	default:
		return false
	}
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

		body := text(user, "nudges.goal.body.today")
		switch {
		case until == 1:
			body = text(user, "nudges.goal.body.tomorrow")
		case until > 1:
			body = text(user, "nudges.goal.body.days", until)
		}

		_, inserted, err := s.Raise(ctx, user, Draft{
			Kind:      KindGoalDeadline,
			DedupeKey: g.ID.String() + ":" + due.Format("2006-01-02"),
			Title:     text(user, "nudges.goal.title", g.Title, weekday(user, due)),
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

// evalCrewCheckIn tells somebody, once an evening, that people in their crew
// checked in today and they have not. It stands aside for any ask already
// made today: the reminder in the morning, or the streak warning that ran
// before it in this sweep.
func (s *Service) evalCrewCheckIn(ctx context.Context, user users.User, today, now time.Time) (int, error) {
	if s.crews == nil || s.checkins == nil || now.In(user.Location()).Hour() < streakAtRiskHour {
		return 0, nil
	}
	last, ok, err := s.checkins.LatestLocalDate(ctx, user.ID)
	if err != nil {
		return 0, err
	}
	if ok && daysBetween(last, today) == 0 {
		return 0, nil
	}
	// One ask a day: the streak warning runs first in a sweep, so when it
	// went out this stands down.
	if asked, askErr := s.askedForCheckInToday(ctx, user, today); askErr != nil || asked {
		return 0, askErr
	}
	names, err := s.crews.CheckedInCrewmates(ctx, user, now)
	if err != nil || len(names) == 0 {
		return 0, err
	}
	_, inserted, err := s.Raise(ctx, user, Draft{
		Kind:      KindCrewCheckIn,
		DedupeKey: today.Format("2006-01-02"),
		Title:     crewTitle(user, names),
		Body:      text(user, "nudges.crew.body"),
		Href:      "/app/check-ins",
	})
	if err != nil || !inserted {
		return 0, err
	}
	return 1, nil
}

// crewTitle names up to two people and counts the rest.
func crewTitle(user users.User, names []string) string {
	switch len(names) {
	case 1:
		return text(user, "nudges.crew.one", names[0])
	case 2:
		return text(user, "nudges.crew.two", names[0], names[1])
	default:
		return text(user, "nudges.crew.many", names[0], names[1], len(names)-2)
	}
}

// text is a nudge's words in the recipient's language.
//
// A nudge is stored with its title and body, so the language is fixed when it
// is raised: the recipient's, read from their account, since a sweep has no
// request to carry one. An account with no locale reads English.
func text(user users.User, key string, args ...any) string {
	s := i18n.Translate(string(user.Locale), key)
	if len(args) == 0 {
		return s
	}
	return fmt.Sprintf(s, args...)
}

// weekday names t's day of the week in the recipient's language.
func weekday(user users.User, t time.Time) string {
	return i18n.FormatTime(string(user.Locale), t, "Monday")
}
