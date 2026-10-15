package lift

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/FACorreiaa/go-utils/pkg/util"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/shared/i18n"
)

// RecapExercise is one movement in a finished session, against the last time
// it was logged when that exists.
type RecapExercise struct {
	Name           string
	Sets           int
	VolumeKg       float64
	Best           string
	E1RM           float64
	PreviousE1RM   float64
	Change         float64
	PreviousVolume float64
	HasPrevious    bool
}

// Recap is one finished workout in words and numbers. The sentence is what
// the person, the coach, and a later trainer all read.
type Recap struct {
	// SessionID, StartedAt, PlanWeekday and Focus say which workout this
	// was. PlanWeekday is empty for a session that finished no plan day.
	SessionID   uuid.UUID
	StartedAt   time.Time
	PlanWeekday string
	Focus       string

	// Sentence is the recap in English, for the coach and for any reader
	// with no language of its own. A person reads SentenceIn.
	Sentence       string
	Duration       time.Duration
	SetsDone       int
	SetsPrescribed int
	VolumeKg       float64
	Calories       float64
	Exercises      []RecapExercise
	HasComparison  bool
}

// BuildRecap summarises the sets of one session. earlier is every set of
// those exercises from before this session; the most recent of those is
// "last time".
// Warm-ups are left out throughout: they are neither sets done against the
// plan nor volume.
func BuildRecap(duration time.Duration, calories float64, prescribed int, current, earlier []Set) Recap {
	current, earlier = Working(current), Working(earlier)
	groups := groupInOrder(current)
	out := Recap{
		Duration:       duration,
		SetsDone:       len(current),
		SetsPrescribed: prescribed,
		Calories:       calories,
	}
	for _, sets := range groups {
		row := summarise(sets, earlier)
		out.VolumeKg += row.VolumeKg
		out.Exercises = append(out.Exercises, row)
		if row.HasPrevious {
			out.HasComparison = true
		}
	}
	out.VolumeKg = util.RoundHalfUpToScale(out.VolumeKg, 1)
	out.Sentence = out.SentenceIn(i18n.DefaultLocale)
	return out
}

func groupInOrder(sets []Set) [][]Set {
	index := map[string]int{}
	var groups [][]Set
	for _, s := range sets {
		i, ok := index[s.Key()]
		if !ok {
			index[s.Key()] = len(groups)
			groups = append(groups, nil)
			i = len(groups) - 1
		}
		groups[i] = append(groups[i], s)
	}
	return groups
}

func summarise(sets, earlier []Set) RecapExercise {
	row := RecapExercise{Name: sets[len(sets)-1].ExerciseName, Sets: len(sets)}
	var best Set
	var haveBest bool
	for _, s := range sets {
		row.VolumeKg += s.Volume()
		if s.WeightKg <= 0 {
			continue
		}
		if !haveBest || s.E1RM() > best.E1RM() {
			best = s
			haveBest = true
		}
	}
	row.VolumeKg = util.RoundHalfUpToScale(row.VolumeKg, 1)
	if haveBest {
		row.Best = fmt.Sprintf("%s × %d", formatKg(best.WeightKg), best.Reps)
		row.E1RM = util.RoundHalfUpToScale(best.E1RM(), 1)
	}
	prev := LastWorkout(filterKey(earlier, sets[0].Key()))
	if len(prev) == 0 {
		return row
	}
	var prevBest float64
	var havePrev bool
	for _, s := range prev {
		row.PreviousVolume += s.Volume()
		if s.WeightKg <= 0 {
			continue
		}
		if !havePrev || s.E1RM() > prevBest {
			prevBest = s.E1RM()
			havePrev = true
		}
	}
	row.PreviousVolume = util.RoundHalfUpToScale(row.PreviousVolume, 1)
	if havePrev && haveBest {
		row.HasPrevious = true
		row.PreviousE1RM = util.RoundHalfUpToScale(prevBest, 1)
		row.Change = util.RoundHalfUpToScale(row.E1RM-row.PreviousE1RM, 1)
	}
	return row
}

func filterKey(sets []Set, key string) []Set {
	var out []Set
	for _, s := range sets {
		if s.Key() == key {
			out = append(out, s)
		}
	}
	return out
}

// SentenceIn is the recap in one line in the language tag names: the time
// taken, the sets against the plan, the volume, and the exercise whose
// estimated max moved most since last time, when one moved at all.
func (r Recap) SentenceIn(tag string) string {
	var parts []string
	if r.Duration >= time.Minute {
		parts = append(parts, formatMinutes(tag, r.Duration))
	}
	switch {
	case r.SetsPrescribed > 0 && r.SetsDone > 0:
		parts = append(parts, i18n.TranslateF(tag, "lifts.recap.sets.of", r.SetsDone, r.SetsPrescribed))
	case r.SetsDone == 1:
		parts = append(parts, i18n.Translate(tag, "lifts.recap.set"))
	case r.SetsDone > 0:
		parts = append(parts, i18n.TranslateF(tag, "lifts.recap.sets", r.SetsDone))
	}
	if r.VolumeKg > 0 {
		parts = append(parts, groupThousands(tag, int(math.Round(r.VolumeKg)))+" kg")
	}
	sentence := strings.Join(parts, ", ")
	if sentence != "" {
		sentence += "."
	}
	change := r.biggestChange()
	if change == nil || math.Abs(change.Change) < 0.05 {
		return sentence
	}
	key, delta := "lifts.recap.up", change.Change
	if delta < 0 {
		key, delta = "lifts.recap.down", -delta
	}
	return strings.TrimSpace(sentence + " " + i18n.TranslateF(tag, key, change.Name, decimal(tag, delta)))
}

// biggestChange is the exercise whose estimated max moved most against last
// time, the first of equals; nil when none was done before.
func (r Recap) biggestChange() *RecapExercise {
	var best *RecapExercise
	for i := range r.Exercises {
		e := &r.Exercises[i]
		if e.HasPrevious && (best == nil || math.Abs(e.Change) > math.Abs(best.Change)) {
			best = e
		}
	}
	return best
}

func formatMinutes(tag string, d time.Duration) string {
	mins := int(d.Round(time.Minute).Minutes())
	if mins < 1 {
		mins = 1
	}
	h := mins / 60
	m := mins % 60
	minutes := i18n.Translate(tag, "lifts.recap.minute")
	if m != 1 {
		minutes = i18n.TranslateF(tag, "lifts.recap.minutes", m)
	}
	hours := i18n.Translate(tag, "lifts.recap.hour")
	if h != 1 {
		hours = i18n.TranslateF(tag, "lifts.recap.hours", h)
	}
	switch {
	case h == 0:
		return minutes
	case m == 0:
		return hours
	default:
		return i18n.TranslateF(tag, "lifts.recap.hours.minutes", hours, minutes)
	}
}

// separators are the thousands and decimal marks of tag's numbers. English
// writes 1,980.5; Spanish and Portuguese write 1.980,5.
func separators(tag string) (thousands, decimalMark string) {
	if tag == "" || tag == i18n.DefaultLocale {
		return ",", "."
	}
	return ".", ","
}

// decimal is v to one decimal place, with tag's decimal mark.
func decimal(tag string, v float64) string {
	_, mark := separators(tag)
	return strings.Replace(fmt.Sprintf("%.1f", v), ".", mark, 1)
}

func groupThousands(tag string, n int) string {
	sep, _ := separators(tag)
	return strings.ReplaceAll(comma(n), ",", sep)
}

func formatKg(v float64) string {
	if v == float64(int64(v)) {
		return fmt.Sprintf("%.0f kg", v)
	}
	return fmt.Sprintf("%.1f kg", v)
}

func comma(n int) string {
	neg := n < 0
	if neg {
		n = -n
	}
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		if neg {
			return "-" + s
		}
		return s
	}
	var b strings.Builder
	if neg {
		b.WriteByte('-')
	}
	lead := len(s) % 3
	if lead == 0 {
		lead = 3
	}
	b.WriteString(s[:lead])
	for i := lead; i < len(s); i += 3 {
		b.WriteByte(',')
		b.WriteString(s[i : i+3])
	}
	return b.String()
}

// RecapSummary renders a recap for the coach: which workout it was, the
// sentence, and each exercise against last time.
func RecapSummary(r Recap, loc *time.Location) string {
	var b strings.Builder
	b.WriteString("Last workout")
	if r.PlanWeekday != "" {
		b.WriteString(" (" + r.PlanWeekday + " plan day")
		if r.Focus != "" {
			b.WriteString(", " + r.Focus)
		}
		b.WriteString(")")
	}
	if !r.StartedAt.IsZero() {
		b.WriteString(", " + r.StartedAt.In(loc).Format("Mon 2 Jan"))
	}
	b.WriteString(": " + r.Sentence)
	for _, e := range r.Exercises {
		fmt.Fprintf(&b, "\n  %s: %d sets, %s kg volume", e.Name, e.Sets, comma(int(math.Round(e.VolumeKg))))
		if e.Best != "" {
			fmt.Fprintf(&b, ", best %s", e.Best)
		}
		if e.HasPrevious {
			fmt.Fprintf(&b, " (last time %s kg volume, e1RM %+.1f kg)", comma(int(math.Round(e.PreviousVolume))), e.Change)
		}
	}
	return b.String()
}
