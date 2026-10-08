package plan

import (
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
)

// A week of training is chosen, not fixed. A plan is an ordered rotation of
// sessions; a week is a set of weekdays, each given the next session of that
// rotation. Training three days instead of the usual four takes the next three
// sessions, and the week after resumes with the one that was left over —
// nothing is skipped for good because a week was short.
//
// The plan's own weekdays are only its default week. A Slot is what binds a
// session to a day, and it names the plan it came from, so one week can mix
// sessions from several saved plans.

// Slot is one training day of one week: on Weekday, train the day at DayIndex
// of the plan whose versions share IntakeID. It names the plan rather than a
// plan version so that exercise edits made after the week was set show up in
// it.
type Slot struct {
	Weekday  string    `json:"weekday"`
	IntakeID uuid.UUID `json:"intake_id"`
	DayIndex int       `json:"day_index"`
}

// spreads are the suggested weekdays for each number of training days: as
// evenly apart as a Monday–Sunday week allows, Sunday kept free until seven.
var spreads = map[int][]time.Weekday{
	1: {time.Wednesday},
	2: {time.Monday, time.Thursday},
	3: {time.Monday, time.Wednesday, time.Friday},
	4: {time.Monday, time.Tuesday, time.Thursday, time.Friday},
	5: {time.Monday, time.Tuesday, time.Wednesday, time.Friday, time.Saturday},
	6: {time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday, time.Saturday},
	7: {time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday, time.Saturday, time.Sunday},
}

// SpreadWeekdays suggests which weekdays to train n days a week on, Monday
// first. n outside 1–7 is no days.
func SpreadWeekdays(n int) []string {
	days := spreads[n]
	out := make([]string, 0, len(days))
	for _, d := range days {
		out = append(out, d.String())
	}
	return out
}

// SuggestWeekdays suggests n training days for a week already under way:
// the days in locked (trained already) count towards n, and the rest are
// spread over the days from today on. today is today's position, Monday 0, or
// -1 for a week not yet started, which gets SpreadWeekdays.
func SuggestWeekdays(n, today int, locked []string) []string {
	if today <= 0 && len(locked) == 0 {
		return SpreadWeekdays(n)
	}
	taken := weekdaySet(locked)
	out := make([]string, 0, n)
	for wd := range taken {
		out = append(out, wd)
	}
	var open []string
	for pos := max(today, 0); pos < 7; pos++ {
		wd := time.Weekday((pos + 1) % 7).String()
		if !taken[wd] {
			open = append(open, wd)
		}
	}
	need := n - len(out)
	switch {
	case need <= 0:
	case need >= len(open):
		out = append(out, open...)
	case need == 1:
		out = append(out, open[0])
	default:
		// Evenly apart, first and last open day included.
		for i := range need {
			out = append(out, open[i*(len(open)-1)/(need-1)])
		}
	}
	days, _ := NormalizeWeekdays(out)
	return days
}

// CanonicalWeekday is label as time.Weekday spells it ("monday" → "Monday"),
// or false when it names no weekday.
func CanonicalWeekday(label string) (string, bool) {
	wd, ok := parseWeekday(label)
	if !ok {
		return "", false
	}
	return wd.String(), true
}

// NormalizeWeekdays canonicalises labels and orders them Monday first. Unknown
// and repeated weekdays are errors: a week trains each day at most once.
func NormalizeWeekdays(labels []string) ([]string, error) {
	seen := make(map[string]bool, len(labels))
	out := make([]string, 0, len(labels))
	for _, label := range labels {
		wd, ok := CanonicalWeekday(label)
		if !ok {
			return nil, fmt.Errorf("%q is not a day of the week", label)
		}
		if seen[wd] {
			return nil, fmt.Errorf("%s is listed twice", wd)
		}
		seen[wd] = true
		out = append(out, wd)
	}
	sort.SliceStable(out, func(i, j int) bool { return WeekPosition(out[i]) < WeekPosition(out[j]) })
	return out, nil
}

// WeekPosition counts from Monday: Monday is 0, Sunday 6, anything else 7.
func WeekPosition(label string) int {
	wd, ok := parseWeekday(label)
	if !ok {
		return 7
	}
	return (int(wd) + 6) % 7
}

// DefaultWeekdays are the weekdays p is written for, Monday first. A plan
// whose labels are not one distinct weekday per day gets the suggested spread
// for its number of days instead, so it still has a week to follow.
func DefaultWeekdays(p Plan) []string {
	labels := make([]string, 0, len(p.Days))
	for _, d := range p.Days {
		labels = append(labels, d.Weekday)
	}
	if days, err := NormalizeWeekdays(labels); err == nil && len(days) > 0 {
		return days
	}
	return SpreadWeekdays(len(p.Days))
}

// RotationOrder is the order p's days are trained in: by the weekday each is
// written for, Monday first, so a plan as written is followed exactly as
// before. Days with no recognisable weekday keep their written order, last.
func RotationOrder(p Plan) []int {
	order := make([]int, len(p.Days))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		return WeekPosition(p.Days[order[a]].Weekday) < WeekPosition(p.Days[order[b]].Weekday)
	})
	return order
}

// After is the rotation position that follows the day at dayIndex: where the
// plan resumes once that day is trained.
func After(p Plan, dayIndex int) int {
	return positionOf(p, dayIndex) + 1
}

// positionOf is where the day at dayIndex sits in the rotation, or 0 when p
// has no such day.
func positionOf(p Plan, dayIndex int) int {
	for pos, i := range RotationOrder(p) {
		if i == dayIndex {
			return pos
		}
	}
	return 0
}

// Fill gives each weekday the next session of p's rotation, starting at
// position start and wrapping, so six days of a four-day plan repeat its first
// two. weekdays must already be normalised.
func Fill(p Plan, intake uuid.UUID, weekdays []string, start int) []Slot {
	order := RotationOrder(p)
	if len(order) == 0 {
		return nil
	}
	slots := make([]Slot, 0, len(weekdays))
	for i, wd := range weekdays {
		slots = append(slots, Slot{Weekday: wd, IntakeID: intake, DayIndex: order[mod(start+i, len(order))]})
	}
	return slots
}

// DefaultWeek is p's usual week: its own weekdays, filled from position start.
func DefaultWeek(p Plan, intake uuid.UUID, start int) []Slot {
	return Fill(p, intake, DefaultWeekdays(p), start)
}

// Resume is the rotation position of intake's plan p in the week after slots:
// just past the last of its sessions that was done, or — when none was — the
// first of its sessions that week, which is owed again. ok is false when the
// week had none of p's sessions.
func Resume(p Plan, intake uuid.UUID, slots []Slot, done []string) (int, bool) {
	finished := weekdaySet(done)
	sorted := sortedSlots(slots)
	first, last := -1, -1
	for i, s := range sorted {
		if s.IntakeID != intake {
			continue
		}
		if first < 0 {
			first = i
		}
		if finished[s.Weekday] {
			last = i
		}
	}
	switch {
	case last >= 0:
		return After(p, sorted[last].DayIndex), true
	case first >= 0:
		return positionOf(p, sorted[first].DayIndex), true
	default:
		return 0, false
	}
}

// Change is a person choosing what the rest of a week trains.
type Change struct {
	// Today is today's position in the week, Monday 0. -1 is a week that has
	// not started, where every day is still to come.
	Today int

	// Existing are the week's slots before the change, and Done the weekdays
	// already finished. A finished slot stays whatever the change says: it is
	// history.
	Existing []Slot
	Done     []string

	// Weekdays are the days to train, normalised. Assign pins a weekday to a
	// specific session; every other weekday takes the next session of
	// Source's rotation.
	Weekdays []string
	Assign   map[string]Slot

	Source       Plan
	SourceIntake uuid.UUID

	// Cursor is Source's rotation position at the start of the week. A
	// finished Source session this week moves it on.
	Cursor int
}

// Rebuild applies a change to a week. Finished slots are kept; past days that
// were not trained are dropped, since nothing can be trained on them any more;
// the chosen days from today on are filled in order, so changing four days to
// three halfway through the week carries on from what was already done.
func Rebuild(c Change) []Slot {
	finished := weekdaySet(c.Done)
	var out []Slot
	taken := make(map[string]bool)
	cursor := c.Cursor
	for _, s := range sortedSlots(c.Existing) {
		if !finished[s.Weekday] {
			continue
		}
		out = append(out, s)
		taken[s.Weekday] = true
		if s.IntakeID == c.SourceIntake {
			cursor = After(c.Source, s.DayIndex)
		}
	}

	order := RotationOrder(c.Source)
	for _, wd := range c.Weekdays {
		if taken[wd] || WeekPosition(wd) < c.Today {
			continue
		}
		if pinned, ok := c.Assign[wd]; ok {
			pinned.Weekday = wd
			out = append(out, pinned)
			continue
		}
		if len(order) == 0 {
			continue
		}
		out = append(out, Slot{Weekday: wd, IntakeID: c.SourceIntake, DayIndex: order[mod(cursor, len(order))]})
		cursor++
	}
	return sortedSlots(out)
}

// NextSlot is the slot to train next in a week: today's when it is not done,
// otherwise the first open one after it. ok is false when nothing is left this
// week.
func NextSlot(slots []Slot, now time.Time, done []string) (Slot, bool) {
	finished := weekdaySet(done)
	today := daysIntoWeek(now)
	for _, s := range sortedSlots(slots) {
		if WeekPosition(s.Weekday) < today || finished[s.Weekday] {
			continue
		}
		return s, true
	}
	return Slot{}, false
}

// SlotOn is the slot on weekday, if the week trains that day.
func SlotOn(slots []Slot, weekday string) (Slot, bool) {
	wd, ok := CanonicalWeekday(weekday)
	if !ok {
		return Slot{}, false
	}
	for _, s := range slots {
		if s.Weekday == wd {
			return s, true
		}
	}
	return Slot{}, false
}

func sortedSlots(slots []Slot) []Slot {
	out := append([]Slot(nil), slots...)
	sort.SliceStable(out, func(i, j int) bool { return WeekPosition(out[i].Weekday) < WeekPosition(out[j].Weekday) })
	return out
}

func weekdaySet(labels []string) map[string]bool {
	out := make(map[string]bool, len(labels))
	for _, l := range labels {
		if wd, ok := CanonicalWeekday(l); ok {
			out[wd] = true
		}
	}
	return out
}

func mod(a, n int) int {
	return ((a % n) + n) % n
}
