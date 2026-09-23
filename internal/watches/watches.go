// Package watches is the coach's standing tasks: "every morning at 8, look at
// my sleep and tell me if it dropped under seven hours".
//
// The coach proposes one through the create_watch tool. It writes, so it stops
// at the approval card like every other write; the card shows the schedule in
// words, and only a Confirm stores the row. The worker's sweep_watches then
// runs each due watch through the coach and posts the answer into the thread
// as a proactive message (_reviews/muse-chat-contract.md, "Standing-task card").
package watches

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// ToolName is the capability the coach calls to propose a watch. Exported so
// the approval card and the confirmation can recognise it without the coach
// importing internal/agent.
const ToolName = "create_watch"

// Cadence is how often a watch runs.
type Cadence string

const (
	CadenceDaily  Cadence = "daily"
	CadenceWeekly Cadence = "weekly"
)

// Schedule is when a watch runs, in the person's own timezone.
type Schedule struct {
	Cadence Cadence

	// Weekday is read only for a weekly watch.
	Weekday time.Weekday

	// Minute is minutes after local midnight: 480 is 8:00.
	Minute int
}

// Clock is the time of day as a person writes it: "8:00", "18:30".
func (s Schedule) Clock() string {
	return fmt.Sprintf("%d:%02d", s.Minute/60, s.Minute%60)
}

// Next is the first run strictly after `after`, in loc.
//
// Computed from now rather than from the previous slot, so a worker that was
// down for a day runs a missed watch once, not once per missed slot.
func (s Schedule) Next(after time.Time, loc *time.Location) time.Time {
	if loc == nil {
		loc = time.UTC
	}
	local := after.In(loc)
	candidate := time.Date(local.Year(), local.Month(), local.Day(), s.Minute/60, s.Minute%60, 0, 0, loc)

	switch s.Cadence {
	case CadenceWeekly:
		days := (int(s.Weekday) - int(candidate.Weekday()) + 7) % 7
		candidate = candidate.AddDate(0, 0, days)
		if !candidate.After(after) {
			candidate = candidate.AddDate(0, 0, 7)
		}
	default:
		if !candidate.After(after) {
			candidate = candidate.AddDate(0, 0, 1)
		}
	}
	return candidate
}

// Proposal is a watch the coach asked to create, before anybody confirmed it.
type Proposal struct {
	// Title is what is watched ("your sleep"), Condition when to speak up
	// ("it drops under seven hours"). Both are read back to the person.
	Title     string
	Condition string

	// Spec is the instruction the coach runs each time.
	Spec string

	Schedule Schedule
}

// Confirmation is the sentence the coach posts once the person confirms:
// "Got it — I'll watch X and ping you when Y."
func (p Proposal) Confirmation() string {
	return "Got it — I'll watch " + trimEnd(p.Title) + " and ping you when " + trimEnd(p.Condition) + "."
}

func trimEnd(s string) string {
	return strings.TrimRight(strings.TrimSpace(s), ".!")
}

// Arguments is create_watch's argument shape, as the model sends it.
type Arguments struct {
	Watch      string `json:"watch"`
	NotifyWhen string `json:"notify_when"`
	Spec       string `json:"spec"`
	Cadence    string `json:"cadence"`
	Weekday    string `json:"weekday"`
	Time       string `json:"time"`
}

// ParseProposal reads a create_watch call's arguments.
//
// Used twice with the same answer: by the approval card, to show the schedule
// in words before anything is stored, and by the capability itself once the
// person has confirmed.
func ParseProposal(raw json.RawMessage) (Proposal, error) {
	var in Arguments
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &in); err != nil {
			return Proposal{}, apperr.Wrap(apperr.ErrValidation, "those arguments were not the shape create_watch expects")
		}
	}

	p := Proposal{
		Title:     strings.TrimSpace(in.Watch),
		Condition: strings.TrimSpace(in.NotifyWhen),
		Spec:      strings.TrimSpace(in.Spec),
	}
	var errs []string
	if p.Title == "" {
		errs = append(errs, "watch is required: what to keep an eye on")
	}
	if p.Condition == "" {
		errs = append(errs, "notify_when is required: when to speak up")
	}
	if p.Spec == "" {
		errs = append(errs, "spec is required: the instruction to run each time")
	}
	if len(p.Title) > maxField || len(p.Condition) > maxField {
		errs = append(errs, "watch and notify_when must each be a short phrase")
	}
	if len(p.Spec) > maxSpec {
		errs = append(errs, "spec is too long")
	}

	switch Cadence(strings.ToLower(strings.TrimSpace(in.Cadence))) {
	case CadenceDaily, "":
		p.Schedule.Cadence = CadenceDaily
	case CadenceWeekly:
		p.Schedule.Cadence = CadenceWeekly
		day, ok := parseWeekday(in.Weekday)
		if !ok {
			errs = append(errs, "weekday is required for a weekly watch: monday to sunday")
		}
		p.Schedule.Weekday = day
	default:
		errs = append(errs, "cadence must be daily or weekly")
	}

	minute, ok := parseClock(in.Time)
	if !ok {
		errs = append(errs, "time must be HH:MM in 24-hour time, like 08:00")
	}
	p.Schedule.Minute = minute

	if len(errs) > 0 {
		return Proposal{}, apperr.Wrap(apperr.ErrValidation, "%s", strings.Join(errs, "; "))
	}
	return p, nil
}

const (
	maxField = 200
	maxSpec  = 2000
)

func parseClock(s string) (int, bool) {
	h, m, found := strings.Cut(strings.TrimSpace(s), ":")
	if !found {
		return 0, false
	}
	hour, err := strconv.Atoi(h)
	if err != nil || hour < 0 || hour > 23 {
		return 0, false
	}
	minute, err := strconv.Atoi(m)
	if err != nil || minute < 0 || minute > 59 || len(m) != 2 {
		return 0, false
	}
	return hour*60 + minute, true
}

func parseWeekday(s string) (time.Weekday, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	for d := time.Sunday; d <= time.Saturday; d++ {
		name := strings.ToLower(d.String())
		if s == name || (len(s) >= 3 && strings.HasPrefix(name, s)) {
			return d, true
		}
	}
	return time.Sunday, false
}

// Watch is a confirmed standing task.
type Watch struct {
	ID             uuid.UUID
	UserID         uuid.UUID
	ConversationID uuid.UUID // uuid.Nil once the thread it was set up in is gone
	Proposal
	Active    bool
	NextRunAt time.Time
	LastRunAt *time.Time
	CreatedAt time.Time
}

// Instruction is what the coach is handed on a run.
func (w Watch) Instruction() string {
	return w.Spec + "\n\nWatching: " + w.Title + "\nSpeak up when: " + w.Condition
}
