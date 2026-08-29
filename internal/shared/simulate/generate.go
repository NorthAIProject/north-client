package simulate

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"time"
)

// Nudge kinds this package raises.
//
// Deliberately plain strings rather than an import of internal/nudges/nudge:
// this package is a leaf under shared, and shared depending on a slice would
// invert the layering the whole codebase is arranged around. The writer, which
// already imports slices, owns a test asserting these still match the real
// constants — that catches drift without the dependency.
const (
	nudgeMissedCheckIn = "missed_checkin"
	nudgeWorkoutToday  = "workout_today"
	nudgeBriefingReady = "briefing_ready"
)

// simulatedZones spans real timezones, in fixed order, because every sweep in
// the product is timezone-aware and a population that all lives in UTC would
// exercise none of that. Two of these are deliberately on the far side of the
// date line from Europe, which is where local-date bugs actually surface.
var simulatedZones = []string{
	"Europe/Lisbon",
	"America/New_York",
	"Asia/Tokyo",
	"Australia/Sydney",
	"America/Los_Angeles",
	"Europe/Berlin",
}

// Options controls one generation run.
type Options struct {
	// Users is how many people to invent. They are dealt round-robin from the
	// persona catalog, so a run of forty covers every shape several times over
	// with different noise.
	Users int

	// Weeks of history to generate, ending the day before Now.
	Weeks int

	// Seed makes a run reproducible. The same seed, user count and week count
	// always produce byte-identical output.
	Seed uint64

	// Now is the simulation's "today". Zero means the real clock, which is what
	// the command uses; tests set it so a fixture does not change meaning
	// tomorrow.
	Now time.Time

	// Only restricts generation to these persona names. Empty means the whole
	// catalog. Useful when developing one detector.
	Only []string
}

func (o Options) validate() error {
	if o.Users < 1 {
		return fmt.Errorf("users must be at least 1, got %d", o.Users)
	}
	if o.Weeks < 1 {
		return fmt.Errorf("weeks must be at least 1, got %d", o.Weeks)
	}
	// Six weeks is the shortest window in which the week-five collapse is
	// visible at all: four good weeks, the break, and one week of aftermath to
	// distinguish a collapse from a bad week.
	if o.Weeks < 6 {
		return fmt.Errorf("weeks must be at least 6 for the collapse personas to be detectable, got %d", o.Weeks)
	}
	return nil
}

// Generate invents people and their histories. It is pure: same options, same
// output, no clock read unless Options.Now is zero, and no database.
func Generate(opts Options) ([]Person, error) {
	if err := opts.validate(); err != nil {
		return nil, err
	}

	catalog, err := selectPersonas(opts.Only)
	if err != nil {
		return nil, err
	}

	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}

	people := make([]Person, 0, opts.Users)
	for i := range opts.Users {
		persona := catalog[i%len(catalog)]

		// One source per person, seeded from the run seed and the person's
		// index. Deriving it this way means adding a person never changes the
		// history of the people before them, which is what makes a growing
		// fixture stable.
		src := rand.New(rand.NewPCG(opts.Seed, uint64(i)))

		people = append(people, generatePerson(persona, i, opts.Weeks, now, src))
	}

	return people, nil
}

func selectPersonas(only []string) ([]Persona, error) {
	if len(only) == 0 {
		return Personas, nil
	}

	out := make([]Persona, 0, len(only))
	for _, name := range only {
		p, ok := PersonaByName(name)
		if !ok {
			return nil, fmt.Errorf("unknown persona %q; known personas are %s", name, knownNames())
		}
		out = append(out, p)
	}
	return out, nil
}

func knownNames() string {
	names := make([]string, 0, len(Personas))
	for _, p := range Personas {
		names = append(names, p.Name)
	}
	return joinComma(names)
}

func joinComma(items []string) string {
	out := ""
	for i, it := range items {
		if i > 0 {
			out += ", "
		}
		out += it
	}
	return out
}

func generatePerson(persona Persona, index, weeks int, now time.Time, src *rand.Rand) Person {
	zone := simulatedZones[index%len(simulatedZones)]
	loc, err := time.LoadLocation(zone)
	if err != nil {
		// A missing tzdata entry is an environment problem, not a data
		// problem, and falling back keeps generation usable on a stripped
		// container rather than failing a whole run over one zone.
		loc = time.UTC
		zone = "UTC"
	}

	person := Person{
		Persona:     persona,
		Email:       fmt.Sprintf("%s-%02d@%s", persona.Name, index, SimulatedEmailDomain),
		DisplayName: displayName(persona.Name, index),
		Timezone:    zone,
		Habits:      persona.Habits,
		TrainingPlan: TrainingPlan{
			Name:           planName(persona),
			WeeksTotal:     weeks,
			DaysPerWeek:    persona.PlanDaysPerWeek,
			SessionMinutes: persona.PlanSessionMinutes,
		},
	}

	days := weeks * 7

	// The window ends yesterday. Generating today would race the real sweeps:
	// a resolver running against a commitment due "today" has a legitimate
	// reason to leave it open, and that would look like a detector bug.
	end := localMidnight(now.In(loc)).AddDate(0, 0, -1)
	start := end.AddDate(0, 0, -(days - 1))

	person.Days = make([]Day, 0, days)

	quietDays := 0
	for offset := range days {
		date := start.AddDate(0, 0, offset)
		week := offset / 7

		day := Day{
			Date:      date,
			WeekIndex: week,
		}

		// Habits and workouts.
		keep := persona.keepRate(week, date.Weekday())
		for _, h := range persona.Habits {
			if !slices.Contains(h.Days, date.Weekday()) {
				continue
			}
			if src.Float64() < keep {
				day.HabitsKept = append(day.HabitsKept, h.Name)
				if h.Domain == "fitness" && !day.WorkoutDone {
					day.WorkoutDone = true
					// Sessions run a little short of the plan more often than
					// long, which is how training actually goes. Never below
					// ten minutes: a shorter session is somebody leaving, and
					// that belongs in adherence rather than in volume.
					planned := persona.PlanSessionMinutes
					if planned <= 0 {
						planned = 45
					}
					day.TrainingMinutes = max(10, planned+src.IntN(21)-14)
				}
			}
		}

		// Check-in.
		if src.Float64() < persona.checkInRate(week) {
			day.CheckIn = generateCheckIn(persona, week, src)
			quietDays = 0
		} else {
			quietDays++
		}

		// Sleep: the device number is the truth, the log is what they typed.
		if persona.WearsDevice {
			minutes := persona.SleepMinutes + src.IntN(61) - 30
			day.DeviceSleepMinutes = &minutes
			day.Steps = 4000 + src.IntN(9000)
			day.RestingHR = 52 + src.IntN(14)
		}
		if persona.LogsSleep {
			// Built from the same night, then inflated. Independent draws would
			// make the two records unrelated noise, and the detector would be
			// finding a difference of means rather than a per-night bias — a
			// much weaker claim than the one the product wants to make.
			base := persona.SleepMinutes + src.IntN(61) - 30
			if day.DeviceSleepMinutes != nil {
				base = *day.DeviceSleepMinutes
			}
			logged := base + persona.SleepOverReport + src.IntN(21) - 10
			day.Sleep = &SleepLog{DurationMinutes: logged}
			if src.Float64() < 0.4 {
				q := qualityFor(persona, week, src)
				day.Sleep.Quality = &q
			}
		}

		day.HydrationML = generateHydration(persona, week, src)

		day.FoodLog = generateFoodLog(persona, week, src)
		day.Messages = generateMessages(persona, src)

		day.Nudges = generateNudges(persona, date, quietDays, src)

		person.Days = append(person.Days, day)
	}

	return person
}

func displayName(persona string, index int) string {
	// Readable in an admin list and obviously fake, which is the point: a
	// simulated account should never be mistaken for a real one at a glance.
	return fmt.Sprintf("Sim %s %02d", persona, index)
}

func generateCheckIn(persona Persona, week int, src *rand.Rand) *CheckIn {
	mood, energy := persona.Mood, persona.Energy
	if persona.collapsed(week) {
		mood--
		energy--
	}

	// Jitter by one in either direction, then bound to the 1-5 the column
	// actually accepts.
	mood = boundRating(mood + src.IntN(3) - 1)
	energy = boundRating(energy + src.IntN(3) - 1)

	in := &CheckIn{Mood: mood, Energy: energy}
	if src.Float64() < 0.5 {
		in.Wins = simulatedWins[src.IntN(len(simulatedWins))]
	}
	if src.Float64() < 0.3 {
		in.Notes = simulatedNotes[src.IntN(len(simulatedNotes))]
	}
	return in
}

// Free text exists because two detectors read it — the LLM pattern synthesizer
// and anything reading check-in prose — and an empty string teaches neither
// anything. Short and bland on purpose: this is filler, not fiction worth
// reading.
var (
	simulatedWins = []string{
		"Got the session done before work.",
		"Said no to a second coffee.",
		"Walked instead of driving.",
		"Cooked rather than ordered.",
		"Closed the laptop at a reasonable hour.",
	}
	simulatedNotes = []string{
		"Busy week, felt rushed.",
		"Shoulder still a bit tight.",
		"Slept badly, pushed through.",
		"Travelling, routine is off.",
		"Good day overall.",
	}
)

func qualityFor(persona Persona, week int, src *rand.Rand) int {
	q := 3
	if persona.SleepMinutes > 420 {
		q = 4
	}
	if persona.collapsed(week) {
		q--
	}
	return boundRating(q + src.IntN(3) - 1)
}

func generateHydration(persona Persona, week int, src *rand.Rand) int {
	// Someone whose adherence has collapsed stops logging water first: it is
	// the lowest-effort record in the product, so it is the first one to go.
	if persona.collapsed(week) && src.Float64() < 0.7 {
		return 0
	}
	if src.Float64() < 0.25 {
		return 0
	}
	// Rounded to the 250ml the UI actually offers, so simulated totals look
	// like real ones.
	return (4 + src.IntN(9)) * 250
}

func generateNudges(persona Persona, date time.Time, quietDays int, src *rand.Rand) []Nudge {
	var out []Nudge

	raise := func(kind string) {
		n := Nudge{Kind: kind}
		n.Read = src.Float64() < persona.NudgeReadRate
		n.Acted = src.Float64() < persona.NudgeActRate
		out = append(out, n)
	}

	// Mirrors the real engine's rule: two quiet local days raises one.
	if quietDays >= 2 {
		raise(nudgeMissedCheckIn)
	}
	if persona.scheduled(date.Weekday()) {
		raise(nudgeWorkoutToday)
	}
	// The briefing is opt-in and off by default in the product, so it is rare
	// here too rather than daily.
	if src.Float64() < 0.15 {
		raise(nudgeBriefingReady)
	}

	return out
}

// planName reads as something a person would have been given, and encodes the
// commitment so a plan row is legible without joining anything.
func planName(persona Persona) string {
	if persona.PlanDaysPerWeek <= 0 {
		return "Starter plan"
	}
	return fmt.Sprintf("%d-day plan, %d minutes", persona.PlanDaysPerWeek, persona.PlanSessionMinutes)
}

// generateFoodLog records what the person says they ate.
//
// Completeness is the signal, not accuracy: there is no device truth for intake
// the way there is for sleep, so a detector here can only observe who stops
// logging. Collapsed weeks log far less, and the drop-off leads the admission.
func generateFoodLog(persona Persona, week int, src *rand.Rand) []FoodEntry {
	if !persona.LogsFood {
		return nil
	}

	rate := persona.FoodLogRate
	if persona.collapsed(week) {
		rate *= 0.25
	}
	if src.Float64() >= clamp(rate) {
		return nil
	}

	// Two to four entries: people log meals, not bites. Partial days are the
	// common shape — breakfast recorded, dinner forgotten — so the count
	// varying is more honest than always logging three.
	count := 2 + src.IntN(3)
	perMeal := persona.TypicalKcal / count

	out := make([]FoodEntry, 0, count)
	for i := range count {
		kcal := perMeal + src.IntN(121) - 60
		if kcal < 50 {
			kcal = 50
		}
		out = append(out, FoodEntry{
			Label: mealLabels[i%len(mealLabels)],
			Kcal:  kcal,
			// Roughly a gram of protein per 12 kcal, which lands in the range
			// a real log does without pretending to be precise.
			ProteinG: kcal / 12,
		})
	}
	return out
}

var mealLabels = []string{"Breakfast", "Lunch", "Dinner", "Snack"}

// generateMessages produces coach exchanges, clustered at the hour this person
// actually answers.
//
// The user turn is placed first and the coach reply follows it, because that is
// the direction that carries the signal: a reply-latency profile is built from
// when the person chose to speak, not from when the engine did.
func generateMessages(persona Persona, src *rand.Rand) []Message {
	if persona.ExchangesPerWeek <= 0 {
		return nil
	}
	// At most one exchange a day, which is why the persona field is capped at
	// seven: a larger number cannot express itself here.
	if src.Float64() >= float64(persona.ExchangesPerWeek)/MaxExchangesPerWeek {
		return nil
	}

	// Two draws rather than one, so the hour clusters around the peak instead
	// of spreading uniformly across a window. A uniform spread would give a
	// detector a flat distribution with a peak it could not distinguish from
	// noise.
	jitter := (src.IntN(121)+src.IntN(121))/2 - 60
	minute := persona.PeakReplyHour*60 + jitter

	// Wrap rather than clamp: somebody who answers at 23:40 sometimes answers
	// at 00:10, and clamping would pile those onto midnight and invent a
	// second peak.
	minute = ((minute % 1440) + 1440) % 1440

	reply := (minute + 1 + src.IntN(3)) % 1440

	return []Message{
		{FromCoach: false, MinuteOfDay: minute, Text: simulatedUserTurns[src.IntN(len(simulatedUserTurns))]},
		{FromCoach: true, MinuteOfDay: reply, Text: "Noted. Keeping that in mind for the week."},
	}
}

var simulatedUserTurns = []string{
	"Managed the session today, felt strong.",
	"Skipped today, work ran late.",
	"Shoulder is bothering me again.",
	"Can we make next week lighter?",
	"Slept badly but got it done.",
	"Not sure this plan is working.",
}

func boundRating(v int) int {
	switch {
	case v < 1:
		return 1
	case v > 5:
		return 5
	default:
		return v
	}
}

// localMidnight is the start of the calendar day t falls in, in t's own
// location. The same shape check-ins, habits and sleep all store.
func localMidnight(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}
