// Package simulate generates synthetic behaviour histories for development.
//
// It exists because the features that make North a coach rather than a tracker
// — the commitment ledger, the nudge-effectiveness loop, the pattern detectors
// — all learn from months of one person's behaviour, and the product has no
// users yet. Tuning a detector against the only real account available means
// tuning against a sample of one with no way to notice the overfit.
//
// So this package invents people. Each one has a named behaviour shape, and
// each shape is a hypothesis a detector is supposed to find: the person who
// quits in week five, the person whose logged sleep is an hour kinder than
// their watch's. A detector that cannot pick its own persona out of a crowd of
// forty is not finished, and that is a claim a table test can make.
//
// The split in here is deliberate and load-bearing:
//
//   - Generate produces plain values and touches nothing. It is pure and
//     deterministic from a seed, so a detector regression is reproducible and
//     detector tests need no database at all.
//   - Write is the only part that knows about Postgres, and it writes through
//     the real repositories rather than raw SQL, so simulated rows obey the
//     same constraints, timezone handling and natural keys as real ones.
//
// Nothing here is reachable over HTTP, and Write refuses to run against a
// production environment. The generated accounts use a reserved email domain
// so they can always be told apart from real ones.
package simulate

import "time"

// SimulatedEmailDomain marks every generated account. Real signups can never
// land here: the domain is reserved by RFC 2606 for exactly this purpose.
const SimulatedEmailDomain = "simulated.example"

// Person is one generated account and everything it did, oldest day first.
//
// This is the whole output of Generate: a value with no database in it, which
// is what lets a detector test build one by hand.
type Person struct {
	Persona Persona

	Email       string
	DisplayName string
	Timezone    string

	// Habits the person declared, by name. The schedule is the persona's, not
	// per-habit: someone who trains Monday/Wednesday/Friday declares all their
	// habits on those days, because the interesting variable is whether they
	// kept them.
	Habits []Habit

	// TrainingPlan is what the person signed up for, as distinct from what they
	// did. The gap between the two is the whole point: a plan is a declaration,
	// and adherence is the observation that tests it.
	TrainingPlan TrainingPlan

	// Days runs from the start of the simulated window to the day before the
	// simulation's "today", one entry per calendar day, with no gaps. A day
	// where the person did nothing is present and empty — the absence is the
	// signal a detector reads.
	Days []Day
}

// Habit is a declared recurring intention.
type Habit struct {
	Name   string
	Domain string

	// Days the habit is scheduled on. A day not in this list is not a missed
	// day, matching the rule habits already enforces.
	Days []time.Weekday
}

// Day is one local calendar day in one person's history.
//
// Pointer fields distinguish "did not record" from "recorded a zero", which is
// the distinction every detector in patterns depends on: a person who logs
// four hours of sleep is telling you something, and a person who logs nothing
// is telling you something else.
type Day struct {
	// Date is local midnight on the person's own calendar, which is the same
	// shape check-ins, habits and sleep all store.
	Date time.Time

	// WeekIndex counts weeks from the start of the window, zero-based. Week 4
	// is the fifth week, which is where the quitter is supposed to be found.
	WeekIndex int

	CheckIn *CheckIn

	// Sleep is what the person typed. DeviceSleepMinutes is what their watch
	// saw. The gap between the two is the point of the sleep-truth detector,
	// so a persona is free to set both, either, or neither.
	Sleep              *SleepLog
	DeviceSleepMinutes *int

	HydrationML int

	// Steps and RestingHR are device readings. Zero means no reading that day.
	Steps     int
	RestingHR int

	// HabitsKept names the habits completed on this day. Names rather than
	// indexes so a fixture stays readable when a persona's habit list changes.
	HabitsKept []string

	// WorkoutDone reports whether a planned training session happened, and
	// TrainingMinutes how long it ran. Zero minutes with WorkoutDone false is
	// the ordinary case; the pair is what makes planned-versus-actual volume
	// computable rather than just planned-versus-attended.
	WorkoutDone     bool
	TrainingMinutes int

	// FoodLog is what the person recorded eating. Empty means they logged
	// nothing that day, which is a signal in itself: food logging is the first
	// record to lapse, well before somebody admits they have stopped.
	FoodLog []FoodEntry

	// Messages are the turns exchanged with the coach on this day. The clock
	// times matter more than the words: when somebody actually replies is what
	// tells the nudge sweep when to speak.
	Messages []Message

	Nudges []Nudge
}

// CheckIn is a daily reflection as the person would have submitted it.
type CheckIn struct {
	Mood   int
	Energy int
	Wins   string
	Notes  string
}

// SleepLog is self-reported sleep: the number the person typed in.
type SleepLog struct {
	DurationMinutes int

	// Quality is 1-5, or nil when they logged hours without rating the night,
	// which is the common case in the real table too.
	Quality *int
}

// TrainingPlan is the declared training commitment.
//
// Deliberately thin: this is the shape a detector compares reality against, not
// the full generated plan the workouts slice stores. Sets, reps and exercise
// selection have no bearing on whether somebody turned up.
type TrainingPlan struct {
	Name           string
	WeeksTotal     int
	DaysPerWeek    int
	SessionMinutes int
}

// FoodEntry is one logged item. Macros beyond calories and protein are left out
// because no detector reads them yet, and inventing precision that nothing
// consumes is how a fixture becomes fiction.
type FoodEntry struct {
	Label    string
	Kcal     int
	ProteinG int
}

// Message is one turn in a coach conversation.
type Message struct {
	// FromCoach distinguishes the two roles. A coach message is the prompt; the
	// user message that follows it is the response whose latency is the signal.
	FromCoach bool

	// MinuteOfDay is local, 0-1439. Stored as a minute rather than a timestamp
	// because the hour of day is the whole finding — "they answer at 22:00" is
	// actionable, "they answered on 4 March" is not.
	MinuteOfDay int

	Text string
}

// Nudge is one proactive message the engine raised, and what became of it.
//
// Simulating the outcome is the entire point: nudge effectiveness cannot be
// learned from nudges that were only ever sent.
type Nudge struct {
	Kind string

	// Read and Acted describe the response. A nudge can be read and not acted
	// on, acted on without being read (the person did the thing anyway), or
	// neither.
	Read  bool
	Acted bool
}

// Window returns the first and last dates in the person's history.
// The zero times come back when the person has no days, which Generate never
// produces but a hand-built fixture might.
func (p Person) Window() (first, last time.Time) {
	if len(p.Days) == 0 {
		return time.Time{}, time.Time{}
	}
	return p.Days[0].Date, p.Days[len(p.Days)-1].Date
}

// Weeks is the number of whole and partial weeks the history covers.
func (p Person) Weeks() int {
	if len(p.Days) == 0 {
		return 0
	}
	return p.Days[len(p.Days)-1].WeekIndex + 1
}

// DaysInWeek returns the days belonging to one zero-based week index.
func (p Person) DaysInWeek(week int) []Day {
	var out []Day
	for _, d := range p.Days {
		if d.WeekIndex == week {
			out = append(out, d)
		}
	}
	return out
}

// Scheduled reports whether any of the person's habits fall on a weekday.
func (p Person) Scheduled(day time.Weekday) bool {
	for _, h := range p.Habits {
		for _, d := range h.Days {
			if d == day {
				return true
			}
		}
	}
	return false
}
