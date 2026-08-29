package simulate_test

import (
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/shared/simulate"
)

// A fixed clock, so a fixture does not change meaning tomorrow. Every test in
// here pins Now for the same reason.
var fixedNow = time.Date(2026, 8, 28, 14, 0, 0, 0, time.UTC)

func opts(users, weeks int, only ...string) simulate.Options {
	return simulate.Options{
		Users: users,
		Weeks: weeks,
		Seed:  7,
		Now:   fixedNow,
		Only:  only,
	}
}

func generate(t *testing.T, o simulate.Options) []simulate.Person {
	t.Helper()
	people, err := simulate.Generate(o)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	return people
}

// one generates a single person of the named persona, with enough weeks for the
// collapse shapes to be visible.
func one(t *testing.T, persona string, weeks int) simulate.Person {
	t.Helper()
	people := generate(t, opts(1, weeks, persona))
	if len(people) != 1 {
		t.Fatalf("got %d people, want 1", len(people))
	}
	if people[0].Persona.Name != persona {
		t.Fatalf("persona = %q, want %q", people[0].Persona.Name, persona)
	}
	return people[0]
}

// adherence is kept-over-scheduled across the given days, which is the same
// ratio internal/habits computes. Returns -1 when nothing was scheduled, so a
// caller can tell "missed everything" from "had nothing to miss" — the
// distinction the habits streak rule is built on.
func adherence(p simulate.Person, days []simulate.Day) float64 {
	var scheduled, kept int
	for _, d := range days {
		for _, h := range p.Habits {
			if !slices.Contains(h.Days, d.Date.Weekday()) {
				continue
			}
			scheduled++
			if slices.Contains(d.HabitsKept, h.Name) {
				kept++
			}
		}
	}
	if scheduled == 0 {
		return -1
	}
	return float64(kept) / float64(scheduled)
}

func weekAdherence(p simulate.Person, week int) float64 {
	return adherence(p, p.DaysInWeek(week))
}

// sustainedCliff reports whether adherence ever drops sharply and *stays* down.
//
// It compares the three weeks before a boundary with the three weeks after,
// rather than one week with the previous one. That distinction is the whole
// difference between the quitter and the slow fade: at a low absolute base a
// single week can halve on noise alone, so a week-over-week ratio calls a
// gradual decline a collapse. A detector built on the naive comparison would
// fire on everybody who was merely getting worse, and the intervention for a
// fade is not the intervention for a quit.
const cliffWindow = 3

func sustainedCliff(p simulate.Person) (boundary int, found bool) {
	for week := cliffWindow; week+cliffWindow <= p.Weeks(); week++ {
		before := windowAdherence(p, week-cliffWindow, week)
		after := windowAdherence(p, week, week+cliffWindow)

		if before < 0.4 || after < 0 {
			continue
		}
		if after < before*0.4 {
			return week, true
		}
	}
	return 0, false
}

// windowAdherence is adherence across the weeks in [from, to).
func windowAdherence(p simulate.Person, from, to int) float64 {
	var days []simulate.Day
	for _, d := range p.Days {
		if d.WeekIndex >= from && d.WeekIndex < to {
			days = append(days, d)
		}
	}
	return adherence(p, days)
}

// adherenceByWeekday is the kept-over-scheduled ratio per weekday, -1 where
// nothing was ever scheduled.
func adherenceByWeekday(p simulate.Person) map[time.Weekday]float64 {
	out := map[time.Weekday]float64{}
	for _, wd := range []time.Weekday{
		time.Sunday, time.Monday, time.Tuesday, time.Wednesday,
		time.Thursday, time.Friday, time.Saturday,
	} {
		var days []simulate.Day
		for _, d := range p.Days {
			if d.Date.Weekday() == wd {
				days = append(days, d)
			}
		}
		out[wd] = adherence(p, days)
	}
	return out
}

// weekdayOutlier reports whether exactly one scheduled weekday sits far below
// the others. This is the flake's signature, and it calls for moving a session
// rather than shrinking the week — a different prescription from any of the
// decline shapes, which is why it needs its own dimension.
func weekdayOutlier(p simulate.Person) (time.Weekday, bool) {
	rates := adherenceByWeekday(p)

	var worstDay time.Weekday
	worst, rest, restCount := 2.0, 0.0, 0

	for wd, rate := range rates {
		if rate < 0 {
			continue
		}
		if rate < worst {
			worst = rate
			_ = wd
			worstDay = wd
		}
	}
	for wd, rate := range rates {
		if rate < 0 || wd == worstDay {
			continue
		}
		rest += rate
		restCount++
	}
	if restCount == 0 || worst > 1 {
		return 0, false
	}

	return worstDay, worst < 0.3 && rest/float64(restCount) > 0.6
}

// --- Determinism -----------------------------------------------------------
//
// The whole value of this package is that a detector regression is
// reproducible. If these two tests fail, nothing else in here means anything.

func TestGenerateIsDeterministic(t *testing.T) {
	t.Parallel()

	first := generate(t, opts(12, 10))
	second := generate(t, opts(12, 10))

	if !reflect.DeepEqual(first, second) {
		t.Fatal("two runs with the same seed produced different histories")
	}
}

func TestDifferentSeedsProduceDifferentHistories(t *testing.T) {
	t.Parallel()

	a := generate(t, opts(4, 8))

	changed := opts(4, 8)
	changed.Seed = 8
	b := generate(t, changed)

	if reflect.DeepEqual(a, b) {
		t.Fatal("different seeds produced identical histories; the seed is not reaching generation")
	}
}

// Adding people must not disturb the ones already generated, or every fixture
// grows a dependency on the population size and stops being a fixture.
func TestAddingUsersDoesNotChangeEarlierHistories(t *testing.T) {
	t.Parallel()

	small := generate(t, opts(3, 8))
	large := generate(t, opts(9, 8))

	for i := range small {
		if !reflect.DeepEqual(small[i], large[i]) {
			t.Fatalf("person %d changed when the population grew from 3 to 9", i)
		}
	}
}

// --- Shape of the output ---------------------------------------------------

func TestDaysAreContiguousLocalMidnightsEndingYesterday(t *testing.T) {
	t.Parallel()

	for _, p := range generate(t, opts(6, 8)) {
		if got, want := len(p.Days), 8*7; got != want {
			t.Fatalf("%s: %d days, want %d", p.Persona.Name, got, want)
		}

		loc, err := time.LoadLocation(p.Timezone)
		if err != nil {
			t.Fatalf("%s: bad timezone %q: %v", p.Persona.Name, p.Timezone, err)
		}

		for i, d := range p.Days {
			if h, m, s := d.Date.Clock(); h != 0 || m != 0 || s != 0 {
				t.Errorf("%s day %d: %v is not local midnight", p.Persona.Name, i, d.Date)
			}
			if d.Date.Location().String() != loc.String() {
				t.Errorf("%s day %d: location %s, want %s", p.Persona.Name, i, d.Date.Location(), loc)
			}
			if want := i / 7; d.WeekIndex != want {
				t.Errorf("%s day %d: WeekIndex = %d, want %d", p.Persona.Name, i, d.WeekIndex, want)
			}
			if i > 0 {
				gap := d.Date.Sub(p.Days[i-1].Date)
				if gap < 23*time.Hour || gap > 25*time.Hour {
					t.Errorf("%s day %d: gap from previous day is %v, want one calendar day", p.Persona.Name, i, gap)
				}
			}
		}

		// The window must end yesterday, never today: a resolver running
		// against a commitment due today has a legitimate reason to leave it
		// open, and that would read as a detector bug.
		last := p.Days[len(p.Days)-1].Date
		yesterday := time.Date(fixedNow.In(loc).Year(), fixedNow.In(loc).Month(), fixedNow.In(loc).Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -1)
		if !last.Equal(yesterday) {
			t.Errorf("%s: last day is %v, want yesterday %v", p.Persona.Name, last, yesterday)
		}
	}
}

func TestRatingsStayInRange(t *testing.T) {
	t.Parallel()

	for _, p := range generate(t, opts(14, 8)) {
		for i, d := range p.Days {
			if d.CheckIn == nil {
				continue
			}
			if d.CheckIn.Mood < 1 || d.CheckIn.Mood > 5 {
				t.Errorf("%s day %d: mood %d out of range", p.Persona.Name, i, d.CheckIn.Mood)
			}
			if d.CheckIn.Energy < 1 || d.CheckIn.Energy > 5 {
				t.Errorf("%s day %d: energy %d out of range", p.Persona.Name, i, d.CheckIn.Energy)
			}
			if d.Sleep != nil && d.Sleep.Quality != nil {
				if q := *d.Sleep.Quality; q < 1 || q > 5 {
					t.Errorf("%s day %d: sleep quality %d out of range", p.Persona.Name, i, q)
				}
			}
		}
	}
}

func TestEveryPersonaStatesWhatADetectorShouldFind(t *testing.T) {
	t.Parallel()

	for _, persona := range simulate.Personas {
		if persona.Finding == "" {
			t.Errorf("persona %q has no Finding; a persona no detector acts on is untested weight", persona.Name)
		}
		if len(persona.Habits) == 0 {
			t.Errorf("persona %q declares no habits, so it has no adherence to observe", persona.Name)
		}
	}
}

// --- One test per persona: the detector contract ---------------------------
//
// Each of these asserts the hypothesis the persona exists to carry. They are
// the specification a real detector in internal/patterns has to satisfy, and
// they fail loudly if generation stops producing the shape.

func TestWeekFiveQuitterCollapsesInWeekFive(t *testing.T) {
	t.Parallel()

	p := one(t, simulate.PersonaWeekFiveQuitter, 10)

	// The four weeks before the break are strong.
	if got := windowAdherence(p, 0, 4); got < 0.7 {
		t.Errorf("weeks 0-3 adherence = %.2f, want strong (>= 0.7)", got)
	}

	// The aftermath is judged as a period, not week by week. Eight scheduled
	// slots a week is a small enough sample that a collapsed week reaches 0.38
	// on noise alone, so a per-week bound would be asserting something the data
	// cannot support — and a detector reads the period anyway.
	if got := windowAdherence(p, 4, p.Weeks()); got > 0.25 {
		t.Errorf("weeks 4+ adherence = %.2f, want collapsed (<= 0.25)", got)
	}

	// No individual week may look healthy, which is the weaker per-week claim
	// that the sample size does support.
	for week := 4; week < p.Weeks(); week++ {
		if got := weekAdherence(p, week); got > 0.55 {
			t.Errorf("week %d adherence = %.2f, want clearly below the healthy weeks", week, got)
		}
	}

	// And the collapse must be findable as a sustained cliff, near week 4.
	boundary, found := sustainedCliff(p)
	if !found {
		t.Fatal("no sustained cliff found; this is the persona a collapse detector exists for")
	}
	if boundary < 3 || boundary > 6 {
		t.Errorf("cliff found at week %d, want it near week 4", boundary)
	}
}

func TestSteadyImproverNeverCollapses(t *testing.T) {
	t.Parallel()

	p := one(t, simulate.PersonaSteadyImprover, 12)

	// The false-positive guard. A collapse detector that fires on this person
	// is worse than useless: it tells somebody who is succeeding to do less.
	early := windowAdherence(p, 0, 4)
	late := windowAdherence(p, p.Weeks()-4, p.Weeks())

	if late <= early {
		t.Errorf("adherence went from %.2f to %.2f; this persona is supposed to improve", early, late)
	}
	if week, found := sustainedCliff(p); found {
		t.Errorf("sustained cliff reported at week %d for the persona that never collapses", week)
	}
}

func TestSleepOverReporterLogsMoreThanTheDeviceSaw(t *testing.T) {
	t.Parallel()

	p := one(t, simulate.PersonaSleepOverReporter, 8)

	var nights, flattering, total int
	for _, d := range p.Days {
		if d.Sleep == nil || d.DeviceSleepMinutes == nil {
			continue
		}
		nights++
		gap := d.Sleep.DurationMinutes - *d.DeviceSleepMinutes
		total += gap
		if gap > 0 {
			flattering++
		}
	}

	if nights < 40 {
		t.Fatalf("only %d nights have both records; the detector needs both", nights)
	}

	// A per-night bias, not a difference of means: the claim the product wants
	// to make is "your log is kinder than your watch every night", which is far
	// stronger than "your averages differ".
	if ratio := float64(flattering) / float64(nights); ratio < 0.9 {
		t.Errorf("%.0f%% of nights over-report; want the bias on nearly every night", ratio*100)
	}

	mean := float64(total) / float64(nights)
	if mean < 55 || mean > 95 {
		t.Errorf("mean over-report = %.0f minutes, want roughly the persona's 75", mean)
	}
}

func TestSleepDetectorHasNothingToCompareWhenOneRecordIsMissing(t *testing.T) {
	t.Parallel()

	// weekend-only wears no device. A detector must stay silent here rather
	// than treating a missing record as agreement.
	p := one(t, simulate.PersonaWeekendOnly, 8)

	for i, d := range p.Days {
		if d.DeviceSleepMinutes != nil {
			t.Fatalf("day %d has a device reading; this persona wears no device", i)
		}
	}
}

func TestWeekendOnlyKeepsAlmostNothingOnWeekdays(t *testing.T) {
	t.Parallel()

	p := one(t, simulate.PersonaWeekendOnly, 8)

	var weekday, weekend []simulate.Day
	for _, d := range p.Days {
		switch d.Date.Weekday() {
		case time.Saturday, time.Sunday:
			weekend = append(weekend, d)
		default:
			weekday = append(weekday, d)
		}
	}

	weekdayRate, weekendRate := adherence(p, weekday), adherence(p, weekend)

	if weekdayRate > 0.25 {
		t.Errorf("weekday adherence = %.2f, want near zero", weekdayRate)
	}
	if weekendRate < 0.7 {
		t.Errorf("weekend adherence = %.2f, want high", weekendRate)
	}
}

func TestWednesdayFlakeMissesOnlyWednesday(t *testing.T) {
	t.Parallel()

	p := one(t, simulate.PersonaWednesdayFlake, 12)

	day, found := weekdayOutlier(p)
	if !found {
		t.Fatal("no single weekday stands out; this persona exists to be a weekday outlier")
	}
	if day != time.Wednesday {
		t.Errorf("outlier weekday = %s, want Wednesday", day)
	}

	rates := adherenceByWeekday(p)
	if got := rates[time.Wednesday]; got > 0.25 {
		t.Errorf("Wednesday adherence = %.2f, want near zero", got)
	}

	// The point of the persona is that the rest of the week is fine, so the
	// right intervention is moving one session rather than shrinking the plan.
	for _, wd := range []time.Weekday{time.Monday, time.Friday} {
		if got := rates[wd]; got >= 0 && got < 0.6 {
			t.Errorf("%s adherence = %.2f, want healthy", wd, got)
		}
	}
}

func TestNudgeBlindReadsWithoutActing(t *testing.T) {
	t.Parallel()

	p := one(t, simulate.PersonaNudgeBlind, 10)

	var raised, read, acted int
	for _, d := range p.Days {
		for _, n := range d.Nudges {
			raised++
			if n.Read {
				read++
			}
			if n.Acted {
				acted++
			}
		}
	}

	if raised < 20 {
		t.Fatalf("only %d nudges raised; the act-rate aggregate needs volume to mean anything", raised)
	}

	readRate := float64(read) / float64(raised)
	actRate := float64(acted) / float64(raised)

	// The distinction that matters: an engine counting reads would call this
	// person engaged and keep sending.
	if readRate < 0.75 {
		t.Errorf("read rate = %.2f, want high", readRate)
	}
	if actRate > 0.15 {
		t.Errorf("act rate = %.2f, want near zero", actRate)
	}
}

func TestSlowFadeDeclinesWithoutACliff(t *testing.T) {
	t.Parallel()

	p := one(t, simulate.PersonaSlowFade, 14)

	early := windowAdherence(p, 0, 4)
	late := windowAdherence(p, p.Weeks()-4, p.Weeks())

	if late >= early {
		t.Errorf("adherence went %.2f -> %.2f; this persona is supposed to decline", early, late)
	}

	// The finding this persona carries: a detector keyed to a cliff misses this
	// person entirely, even though they are on their way out. Catching a fade
	// needs a trend, not a threshold.
	if week, found := sustainedCliff(p); found {
		t.Errorf("sustained cliff reported at week %d; a fade must not present as a collapse", week)
	}
}

// Every persona must be distinguishable from every other by at least one of
// the coarse statistics a detector reads. Two personas with the same signature
// mean one of them is not carrying its own hypothesis.
func TestPersonasAreDistinguishable(t *testing.T) {
	t.Parallel()

	type signature struct {
		collapses      bool
		weekendGap     bool
		sleepBias      bool
		nudgeGap       bool
		declining      bool
		improving      bool
		weekdayOutlier bool
	}

	seen := map[signature]string{}

	for _, persona := range simulate.Personas {
		p := one(t, persona.Name, 12)

		early := windowAdherence(p, 0, 4)
		late := windowAdherence(p, p.Weeks()-4, p.Weeks())

		var sig signature
		sig.declining = late < early*0.9
		sig.improving = late > early*1.1

		_, sig.collapses = sustainedCliff(p)
		_, sig.weekdayOutlier = weekdayOutlier(p)

		var weekday, weekend []simulate.Day
		for _, d := range p.Days {
			switch d.Date.Weekday() {
			case time.Saturday, time.Sunday:
				weekend = append(weekend, d)
			default:
				weekday = append(weekday, d)
			}
		}
		sig.weekendGap = adherence(p, weekend)-adherence(p, weekday) > 0.4

		var nights, gapTotal int
		for _, d := range p.Days {
			if d.Sleep == nil || d.DeviceSleepMinutes == nil {
				continue
			}
			nights++
			gapTotal += d.Sleep.DurationMinutes - *d.DeviceSleepMinutes
		}
		sig.sleepBias = nights > 0 && float64(gapTotal)/float64(nights) > 40

		var raised, read, acted int
		for _, d := range p.Days {
			for _, n := range d.Nudges {
				raised++
				if n.Read {
					read++
				}
				if n.Acted {
					acted++
				}
			}
		}
		if raised > 0 {
			sig.nudgeGap = float64(read)/float64(raised)-float64(acted)/float64(raised) > 0.5
		}

		if other, clash := seen[sig]; clash {
			t.Errorf("personas %q and %q have the same detectable signature %+v", persona.Name, other, sig)
		}
		seen[sig] = persona.Name
	}
}

// --- Options --------------------------------------------------------------

func TestGenerateRejectsWindowsTooShortToShowACollapse(t *testing.T) {
	t.Parallel()

	for _, weeks := range []int{0, 1, 5} {
		o := opts(2, 8)
		o.Weeks = weeks
		if _, err := simulate.Generate(o); err == nil {
			t.Errorf("Generate accepted weeks = %d, want an error", weeks)
		}
	}
}

func TestGenerateRejectsUnknownPersona(t *testing.T) {
	t.Parallel()

	if _, err := simulate.Generate(opts(1, 8, "no-such-persona")); err == nil {
		t.Fatal("Generate accepted an unknown persona name")
	}
}

func TestGenerateDealsPersonasRoundRobin(t *testing.T) {
	t.Parallel()

	people := generate(t, opts(len(simulate.Personas)*2, 8))

	counts := map[string]int{}
	for _, p := range people {
		counts[p.Persona.Name]++
	}
	for _, persona := range simulate.Personas {
		if counts[persona.Name] != 2 {
			t.Errorf("persona %q appeared %d times, want 2", persona.Name, counts[persona.Name])
		}
	}
}

func TestSimulatedAccountsUseTheReservedDomain(t *testing.T) {
	t.Parallel()

	for _, p := range generate(t, opts(8, 8)) {
		if !hasSuffix(p.Email, "@"+simulate.SimulatedEmailDomain) {
			t.Errorf("email %q is not on the reserved simulated domain", p.Email)
		}
	}
}

func hasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

// --- The three domains added after the first pass --------------------------

func TestEveryPersonaDeclaresATrainingPlan(t *testing.T) {
	t.Parallel()

	for _, p := range generate(t, opts(len(simulate.Personas), 8)) {
		plan := p.TrainingPlan
		if plan.DaysPerWeek < 1 || plan.DaysPerWeek > 7 {
			t.Errorf("%s: plan declares %d days a week", p.Persona.Name, plan.DaysPerWeek)
		}
		if plan.SessionMinutes < 10 {
			t.Errorf("%s: plan declares %d-minute sessions", p.Persona.Name, plan.SessionMinutes)
		}
		if plan.Name == "" {
			t.Errorf("%s: plan has no name", p.Persona.Name)
		}
	}
}

// sessionsPerWeek is how many training sessions actually happened, averaged
// over the weeks in [from, to).
func sessionsPerWeek(p simulate.Person, from, to int) float64 {
	var sessions int
	for _, d := range p.Days {
		if d.WeekIndex >= from && d.WeekIndex < to && d.WorkoutDone {
			sessions++
		}
	}
	weeks := to - from
	if weeks <= 0 {
		return 0
	}
	return float64(sessions) / float64(weeks)
}

// The gap between what somebody signed up for and what they did is the input to
// plan revision. Two personas produce a wide gap for opposite reasons, and the
// prescriptions differ — which is exactly why the plan has to be recorded
// separately from the adherence it produces.
func TestPlannedVersusActualVolumeIsRecoverable(t *testing.T) {
	t.Parallel()

	t.Run("weekend-only signed up for a week they do not have", func(t *testing.T) {
		t.Parallel()

		p := one(t, simulate.PersonaWeekendOnly, 10)
		declared := float64(p.TrainingPlan.DaysPerWeek)
		actual := sessionsPerWeek(p, 0, p.Weeks())

		if declared < 4 {
			t.Fatalf("plan declares %.0f days; this persona is supposed to be over-committed", declared)
		}
		if actual > declared*0.6 {
			t.Errorf("did %.1f of %.0f declared sessions a week; want a wide gap", actual, declared)
		}
	})

	t.Run("the quitter's gap opens at the collapse", func(t *testing.T) {
		t.Parallel()

		p := one(t, simulate.PersonaWeekFiveQuitter, 10)
		before := sessionsPerWeek(p, 0, 4)
		after := sessionsPerWeek(p, 4, p.Weeks())

		if before < 2 {
			t.Errorf("did %.1f sessions a week before the collapse; want most of the plan", before)
		}
		if after > before*0.4 {
			t.Errorf("sessions went %.1f -> %.1f a week; want the gap to open", before, after)
		}
	})

	t.Run("the improver closes on their plan", func(t *testing.T) {
		t.Parallel()

		p := one(t, simulate.PersonaSteadyImprover, 12)
		early := sessionsPerWeek(p, 0, 4)
		late := sessionsPerWeek(p, p.Weeks()-4, p.Weeks())

		if late <= early {
			t.Errorf("sessions went %.1f -> %.1f a week; this persona improves", early, late)
		}
	})
}

// Food logging has no device truth to check it against, so the only honest
// signal is completeness: who stops recording, and when. It lapses before
// anybody says they have stopped, which is what makes it worth reading.
func TestFoodLoggingLapsesBeforeTheAdmission(t *testing.T) {
	t.Parallel()

	loggedDays := func(p simulate.Person, from, to int) float64 {
		var logged, total int
		for _, d := range p.Days {
			if d.WeekIndex < from || d.WeekIndex >= to {
				continue
			}
			total++
			if len(d.FoodLog) > 0 {
				logged++
			}
		}
		if total == 0 {
			return 0
		}
		return float64(logged) / float64(total)
	}

	quitter := one(t, simulate.PersonaWeekFiveQuitter, 10)
	before := loggedDays(quitter, 0, 4)
	after := loggedDays(quitter, 4, quitter.Weeks())

	if before < 0.5 {
		t.Errorf("logged %.0f%% of days before the collapse; want a real habit to lose", before*100)
	}
	if after > before*0.5 {
		t.Errorf("food logging went %.0f%% -> %.0f%%; want it to lapse", before*100, after*100)
	}

	improver := one(t, simulate.PersonaSteadyImprover, 10)
	if got := loggedDays(improver, 0, improver.Weeks()); got < 0.6 {
		t.Errorf("the improver logged %.0f%% of days; want it sustained", got*100)
	}

	// The persona that logs nothing must produce nothing, so a completeness
	// detector can tell "stopped" from "never started".
	weekend := one(t, simulate.PersonaWeekendOnly, 8)
	for i, d := range weekend.Days {
		if len(d.FoodLog) > 0 {
			t.Fatalf("day %d has a food log; this persona does not log food", i)
		}
	}
}

// The response-latency profile is what would let the nudge sweep speak when
// somebody is listening, instead of on a fixed schedule.
func TestPeakReplyHourIsRecoverable(t *testing.T) {
	t.Parallel()

	for _, persona := range simulate.Personas {
		if persona.ExchangesPerWeek <= 0 {
			continue
		}

		// Sixteen weeks: an hour-of-day distribution needs volume, and a
		// persona who says something twice a week has very little of it.
		p := one(t, persona.Name, 16)

		hours := map[int]int{}
		var userTurns int
		for _, d := range p.Days {
			for _, m := range d.Messages {
				if m.FromCoach {
					continue
				}
				userTurns++
				hours[m.MinuteOfDay/60]++
			}
		}

		if userTurns < 8 {
			t.Errorf("%s: only %d user turns in 16 weeks; too few to profile", persona.Name, userTurns)
			continue
		}

		peak, best := -1, 0
		for hour, n := range hours {
			if n > best {
				peak, best = hour, n
			}
		}

		// Within two hours: the jitter is deliberate, and a detector that
		// needed the exact hour would be reading noise.
		if diff := hourDistance(peak, persona.PeakReplyHour); diff > 2 {
			t.Errorf("%s: recovered peak hour %d, want near %d (distance %d)",
				persona.Name, peak, persona.PeakReplyHour, diff)
		}
	}
}

// hourDistance is the shortest distance between two hours on a 24-hour clock,
// so 23 and 0 are one apart rather than twenty-three.
func hourDistance(a, b int) int {
	d := a - b
	if d < 0 {
		d = -d
	}
	if d > 12 {
		d = 24 - d
	}
	return d
}

// Every user turn must be followed by a coach reply, and the reply must come
// after it. A conversation that opens with the coach and never gets an answer
// would teach a latency detector nothing.
func TestEveryExchangeIsAUserTurnThenACoachReply(t *testing.T) {
	t.Parallel()

	for _, p := range generate(t, opts(len(simulate.Personas), 8)) {
		for i, d := range p.Days {
			if len(d.Messages) == 0 {
				continue
			}
			if len(d.Messages) != 2 {
				t.Errorf("%s day %d: %d messages, want a pair", p.Persona.Name, i, len(d.Messages))
				continue
			}
			user, coach := d.Messages[0], d.Messages[1]
			if user.FromCoach || !coach.FromCoach {
				t.Errorf("%s day %d: exchange is not user-then-coach", p.Persona.Name, i)
			}
			if user.Text == "" || coach.Text == "" {
				t.Errorf("%s day %d: an empty turn", p.Persona.Name, i)
			}
			for _, m := range d.Messages {
				if m.MinuteOfDay < 0 || m.MinuteOfDay > 1439 {
					t.Errorf("%s day %d: minute %d out of range", p.Persona.Name, i, m.MinuteOfDay)
				}
			}
		}
	}
}

// A session that happened must have a duration, and one that did not must not.
func TestTrainingMinutesAgreeWithWhetherTheSessionHappened(t *testing.T) {
	t.Parallel()

	for _, p := range generate(t, opts(len(simulate.Personas), 8)) {
		for i, d := range p.Days {
			switch {
			case d.WorkoutDone && d.TrainingMinutes < 10:
				t.Errorf("%s day %d: session done but %d minutes", p.Persona.Name, i, d.TrainingMinutes)
			case !d.WorkoutDone && d.TrainingMinutes != 0:
				t.Errorf("%s day %d: no session but %d minutes", p.Persona.Name, i, d.TrainingMinutes)
			}
		}
	}
}

// A persona asking for more exchanges than a day can hold would read as chatty
// and generate the same volume as one asking for seven. Better to fail here
// than to have the parameter quietly mean nothing.
func TestNoPersonaExceedsTheExchangeCeiling(t *testing.T) {
	t.Parallel()

	for _, persona := range simulate.Personas {
		if persona.ExchangesPerWeek > simulate.MaxExchangesPerWeek {
			t.Errorf("persona %q asks for %d exchanges a week; generation can produce at most %d",
				persona.Name, persona.ExchangesPerWeek, simulate.MaxExchangesPerWeek)
		}
	}
}
