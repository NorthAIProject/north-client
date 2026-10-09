// Package medication holds what a medication is, its daily schedule, and what
// a day of doses looks like.
//
// North records what a person tells it — the name, the dose as they say it,
// the times they take it — and never suggests, changes or advises on a dose.
// Dose is free text for that reason: there is no arithmetic on it anywhere.
package medication

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

const (
	StatusTaken   = "taken"
	StatusSkipped = "skipped"

	// The states of a scheduled slot nobody has answered yet: its time has
	// passed, or it is still to come.
	StatusDue      = "due"
	StatusUpcoming = "upcoming"

	MaxName = 80
	MaxDose = 40

	// ReminderWindow is how long after a slot's time a reminder may still go
	// out. Past it the dose is history rather than a prompt, so a worker that
	// was down all morning does not send the 08:00 dose at dinner.
	ReminderWindow = 2 * time.Hour
)

// AllDays is every weekday, 0=Sunday .. 6=Saturday as Go's time.Weekday.
func AllDays() []int { return []int{0, 1, 2, 3, 4, 5, 6} }

// ValidStatus reports whether s is something a dose can be logged as.
func ValidStatus(s string) bool { return s == StatusTaken || s == StatusSkipped }

// Medication is one thing someone takes, on a schedule or as needed.
type Medication struct {
	ID   uuid.UUID
	Name string
	Dose string
	// Times are "HH:MM" in the person's zone, sorted. Empty means as needed.
	Times []string
	// Days are 0=Sunday .. 6=Saturday, sorted; never empty.
	Days      []int
	Remind    bool
	Notes     string
	StoppedAt *time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

// AsNeeded reports a medication with no schedule.
func (m Medication) AsNeeded() bool { return len(m.Times) == 0 }

// Active reports a medication still being taken.
func (m Medication) Active() bool { return m.StoppedAt == nil }

// Label is "Metformin 500 mg", or just the name when no dose was given.
func (m Medication) Label() string {
	if m.Dose == "" {
		return m.Name
	}
	return m.Name + " " + m.Dose
}

// TakenOn reports whether the schedule includes that weekday.
func (m Medication) TakenOn(day time.Weekday) bool { return slices.Contains(m.Days, int(day)) }

// Input is a medication as someone describes it, before validation.
type Input struct {
	Name   string
	Dose   string
	Times  []string
	Days   []int
	Remind bool
	Notes  string
}

// Validate trims and normalises in: times sorted and deduplicated, days
// defaulting to every day.
func Validate(in Input) (Input, error) {
	var errs apperr.FieldErrors
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len([]rune(in.Name)) > MaxName {
		errs = errs.Add("name", "Name the medication, in up to 80 characters.")
	}
	in.Dose = strings.TrimSpace(in.Dose)
	if len([]rune(in.Dose)) > MaxDose {
		errs = errs.Add("dose", "Keep the dose to 40 characters, as you would write it on the box.")
	}
	in.Notes = strings.TrimSpace(in.Notes)

	times := make([]string, 0, len(in.Times))
	for _, raw := range in.Times {
		t, ok := NormalizeTime(raw)
		if !ok {
			errs = errs.Add("times", "Times are HH:MM, like 08:00 or 20:30.")
			break
		}
		if !slices.Contains(times, t) {
			times = append(times, t)
		}
	}
	sort.Strings(times)
	in.Times = times

	if len(in.Days) == 0 {
		in.Days = AllDays()
	}
	days := make([]int, 0, len(in.Days))
	for _, d := range in.Days {
		if d < 0 || d > 6 {
			errs = errs.Add("days", "Days are 0 (Sunday) to 6 (Saturday).")
			break
		}
		if !slices.Contains(days, d) {
			days = append(days, d)
		}
	}
	slices.Sort(days)
	in.Days = days

	return in, errs.OrNil()
}

// NormalizeTime reads "8:00", "08:00" or " 20:30 " as zero-padded "HH:MM".
func NormalizeTime(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	t, err := time.Parse("15:04", raw)
	if err != nil {
		return "", false
	}
	return t.Format("15:04"), true
}

// Dose is one logged dose: a scheduled slot answered, or an as-needed dose.
type Dose struct {
	ID           uuid.UUID
	MedicationID uuid.UUID
	// Name and DoseText are the medication's, as it reads now.
	Name     string
	DoseText string
	LogDate  time.Time
	// Slot is the scheduled "HH:MM" this answers; nil for an unscheduled dose.
	Slot     *string
	Status   string
	LoggedAt time.Time
}

// Label is "Metformin 500 mg".
func (d Dose) Label() string {
	return Medication{Name: d.Name, Dose: d.DoseText}.Label()
}

// SlotsFor is the medication's scheduled times on a weekday: none for an
// as-needed medication or a day off.
func SlotsFor(m Medication, day time.Weekday) []string {
	if m.AsNeeded() || !m.TakenOn(day) {
		return nil
	}
	return m.Times
}

// PickSlot is the slot a dose logged now most likely answers: today's
// scheduled time nearest to localNow that is not already logged.
//
// It returns nil with ok for an as-needed medication or on a day it is not
// scheduled — the dose is recorded without a slot. ok is false only when every
// slot today has an answer already, which is for the caller to explain rather
// than to overwrite silently.
func PickSlot(m Medication, logsToday []Dose, localNow time.Time) (slot *string, ok bool) {
	slots := SlotsFor(m, localNow.Weekday())
	if len(slots) == 0 {
		return nil, true
	}
	nowMin := localNow.Hour()*60 + localNow.Minute()
	best, bestGap := "", -1
	for _, s := range slots {
		if loggedSlot(m.ID, logsToday, s) {
			continue
		}
		gap := minutesOf(s) - nowMin
		if gap < 0 {
			gap = -gap
		}
		if bestGap < 0 || gap < bestGap {
			best, bestGap = s, gap
		}
	}
	if bestGap < 0 {
		return nil, false
	}
	return &best, true
}

func loggedSlot(medID uuid.UUID, logs []Dose, slot string) bool {
	for _, l := range logs {
		if l.MedicationID == medID && l.Slot != nil && *l.Slot == slot {
			return true
		}
	}
	return false
}

func minutesOf(hhmm string) int {
	t, err := time.Parse("15:04", hhmm)
	if err != nil {
		return 0
	}
	return t.Hour()*60 + t.Minute()
}

// Slot is one scheduled dose today and what happened to it.
type Slot struct {
	Medication Medication
	Time       string
	// Log is the answer, nil while nobody has given one.
	Log *Dose
}

// Status is taken or skipped once answered; otherwise due once its time has
// passed, and upcoming before.
func (s Slot) Status(localNow time.Time) string {
	if s.Log != nil {
		return s.Log.Status
	}
	if minutesOf(s.Time) <= localNow.Hour()*60+localNow.Minute() {
		return StatusDue
	}
	return StatusUpcoming
}

// Unscheduled is a medication's doses taken outside any slot: everything for
// an as-needed medication, an extra dose on a day off for a scheduled one.
type Unscheduled struct {
	Medication Medication
	Doses      []Dose
}

// Day is one day of medications.
type Day struct {
	// Now is the moment the day was read at, in the person's zone. It is what
	// tells a due slot from an upcoming one.
	Now time.Time
	// Slots are every scheduled dose today, in time order.
	Slots []Slot
	// AsNeeded lists every as-needed medication, with or without doses, and
	// any scheduled one with a dose outside its slots.
	AsNeeded []Unscheduled
}

// Empty reports a day with no medications at all to show.
func (d Day) Empty() bool { return len(d.Slots) == 0 && len(d.AsNeeded) == 0 }

// BuildDay lays the active medications against the doses logged on
// localNow's date. localNow must be in the person's zone.
func BuildDay(meds []Medication, logs []Dose, localNow time.Time) Day {
	out := Day{Now: localNow}
	day := localNow.Weekday()
	for _, m := range meds {
		if !m.Active() {
			continue
		}
		for _, t := range SlotsFor(m, day) {
			s := Slot{Medication: m, Time: t}
			for i := range logs {
				if logs[i].MedicationID == m.ID && logs[i].Slot != nil && *logs[i].Slot == t {
					s.Log = &logs[i]
					break
				}
			}
			out.Slots = append(out.Slots, s)
		}

		var extra []Dose
		for _, l := range logs {
			if l.MedicationID == m.ID && l.Slot == nil {
				extra = append(extra, l)
			}
		}
		if m.AsNeeded() || len(extra) > 0 {
			sort.Slice(extra, func(i, j int) bool { return extra[i].LoggedAt.Before(extra[j].LoggedAt) })
			out.AsNeeded = append(out.AsNeeded, Unscheduled{Medication: m, Doses: extra})
		}
	}
	sort.SliceStable(out.Slots, func(i, j int) bool { return out.Slots[i].Time < out.Slots[j].Time })
	return out
}

// Reminder is a scheduled dose whose time has come and that nobody answered.
type Reminder struct {
	Medication Medication
	Slot       string
	DueAt      time.Time
}

// DueReminders are the unanswered slots of medications set to remind whose
// time came within the last window before the day's Now.
func DueReminders(d Day, window time.Duration) []Reminder {
	localNow := d.Now
	var out []Reminder
	for _, s := range d.Slots {
		if s.Log != nil || !s.Medication.Remind {
			continue
		}
		clock := minutesOf(s.Time)
		dueAt := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), clock/60, clock%60, 0, 0, localNow.Location())
		if localNow.Before(dueAt) || localNow.Sub(dueAt) > window {
			continue
		}
		out = append(out, Reminder{Medication: s.Medication, Slot: s.Time, DueAt: dueAt})
	}
	return out
}

// Summary renders the day for the coach, one medication after another:
// "Medications today: Metformin 500 mg — 08:00 taken, 20:00 due."
func Summary(d Day) string {
	localNow := d.Now
	if d.Empty() {
		return "Medications: none tracked."
	}

	var order []uuid.UUID
	slotsBy := map[uuid.UUID][]string{}
	labels := map[uuid.UUID]string{}
	for _, s := range d.Slots {
		id := s.Medication.ID
		if _, seen := labels[id]; !seen {
			order = append(order, id)
			labels[id] = s.Medication.Label()
		}
		slotsBy[id] = append(slotsBy[id], s.Time+" "+s.Status(localNow))
	}
	parts := make([]string, 0, len(order)+len(d.AsNeeded))
	for _, id := range order {
		parts = append(parts, labels[id]+" — "+strings.Join(slotsBy[id], ", "))
	}
	for _, u := range d.AsNeeded {
		parts = append(parts, u.Medication.Label()+" (as needed) — "+unscheduledSummary(u.Doses, localNow.Location()))
	}
	return "Medications today: " + strings.Join(parts, "; ") + "."
}

func unscheduledSummary(doses []Dose, loc *time.Location) string {
	var taken []string
	skipped := 0
	for _, d := range doses {
		if d.Status == StatusTaken {
			taken = append(taken, d.LoggedAt.In(loc).Format("15:04"))
		} else {
			skipped++
		}
	}
	if len(taken) == 0 && skipped == 0 {
		return "none today"
	}
	out := "none taken"
	if len(taken) > 0 {
		out = fmt.Sprintf("taken %d× (%s)", len(taken), strings.Join(taken, ", "))
	}
	if skipped > 0 {
		out += fmt.Sprintf(", skipped %d", skipped)
	}
	return out
}

// Schedule is "08:00, 20:00 · every day" or "as needed".
func Schedule(m Medication) string {
	if m.AsNeeded() {
		return "as needed"
	}
	days := "every day"
	if len(m.Days) < 7 {
		names := make([]string, len(m.Days))
		for i, d := range m.Days {
			names[i] = time.Weekday(d).String()[:3]
		}
		days = strings.Join(names, ", ")
	}
	return strings.Join(m.Times, ", ") + " · " + days
}

// Matching finds the medications a spoken name refers to. An exact name wins
// outright, so "Metformin" is not ambiguous beside "Metformin XR"; otherwise
// every medication whose name contains it, case-insensitively.
func Matching(meds []Medication, name string) []Medication {
	needle := strings.ToLower(strings.TrimSpace(name))
	if needle == "" {
		return nil
	}
	var hits []Medication
	for _, m := range meds {
		lower := strings.ToLower(m.Name)
		if lower == needle {
			return []Medication{m}
		}
		if strings.Contains(lower, needle) {
			hits = append(hits, m)
		}
	}
	return hits
}

// Names lists medications' names, for errors that tell the reader what there is.
func Names(meds []Medication) []string {
	out := make([]string, len(meds))
	for i, m := range meds {
		out[i] = m.Name
	}
	return out
}
