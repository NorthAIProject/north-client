package lift

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
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
func BuildRecap(duration time.Duration, calories float64, prescribed int, current, earlier []Set) Recap {
	groups := groupInOrder(current)
	out := Recap{
		Duration:       duration,
		SetsDone:       len(current),
		SetsPrescribed: prescribed,
		Calories:       calories,
	}
	var bestChange *RecapExercise
	for _, sets := range groups {
		row := summarise(sets, earlier)
		out.VolumeKg += row.VolumeKg
		out.Exercises = append(out.Exercises, row)
		if row.HasPrevious {
			out.HasComparison = true
			if bestChange == nil || math.Abs(row.Change) > math.Abs(bestChange.Change) {
				copied := row
				bestChange = &copied
			}
		}
	}
	out.VolumeKg = Round(out.VolumeKg)
	out.Sentence = recapSentence(out, bestChange)
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
	row.VolumeKg = Round(row.VolumeKg)
	if haveBest {
		row.Best = fmt.Sprintf("%s × %d", formatKg(best.WeightKg), best.Reps)
		row.E1RM = Round(best.E1RM())
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
	row.PreviousVolume = Round(row.PreviousVolume)
	if havePrev && haveBest {
		row.HasPrevious = true
		row.PreviousE1RM = Round(prevBest)
		row.Change = Round(row.E1RM - row.PreviousE1RM)
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

func recapSentence(r Recap, change *RecapExercise) string {
	var parts []string
	if r.Duration >= time.Minute {
		parts = append(parts, formatMinutes(r.Duration))
	}
	switch {
	case r.SetsPrescribed > 0 && r.SetsDone > 0:
		parts = append(parts, fmt.Sprintf("%d of %d sets", r.SetsDone, r.SetsPrescribed))
	case r.SetsDone == 1:
		parts = append(parts, "1 set")
	case r.SetsDone > 0:
		parts = append(parts, fmt.Sprintf("%d sets", r.SetsDone))
	}
	if r.VolumeKg > 0 {
		parts = append(parts, comma(int(math.Round(r.VolumeKg)))+" kg")
	}
	sentence := strings.Join(parts, ", ")
	if sentence != "" {
		sentence += "."
	}
	if change == nil || math.Abs(change.Change) < 0.05 {
		return sentence
	}
	dir := "up"
	delta := change.Change
	if delta < 0 {
		dir = "down"
		delta = -delta
	}
	return strings.TrimSpace(sentence + fmt.Sprintf(" %s estimated max %s %.1f kg.", change.Name, dir, delta))
}

func formatMinutes(d time.Duration) string {
	mins := int(d.Round(time.Minute).Minutes())
	if mins < 1 {
		mins = 1
	}
	h := mins / 60
	m := mins % 60
	switch {
	case h == 0:
		return fmt.Sprintf("%d minutes", m)
	case m == 0:
		if h == 1 {
			return "1 hour"
		}
		return fmt.Sprintf("%d hours", h)
	default:
		return fmt.Sprintf("%d hours %d minutes", h, m)
	}
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
