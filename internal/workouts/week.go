package workouts

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/activity/activity"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/users"
	"github.com/NorthAIProject/north-client/internal/workouts/plan"
)

// The plan someone follows, and what each week of it trains.
//
// A plan is a rotation of sessions; a week is a set of weekdays given the next
// sessions of that rotation (see plan/week.go). The week is what everything
// downstream reads — the next session, today's nudge, the coach, adherence —
// so training three days instead of four is a change to one week, not a new
// plan.

// ActivePlan is the newest version of the plan someone follows.
func (s *Service) ActivePlan(ctx context.Context, userID uuid.UUID) (StoredPlan, error) {
	return s.repo.ActivePlan(ctx, userID)
}

// SetActivePlan makes planID's plan the one followed. A week nobody changed
// switches over with it, keeping what was already trained; a week someone set
// by hand stays as they set it.
func (s *Service) SetActivePlan(ctx context.Context, user users.User, planID uuid.UUID) (StoredPlan, error) {
	chosen, err := s.repo.GetPlan(ctx, planID, user.ID)
	if err != nil {
		return StoredPlan{}, err
	}
	if err := s.repo.SetActivePlan(ctx, user.ID, chosen.IntakeID); err != nil {
		return StoredPlan{}, err
	}
	current, err := s.repo.LatestPlanForIntake(ctx, user.ID, chosen.IntakeID)
	if err != nil {
		return StoredPlan{}, err
	}
	if err := s.followInThisWeek(ctx, user, current, time.Now()); err != nil {
		return StoredPlan{}, err
	}
	return current, nil
}

// followInThisWeek rebuilds an untouched current week around active.
func (s *Service) followInThisWeek(ctx context.Context, user users.User, active StoredPlan, now time.Time) error {
	now = now.In(user.Location())
	start := activity.WeekStart(now, user.Location())
	stored, ok, err := s.repo.GetWeek(ctx, user.ID, start)
	if err != nil || !ok || stored.Custom {
		return err
	}
	done, err := s.completedIn(ctx, user, now)
	if err != nil {
		return err
	}
	cursor, err := s.cursor(ctx, user, active, start)
	if err != nil {
		return err
	}
	slots := plan.Rebuild(plan.Change{
		Today: -1, Existing: stored.Slots, Done: done,
		Weekdays: plan.DefaultWeekdays(active.Plan),
		Source:   active.Plan, SourceIntake: active.IntakeID, Cursor: cursor,
	})
	return s.repo.UpsertWeek(ctx, user.ID, StoredWeek{Start: start, Slots: slots})
}

// WeekDay is one training day of a week, resolved to the session it trains.
type WeekDay struct {
	Slot
	Date time.Time

	// PlanID is the version of the plan the session is read from, and
	// PlanName its name.
	PlanID   uuid.UUID
	PlanName string

	// Day is the session as trained that date: Weekday is the slot's, the
	// sets are adjusted for the week's volume, and StartTime is the time the
	// person usually trains on that weekday.
	Day PlanDay

	Completed bool
}

// WeekProgress is a week of training laid over what was done in it.
type WeekProgress struct {
	Start time.Time

	// Custom is a week someone changed rather than the plan's usual week.
	Custom bool

	// Days are the week's training days, Monday first.
	Days []WeekDay

	// Completed are finished weekdays, Monday first. A weekday can be
	// finished without being a training day — an unplanned lift counts for
	// the day it was done.
	Completed []string

	// Next is the first unfinished day from today on, wrapping into next week
	// when everything left this week is done. NextDay carries its date and
	// plan; Next is its session, kept for the many readers that want only that.
	Next    PlanDay
	NextDay WeekDay
	HasNext bool

	// Volume is this week's choice from the weekly review, and every Day
	// already has its sets adjusted for it.
	Volume Volume
}

// Done reports whether weekday was finished this week.
func (w WeekProgress) Done(weekday string) bool {
	for _, d := range w.Completed {
		if strings.EqualFold(d, strings.TrimSpace(weekday)) {
			return true
		}
	}
	return false
}

// DoneToday is today's training day when it is already finished.
func (w WeekProgress) DoneToday(now time.Time) (PlanDay, bool) {
	today := now.Weekday().String()
	for _, d := range w.Days {
		if d.Weekday == today && d.Completed {
			return d.Day, true
		}
	}
	return PlanDay{}, false
}

// Trained reports whether the plan day at index of intake's plan was trained
// this week, on whichever day it was scheduled.
func (w WeekProgress) Trained(intake uuid.UUID, index int) bool {
	for _, d := range w.Days {
		if d.IntakeID == intake && d.DayIndex == index && d.Completed {
			return true
		}
	}
	return false
}

// IsNext reports whether the plan day at index of intake's plan is the one to
// train next.
func (w WeekProgress) IsNext(intake uuid.UUID, index int) bool {
	return w.HasNext && w.NextDay.IntakeID == intake && w.NextDay.DayIndex == index
}

// WeekProgress is the week containing now. The first read of a week records
// it, so the week after knows where the plan's rotation stopped. Without a
// plan the week is empty.
func (s *Service) WeekProgress(ctx context.Context, user users.User, now time.Time) (WeekProgress, error) {
	now = now.In(user.Location())
	start := activity.WeekStart(now, user.Location())
	week, err := s.currentWeek(ctx, user, start)
	if err != nil {
		return WeekProgress{}, err
	}
	done, err := s.completedIn(ctx, user, now)
	if err != nil {
		return WeekProgress{}, err
	}
	out, err := s.resolve(ctx, user, week, done, now)
	if err != nil {
		return WeekProgress{}, err
	}

	next, ok := plan.NextSlot(week.Slots, now, done)
	if ok {
		out.NextDay, out.HasNext = dayFor(out.Days, next.Weekday), true
	} else {
		// Everything left this week is done: the next session is the first of
		// next week.
		following, err := s.NextWeek(ctx, user, now)
		if err != nil {
			return WeekProgress{}, err
		}
		if len(following.Days) > 0 {
			out.NextDay, out.HasNext = following.Days[0], true
		}
	}
	out.Next = out.NextDay.Day
	return out, nil
}

// NextWeek is the week after the one containing now: as someone set it, or
// the plan's usual week resuming where this one leaves off. It is not
// recorded, since this week can still change where it resumes, and it names
// no next session.
func (s *Service) NextWeek(ctx context.Context, user users.User, now time.Time) (WeekProgress, error) {
	now = now.In(user.Location())
	start := activity.WeekStart(now, user.Location()).AddDate(0, 0, 7)
	week, ok, err := s.repo.GetWeek(ctx, user.ID, start)
	if err != nil {
		return WeekProgress{}, err
	}
	if !ok {
		week, err = s.defaultWeek(ctx, user, start)
		if err != nil {
			return WeekProgress{}, err
		}
	}
	week.Start = start
	// No Next: the session to train next is this week's until this week is
	// done, and WeekProgress is what knows that.
	return s.resolve(ctx, user, week, nil, now)
}

// SessionRef names one day of one saved plan.
type SessionRef struct {
	PlanID   uuid.UUID
	DayIndex int
}

// WeekChange is someone choosing what a week trains.
type WeekChange struct {
	// Weekdays are the days to train. Left empty with Days set, the days are
	// suggested; both empty is a rest week.
	Weekdays []string
	Days     int

	// PlanID is the plan whose sessions fill the days; nil is the active plan.
	PlanID *uuid.UUID

	// Assign pins a weekday to a specific session of any saved plan.
	Assign map[string]SessionRef

	// NextWeek changes next week rather than this one.
	NextWeek bool
}

// SetWeek changes this week or next. What was already trained this week
// stays; the rest of the chosen days take the next sessions of the plan.
func (s *Service) SetWeek(ctx context.Context, user users.User, now time.Time, change WeekChange) (WeekProgress, error) {
	now = now.In(user.Location())
	slots, start, err := s.plannedChange(ctx, user, now, change)
	if err != nil {
		return WeekProgress{}, err
	}
	if err := s.repo.UpsertWeek(ctx, user.ID, StoredWeek{Start: start, Slots: slots, Custom: true}); err != nil {
		return WeekProgress{}, err
	}
	if change.NextWeek {
		return s.NextWeek(ctx, user, now)
	}
	return s.WeekProgress(ctx, user, now)
}

// SuggestWeek is what SetWeek would make of change, without saving it: the
// page shows it as soon as someone picks a number of days.
func (s *Service) SuggestWeek(ctx context.Context, user users.User, now time.Time, change WeekChange) (WeekProgress, error) {
	now = now.In(user.Location())
	slots, start, err := s.plannedChange(ctx, user, now, change)
	if err != nil {
		return WeekProgress{}, err
	}
	var done []string
	if !change.NextWeek {
		if done, err = s.completedIn(ctx, user, now); err != nil {
			return WeekProgress{}, err
		}
	}
	return s.resolve(ctx, user, StoredWeek{Start: start, Slots: slots, Custom: true}, done, now)
}

// ResetWeek puts a week back to the plan's usual one. This week keeps what
// was already trained.
func (s *Service) ResetWeek(ctx context.Context, user users.User, now time.Time, nextWeek bool) (WeekProgress, error) {
	now = now.In(user.Location())
	start := activity.WeekStart(now, user.Location())
	if nextWeek {
		if err := s.repo.DeleteWeek(ctx, user.ID, start.AddDate(0, 0, 7)); err != nil {
			return WeekProgress{}, err
		}
		return s.NextWeek(ctx, user, now)
	}

	active, err := s.repo.ActivePlan(ctx, user.ID)
	if err != nil {
		return WeekProgress{}, err
	}
	stored, _, err := s.repo.GetWeek(ctx, user.ID, start)
	if err != nil {
		return WeekProgress{}, err
	}
	// Recorded as untouched before the rebuild, so followInThisWeek treats it
	// as the plan's own week.
	stored.Start, stored.Custom = start, false
	if err := s.repo.UpsertWeek(ctx, user.ID, stored); err != nil {
		return WeekProgress{}, err
	}
	if err := s.followInThisWeek(ctx, user, active, now); err != nil {
		return WeekProgress{}, err
	}
	return s.WeekProgress(ctx, user, now)
}

// plannedChange works out the slots a change makes, and the week they are for.
func (s *Service) plannedChange(ctx context.Context, user users.User, now time.Time, change WeekChange) ([]Slot, time.Time, error) {
	start := activity.WeekStart(now, user.Location())
	today := plan.WeekPosition(now.Weekday().String())
	var existing []Slot
	var done []string
	if change.NextWeek {
		start, today = start.AddDate(0, 0, 7), -1
	} else {
		week, err := s.currentWeek(ctx, user, start)
		if err != nil {
			return nil, time.Time{}, err
		}
		existing = week.Slots
		if done, err = s.completedIn(ctx, user, now); err != nil {
			return nil, time.Time{}, err
		}
	}

	source, err := s.sourcePlan(ctx, user, change.PlanID)
	if err != nil {
		return nil, time.Time{}, err
	}

	if change.Days < 0 || change.Days > 7 {
		return nil, time.Time{}, apperr.Wrap(apperr.ErrValidation, "a week trains between 0 and 7 days")
	}
	weekdays, err := plan.NormalizeWeekdays(change.Weekdays)
	if err != nil {
		return nil, time.Time{}, apperr.Wrap(apperr.ErrValidation, "%v", err)
	}
	if len(weekdays) == 0 && change.Days > 0 {
		weekdays = plan.SuggestWeekdays(change.Days, today, lockedWeekdays(existing, done))
	}

	assign, err := s.pinned(ctx, user, change.Assign, weekdays)
	if err != nil {
		return nil, time.Time{}, err
	}

	cursor, err := s.cursor(ctx, user, source, start)
	if err != nil {
		return nil, time.Time{}, err
	}
	slots := plan.Rebuild(plan.Change{
		Today: today, Existing: existing, Done: done,
		Weekdays: weekdays, Assign: assign,
		Source: source.Plan, SourceIntake: source.IntakeID, Cursor: cursor,
	})
	return slots, start, nil
}

// sourcePlan is the newest version of planID's plan, or the active plan.
func (s *Service) sourcePlan(ctx context.Context, user users.User, planID *uuid.UUID) (StoredPlan, error) {
	if planID == nil {
		return s.repo.ActivePlan(ctx, user.ID)
	}
	named, err := s.repo.GetPlan(ctx, *planID, user.ID)
	if err != nil {
		return StoredPlan{}, err
	}
	return s.repo.LatestPlanForIntake(ctx, user.ID, named.IntakeID)
}

// pinned turns session references into slots, refusing days the plan does not
// have and weekdays the week does not train.
func (s *Service) pinned(ctx context.Context, user users.User, refs map[string]SessionRef, weekdays []string) (map[string]Slot, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	training := make(map[string]bool, len(weekdays))
	for _, wd := range weekdays {
		training[wd] = true
	}
	out := make(map[string]Slot, len(refs))
	for label, ref := range refs {
		wd, ok := plan.CanonicalWeekday(label)
		if !ok || !training[wd] {
			return nil, apperr.Wrap(apperr.ErrValidation, "%q is not one of the days this week trains", label)
		}
		p, err := s.sourcePlan(ctx, user, &ref.PlanID)
		if err != nil {
			return nil, err
		}
		if ref.DayIndex < 0 || ref.DayIndex >= len(p.Plan.Days) {
			return nil, apperr.Wrap(apperr.ErrValidation, "%s has no day %d", p.Plan.Name, ref.DayIndex+1)
		}
		out[wd] = Slot{Weekday: wd, IntakeID: p.IntakeID, DayIndex: ref.DayIndex}
	}
	return out, nil
}

// currentWeek is the recorded week starting on start, recording the plan's
// usual week first when there is none yet.
func (s *Service) currentWeek(ctx context.Context, user users.User, start time.Time) (StoredWeek, error) {
	week, ok, err := s.repo.GetWeek(ctx, user.ID, start)
	if err != nil || ok {
		week.Start = start
		return week, err
	}
	week, err = s.defaultWeek(ctx, user, start)
	if err != nil || len(week.Slots) == 0 {
		return week, err
	}
	if err := s.repo.InsertWeekIfAbsent(ctx, user.ID, start, week.Slots); err != nil {
		return StoredWeek{}, err
	}
	// Re-read: a concurrent first read may have recorded it first.
	week, _, err = s.repo.GetWeek(ctx, user.ID, start)
	week.Start = start
	return week, err
}

// defaultWeek is the active plan's usual week starting on start, resuming its
// rotation. No plan is an empty week.
func (s *Service) defaultWeek(ctx context.Context, user users.User, start time.Time) (StoredWeek, error) {
	active, err := s.repo.ActivePlan(ctx, user.ID)
	if err != nil {
		if apperr.Is(err, apperr.ErrNotFound) {
			return StoredWeek{Start: start}, nil
		}
		return StoredWeek{}, err
	}
	cursor, err := s.cursor(ctx, user, active, start)
	if err != nil {
		return StoredWeek{}, err
	}
	return StoredWeek{Start: start, Slots: plan.DefaultWeek(active.Plan, active.IntakeID, cursor)}, nil
}

// cursor is where p's rotation stands at the start of the week beginning on
// start: past the last of its sessions trained in the latest recorded week
// that had any. A plan never scheduled starts at its beginning.
func (s *Service) cursor(ctx context.Context, user users.User, p StoredPlan, start time.Time) (int, error) {
	prev, ok, err := s.repo.PreviousWeekWithIntake(ctx, user.ID, start, p.IntakeID)
	if err != nil || !ok {
		return 0, err
	}
	// Midday, not midnight, so a daylight-saving change cannot move the
	// instant into the previous week.
	loc := user.Location()
	inWeek := time.Date(prev.Start.Year(), prev.Start.Month(), prev.Start.Day(), 12, 0, 0, 0, loc)
	done, err := s.completedIn(ctx, user, inWeek)
	if err != nil {
		return 0, err
	}
	pos, _ := plan.Resume(p.Plan, p.IntakeID, prev.Slots, done)
	return pos, nil
}

// completedIn lists the weekdays finished in the week containing now. Without
// an activity tracker nothing is finished, which is the calendar-only answer
// the plan gave before sessions were counted.
func (s *Service) completedIn(ctx context.Context, user users.User, now time.Time) ([]string, error) {
	if s.activity == nil {
		return nil, nil
	}
	return s.activity.CompletedWeekdays(ctx, user.ID, user.Location(), now)
}

// resolve reads each slot's session from the newest version of its plan.
// A slot whose plan was deleted, or whose day no longer exists, is left out.
func (s *Service) resolve(ctx context.Context, user users.User, week StoredWeek, done []string, now time.Time) (WeekProgress, error) {
	loc := user.Location()
	start := time.Date(week.Start.Year(), week.Start.Month(), week.Start.Day(), 0, 0, 0, 0, loc)
	volume, err := s.volumeFor(ctx, user, start.Add(12*time.Hour))
	if err != nil {
		return WeekProgress{}, err
	}
	out := WeekProgress{Start: start, Custom: week.Custom, Completed: done, Volume: volume}

	plans := make(map[uuid.UUID]StoredPlan)
	var home *StoredPlan
	if active, err := s.repo.ActivePlan(ctx, user.ID); err == nil {
		home = &active
		plans[active.IntakeID] = active
	} else if !apperr.Is(err, apperr.ErrNotFound) {
		return WeekProgress{}, err
	}

	finished := make(map[string]bool, len(done))
	for _, d := range done {
		finished[d] = true
	}
	for _, slot := range week.Slots {
		p, ok := plans[slot.IntakeID]
		if !ok {
			p, err = s.repo.LatestPlanForIntake(ctx, user.ID, slot.IntakeID)
			if apperr.Is(err, apperr.ErrNotFound) {
				continue
			}
			if err != nil {
				return WeekProgress{}, err
			}
			plans[slot.IntakeID] = p
		}
		if slot.DayIndex < 0 || slot.DayIndex >= len(p.Plan.Days) {
			continue
		}
		day := volume.Day(p.Plan.Days[slot.DayIndex])
		day.Weekday = slot.Weekday
		day.StartTime = startTimeOn(home, slot.Weekday, day.StartTime)
		out.Days = append(out.Days, WeekDay{
			Slot:      slot,
			Date:      start.AddDate(0, 0, plan.WeekPosition(slot.Weekday)),
			PlanID:    p.ID,
			PlanName:  p.Plan.Name,
			Day:       day,
			Completed: finished[slot.Weekday],
		})
	}
	return out, nil
}

// startTimeOn is when someone trains on weekday: the time their plan sets for
// that weekday when it trains then, otherwise the session's own.
func startTimeOn(home *StoredPlan, weekday, own string) string {
	if home == nil {
		return own
	}
	for _, d := range home.Plan.Days {
		if wd, ok := plan.CanonicalWeekday(d.Weekday); ok && wd == weekday {
			return d.StartTime
		}
	}
	return own
}

func dayFor(days []WeekDay, weekday string) WeekDay {
	for _, d := range days {
		if d.Weekday == weekday {
			return d
		}
	}
	return WeekDay{}
}

// lockedWeekdays are the week's training days already finished.
func lockedWeekdays(slots []Slot, done []string) []string {
	var out []string
	for _, s := range slots {
		for _, d := range done {
			if s.Weekday == d {
				out = append(out, s.Weekday)
			}
		}
	}
	return out
}

// DueToday reports whether this week trains today, and the session's name.
func (s *Service) DueToday(ctx context.Context, user users.User, today time.Time) (string, string, bool, error) {
	progress, err := s.WeekProgress(ctx, user, today)
	if err != nil {
		return "", "", false, err
	}
	day := dayFor(progress.Days, today.In(user.Location()).Weekday().String())
	if day.Weekday == "" {
		return "", "", false, nil
	}
	title := day.Day.Focus
	if title == "" {
		title = day.Weekday
	}
	// The training routes live under /training, not /workouts — see
	// Handler.Routes. This link is persisted into user_nudges.href and sent as
	// a push payload, so getting it wrong strands people on a 404 long after
	// the row is written.
	return title, "/app/training/" + day.PlanID.String(), true, nil
}

// CompletedToday reports whether today's training day is already finished,
// and its focus. Only a session that finishes today's day counts: an evening
// run on leg day leaves the leg session open.
func (s *Service) CompletedToday(ctx context.Context, user users.User, today time.Time) (bool, string, error) {
	progress, err := s.WeekProgress(ctx, user, today)
	if err != nil {
		return false, "", err
	}
	day, ok := progress.DoneToday(today.In(user.Location()))
	if !ok {
		return false, "", nil
	}
	return true, day.Focus, nil
}

// Prescription is what the week containing at asked for on weekday: the
// day's focus and its sets in total. ok is false when that week did not train
// that day.
func (s *Service) Prescription(ctx context.Context, user users.User, at time.Time, weekday string) (string, int, bool, error) {
	week, err := s.weekAt(ctx, user, at)
	if err != nil {
		return "", 0, false, err
	}
	resolved, err := s.resolve(ctx, user, week, nil, at)
	if err != nil {
		return "", 0, false, err
	}
	wd, ok := plan.CanonicalWeekday(weekday)
	if !ok {
		return "", 0, false, nil
	}
	day := dayFor(resolved.Days, wd)
	if day.Weekday == "" {
		return "", 0, false, nil
	}
	sets := 0
	for _, e := range day.Day.Exercises {
		sets += e.Sets
	}
	return day.Day.Focus, sets, true, nil
}

// weekAt is the week containing at as recorded, or — for a week never read
// while it ran — the plan's usual week.
func (s *Service) weekAt(ctx context.Context, user users.User, at time.Time) (StoredWeek, error) {
	start := activity.WeekStart(at.In(user.Location()), user.Location())
	week, ok, err := s.repo.GetWeek(ctx, user.ID, start)
	if err != nil {
		return StoredWeek{}, err
	}
	if !ok {
		return s.defaultWeek(ctx, user, start)
	}
	week.Start = start
	return week, nil
}

// PlanSchedule is the training days of every week in rg, enough to count
// adherence: each recorded week as it was, every other week as the active
// plan's usual one. ok is false without a plan.
func (s *Service) PlanSchedule(ctx context.Context, user users.User, rg timerange.Range) (activity.Schedule, bool, error) {
	active, err := s.repo.ActivePlan(ctx, user.ID)
	if err != nil {
		if apperr.Is(err, apperr.ErrNotFound) {
			return activity.Schedule{}, false, nil
		}
		return activity.Schedule{}, false, err
	}
	// Records this week if nothing has yet, so it is counted as it will be
	// followed.
	if _, err := s.WeekProgress(ctx, user, time.Now()); err != nil {
		return activity.Schedule{}, false, err
	}

	loc := user.Location()
	out := activity.Schedule{Weeks: make(map[string][]activity.PlanSlot)}
	for _, slot := range plan.DefaultWeek(active.Plan, active.IntakeID, 0) {
		out.Default = append(out.Default, activity.PlanSlot{Weekday: slot.Weekday, Focus: active.Plan.Days[slot.DayIndex].Focus})
	}

	since := activity.WeekStart(rg.Since.In(loc), loc)
	weeks, err := s.repo.WeeksBetween(ctx, user.ID, since, rg.Until)
	if err != nil {
		return activity.Schedule{}, false, err
	}
	for _, w := range weeks {
		resolved, err := s.resolve(ctx, user, w, nil, rg.Until)
		if err != nil {
			return activity.Schedule{}, false, err
		}
		slots := make([]activity.PlanSlot, 0, len(resolved.Days))
		for _, d := range resolved.Days {
			slots = append(slots, activity.PlanSlot{Weekday: d.Weekday, Focus: d.Day.Focus})
		}
		out.Weeks[w.Start.Format(time.DateOnly)] = slots
	}
	return out, true, nil
}
