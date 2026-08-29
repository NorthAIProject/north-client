package simulate

import "time"

// Persona is a behaviour shape, expressed as parameters rather than code.
//
// Declarative on purpose. The alternative — a switch with a branch per persona
// — makes every new behaviour shape a code change in the generator, and makes
// it impossible to see at a glance how two personas differ. Here the whole
// hypothesis a detector has to find is visible in one literal.
//
// Every field is a tendency, not a script: generation applies them through a
// seeded random source, so two people with the same persona have the same shape
// and different histories. A detector that only works on a noiseless persona
// does not work.
type Persona struct {
	// Name is the handle tests assert against. It also seeds the account's
	// email, so a simulated database can be read by eye.
	Name string

	// Finding states, in one sentence, what a detector is supposed to conclude
	// about this person. If no detector could ever act on it, the persona is
	// decoration and should not exist.
	Finding string

	// KeepRate is the baseline probability of keeping a scheduled habit or a
	// planned workout, before any of the modifiers below.
	KeepRate float64

	// QuitWeek is the zero-based week index at which adherence collapses to
	// QuitRate and stays there. Negative means this person never collapses.
	//
	// Week 4 is the fifth week, which is the one the landing page names.
	QuitWeek int
	QuitRate float64

	// WeeklyDrift is added to KeepRate each week. Positive is somebody slowly
	// getting their act together; negative is a slow fade, which is a
	// different and harder shape to detect than a collapse.
	WeeklyDrift float64

	// WeekdayFactor multiplies KeepRate on specific weekdays. Absent weekdays
	// are unmodified.
	//
	// Only ever read by lookup. Never range over it during generation: Go
	// randomises map iteration order, and that would make a seeded run
	// unreproducible — the one property this package exists to have.
	WeekdayFactor map[time.Weekday]float64

	// CheckInRate is the probability of filling in a daily check-in. Weighted
	// by the same collapse as KeepRate, because someone who has quit tends to
	// stop reporting before they admit they stopped.
	CheckInRate float64

	// Mood and Energy are the person's typical 1-5 self-ratings on a day they
	// checked in. Both drop while adherence is collapsed.
	Mood   int
	Energy int

	// SleepMinutes is the night this person actually gets, per their device.
	SleepMinutes int

	// SleepOverReport is how many minutes their self-reported sleep exceeds
	// what the device saw. Positive is the flattering direction, which is the
	// common one. Zero means their log and their watch agree.
	SleepOverReport int

	// LogsSleep and WearsDevice say which of the two sleep records exist. The
	// sleep-truth detector needs both and must stay silent when it has one.
	LogsSleep   bool
	WearsDevice bool

	// NudgeReadRate and NudgeActRate are the response profile the nudge
	// effectiveness detector is meant to recover. ActRate is conditional on
	// nothing — a person can act without reading, having done the thing anyway.
	NudgeReadRate float64
	NudgeActRate  float64

	// PlanDaysPerWeek and PlanSessionMinutes are what the person *signed up
	// for*. Kept separate from KeepRate on purpose: an over-ambitious plan and
	// a weak will produce the same adherence number, and the prescriptions are
	// opposite — one needs a smaller plan, the other needs a smaller ask.
	PlanDaysPerWeek    int
	PlanSessionMinutes int

	// LogsFood, FoodLogRate and TypicalKcal describe food recording. There is
	// no device truth for intake the way there is for sleep, so the detectable
	// signal here is completeness, not accuracy: who stops logging, and when.
	LogsFood    bool
	FoodLogRate float64
	TypicalKcal int

	// PeakReplyHour is the local hour this person actually answers, 0-23. The
	// nudge sweep currently fires on a fixed schedule; recovering this is what
	// would let it speak when somebody is listening.
	PeakReplyHour int

	// ExchangesPerWeek is how many user-then-coach exchanges happen in a week.
	//
	// Capped at 7, because generation raises at most one exchange per day and a
	// larger number would silently saturate — the parameter would read as
	// "chatty" and mean nothing. MaxExchangesPerWeek is enforced by a test
	// rather than clamped here, so an over-large value is a mistake somebody
	// hears about instead of one the generator quietly absorbs.
	ExchangesPerWeek int

	// Habits this persona declares.
	Habits []Habit
}

// Common habit schedules, named because a bare []time.Weekday at a call site
// says nothing about intent.
var (
	weekdaysOnly = []time.Weekday{
		time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday,
	}
	threeDays = []time.Weekday{time.Monday, time.Wednesday, time.Friday}
	weekends  = []time.Weekday{time.Saturday, time.Sunday}
	everyDay  = []time.Weekday{
		time.Sunday, time.Monday, time.Tuesday, time.Wednesday,
		time.Thursday, time.Friday, time.Saturday,
	}
)

// MaxExchangesPerWeek is the ceiling generation can actually produce: one
// user-then-coach exchange per day.
const MaxExchangesPerWeek = 7

// Persona names, exported so tests assert against a constant rather than a
// string literal that a rename would silently orphan.
const (
	PersonaWeekFiveQuitter   = "week-five-quitter"
	PersonaSleepOverReporter = "sleep-over-reporter"
	PersonaWeekendOnly       = "weekend-only"
	PersonaNudgeBlind        = "nudge-blind"
	PersonaSteadyImprover    = "steady-improver"
	PersonaWednesdayFlake    = "wednesday-flake"
	PersonaSlowFade          = "slow-fade"
)

// Personas is the catalog, in the order Generate cycles through it.
//
// Seven shapes rather than seven hundred: each one exists because a specific
// detector has to tell it apart from the others, and a persona nobody asserts
// against is untested weight.
var Personas = []Persona{
	{
		Name:    PersonaWeekFiveQuitter,
		Finding: "Adherence is strong for four weeks and collapses in week five; propose a lighter plan before it happens.",

		KeepRate: 0.88,
		QuitWeek: 4,
		QuitRate: 0.12,

		CheckInRate: 0.85,
		Mood:        4,
		Energy:      4,

		SleepMinutes: 430,
		LogsSleep:    true,
		WearsDevice:  true,

		NudgeReadRate: 0.7,
		NudgeActRate:  0.45,

		// Four days a week is the over-ambitious plan that sets up the
		// collapse. The plan is the co-author of the quit.
		PlanDaysPerWeek:    4,
		PlanSessionMinutes: 60,

		LogsFood:    true,
		FoodLogRate: 0.7,
		TypicalKcal: 2400,

		PeakReplyHour:    7,
		ExchangesPerWeek: 6,

		Habits: []Habit{
			{Name: "Train", Domain: "fitness", Days: threeDays},
			{Name: "Read 20 minutes", Domain: "learning", Days: weekdaysOnly},
		},
	},
	{
		Name:    PersonaSleepOverReporter,
		Finding: "Self-reported sleep runs about 75 minutes above the device every night; stop treating the logged number as fact.",

		KeepRate: 0.72,
		QuitWeek: -1,

		CheckInRate: 0.9,
		Mood:        3,
		Energy:      2,

		// The gap is the whole persona: they believe they sleep seven and a
		// half hours and the watch says a bit over six.
		SleepMinutes:    375,
		SleepOverReport: 75,
		LogsSleep:       true,
		WearsDevice:     true,

		NudgeReadRate: 0.65,
		NudgeActRate:  0.35,

		PlanDaysPerWeek:    3,
		PlanSessionMinutes: 45,

		LogsFood:    true,
		FoodLogRate: 0.8,
		TypicalKcal: 2200,

		// Answers just before midnight, which is consistent with the sleep they
		// are not getting — and is the kind of corroboration that makes a
		// pattern worth stating rather than guessing.
		PeakReplyHour:    23,
		ExchangesPerWeek: 6,

		Habits: []Habit{
			{Name: "Lights out by 23:00", Domain: "health", Days: everyDay},
			{Name: "Train", Domain: "fitness", Days: threeDays},
		},
	},
	{
		Name:    PersonaWeekendOnly,
		Finding: "Nothing scheduled on a weekday ever happens; the plan is fighting their week, not their willingness.",

		KeepRate: 0.9,
		QuitWeek: -1,

		WeekdayFactor: map[time.Weekday]float64{
			time.Monday:    0.1,
			time.Tuesday:   0.1,
			time.Wednesday: 0.1,
			time.Thursday:  0.1,
			time.Friday:    0.15,
		},

		CheckInRate: 0.5,
		Mood:        3,
		Energy:      3,

		SleepMinutes: 400,
		LogsSleep:    true,
		WearsDevice:  false,

		NudgeReadRate: 0.6,
		NudgeActRate:  0.3,

		// Five days declared against a life that only has two. The mismatch is
		// the finding, not the adherence number it produces.
		PlanDaysPerWeek:    5,
		PlanSessionMinutes: 60,

		LogsFood: false,

		PeakReplyHour:    11,
		ExchangesPerWeek: 3,

		Habits: []Habit{
			{Name: "Long walk", Domain: "fitness", Days: weekends},
			{Name: "Meal prep", Domain: "health", Days: weekends},
			{Name: "Train", Domain: "fitness", Days: weekdaysOnly},
		},
	},
	{
		Name:    PersonaNudgeBlind,
		Finding: "Reads almost every nudge and acts on almost none; the channel works and the message does not.",

		KeepRate: 0.55,
		QuitWeek: -1,

		CheckInRate: 0.6,
		Mood:        3,
		Energy:      3,

		SleepMinutes: 410,
		LogsSleep:    false,
		WearsDevice:  true,

		// The distinction that matters: high read, near-zero act. A engine that
		// only counts reads would call this person engaged.
		NudgeReadRate: 0.9,
		NudgeActRate:  0.04,

		PlanDaysPerWeek:    3,
		PlanSessionMinutes: 45,

		LogsFood:    true,
		FoodLogRate: 0.4,
		TypicalKcal: 2600,

		PeakReplyHour:    13,
		ExchangesPerWeek: 2,

		Habits: []Habit{
			{Name: "Train", Domain: "fitness", Days: threeDays},
			{Name: "Journal", Domain: "personal", Days: weekdaysOnly},
		},
	},
	{
		Name:    PersonaSteadyImprover,
		Finding: "Adherence climbs week over week with no collapse; the correct intervention is none. A detector that fires here is a false positive.",

		KeepRate:    0.45,
		QuitWeek:    -1,
		WeeklyDrift: 0.03,

		CheckInRate: 0.75,
		Mood:        4,
		Energy:      4,

		SleepMinutes: 445,
		LogsSleep:    true,
		WearsDevice:  true,

		NudgeReadRate: 0.8,
		NudgeActRate:  0.6,

		// A modest plan, which is part of why it survives.
		PlanDaysPerWeek:    3,
		PlanSessionMinutes: 40,

		LogsFood:    true,
		FoodLogRate: 0.85,
		TypicalKcal: 2100,

		PeakReplyHour:    6,
		ExchangesPerWeek: 7,

		Habits: []Habit{
			{Name: "Train", Domain: "fitness", Days: threeDays},
			{Name: "Walk after lunch", Domain: "health", Days: weekdaysOnly},
		},
	},
	{
		Name:    PersonaWednesdayFlake,
		Finding: "Misses Wednesdays and only Wednesdays; move the session rather than reducing the week.",

		KeepRate: 0.85,
		QuitWeek: -1,

		WeekdayFactor: map[time.Weekday]float64{
			time.Wednesday: 0.08,
		},

		CheckInRate: 0.8,
		Mood:        3,
		Energy:      3,

		SleepMinutes: 420,
		LogsSleep:    true,
		WearsDevice:  true,

		NudgeReadRate: 0.75,
		NudgeActRate:  0.5,

		PlanDaysPerWeek:    3,
		PlanSessionMinutes: 50,

		LogsFood:    true,
		FoodLogRate: 0.75,
		TypicalKcal: 2300,

		PeakReplyHour:    20,
		ExchangesPerWeek: 7,

		Habits: []Habit{
			{Name: "Train", Domain: "fitness", Days: threeDays},
			{Name: "Stretch", Domain: "health", Days: everyDay},
		},
	},
	{
		Name:    PersonaSlowFade,
		Finding: "No collapse, just a steady decline over months. Harder than the quitter: any detector keyed to a cliff will miss this entirely.",

		KeepRate:    0.9,
		QuitWeek:    -1,
		WeeklyDrift: -0.05,

		CheckInRate: 0.7,
		Mood:        3,
		Energy:      3,

		SleepMinutes: 415,
		LogsSleep:    true,
		WearsDevice:  true,

		NudgeReadRate: 0.5,
		NudgeActRate:  0.25,

		PlanDaysPerWeek:    4,
		PlanSessionMinutes: 55,

		LogsFood:    true,
		FoodLogRate: 0.6,
		TypicalKcal: 2500,

		PeakReplyHour:    21,
		ExchangesPerWeek: 5,

		Habits: []Habit{
			{Name: "Train", Domain: "fitness", Days: threeDays},
			{Name: "Cook at home", Domain: "health", Days: weekdaysOnly},
		},
	},
}

// PersonaByName returns a persona from the catalog.
func PersonaByName(name string) (Persona, bool) {
	for _, p := range Personas {
		if p.Name == name {
			return p, true
		}
	}
	return Persona{}, false
}

// keepRate is the persona's probability of keeping a scheduled thing on one
// day, after the collapse, the drift and the weekday factor have been applied.
//
// Clamped to [0,1] rather than trusted: drift over sixteen weeks can carry a
// rate out of range, and a negative probability is a silent bug in a place
// nobody looks.
func (p Persona) keepRate(week int, day time.Weekday) float64 {
	rate := p.KeepRate

	if p.QuitWeek >= 0 && week >= p.QuitWeek {
		rate = p.QuitRate
	} else {
		rate += p.WeeklyDrift * float64(week)
	}

	if f, ok := p.WeekdayFactor[day]; ok {
		rate *= f
	}

	return clamp(rate)
}

// checkInRate follows adherence down. Somebody who has stopped doing the work
// stops reporting first, and a check-in gap is often the earliest signal there
// is — so a simulator that kept reporting perfect while adherence collapsed
// would hide the very thing the detector should catch.
func (p Persona) checkInRate(week int) float64 {
	rate := p.CheckInRate
	if p.QuitWeek >= 0 && week >= p.QuitWeek {
		rate *= 0.4
	}
	return clamp(rate)
}

// collapsed reports whether the person is past their quit point, which mood
// and energy also reflect.
func (p Persona) collapsed(week int) bool {
	return p.QuitWeek >= 0 && week >= p.QuitWeek
}

func clamp(f float64) float64 {
	switch {
	case f < 0:
		return 0
	case f > 1:
		return 1
	default:
		return f
	}
}

// scheduled reports whether any of the persona's habits fall on a weekday.
// Person has the same method for hand-built fixtures; this one is what
// generation uses, before a Person exists.
func (p Persona) scheduled(day time.Weekday) bool {
	for _, h := range p.Habits {
		for _, d := range h.Days {
			if d == day {
				return true
			}
		}
	}
	return false
}
