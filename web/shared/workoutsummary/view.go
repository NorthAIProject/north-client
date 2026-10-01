// Package workoutsummary renders a finished workout and a plan's week — the
// parts the plan page and Insights → Training both show.
//
// A shared package rather than one page importing the other's templates, for
// the reason web/shared/exerciserow gives: web/workouts owns the plan page,
// web/insights owns Insights, and neither should reach into the other.
package workoutsummary

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/NorthAIProject/north-client/internal/activity/activity"
	"github.com/NorthAIProject/north-client/internal/lifts/lift"
	"github.com/NorthAIProject/north-client/internal/shared/viz"
	"github.com/NorthAIProject/north-client/web/shared/ui/chart"
)

// Recap is one finished workout as the card draws it.
type Recap struct {
	Title    string
	When     string
	Sentence string

	Duration string
	Sets     string
	Volume   string
	Calories string

	Exercises []ExerciseRow
	// Chart is this session's volume per exercise beside last time's.
	Chart    chart.Props
	HasChart bool
}

// ExerciseRow is one movement with its estimated-max change, when known.
type ExerciseRow struct {
	Name   string
	Best   string
	E1RM   string
	Change float64
	// HasChange is false the first time an exercise is logged.
	HasChange bool
}

// NewRecap shapes a recap for the card. id names the chart, and must differ
// between two cards on one page.
func NewRecap(id string, r lift.Recap, loc *time.Location) Recap {
	out := Recap{
		Title:    recapTitle(r),
		Sentence: r.Sentence,
		Duration: minutes(r.Duration),
		Sets:     setsLabel(r.SetsDone, r.SetsPrescribed),
		Volume:   kg(r.VolumeKg),
		Calories: "—",
	}
	if !r.StartedAt.IsZero() {
		out.When = r.StartedAt.In(loc).Format("Mon 2 Jan, 15:04")
	}
	if r.Calories > 0 {
		out.Calories = fmt.Sprintf("%.0f kcal", r.Calories)
	}

	var labels []string
	var now, before []float64
	withVolume := false
	for _, e := range r.Exercises {
		row := ExerciseRow{Name: e.Name, Best: e.Best, HasChange: e.HasPrevious, Change: e.Change}
		if e.E1RM > 0 {
			row.E1RM = kg(e.E1RM)
		}
		out.Exercises = append(out.Exercises, row)

		labels = append(labels, e.Name)
		now = append(now, e.VolumeKg)
		before = append(before, e.PreviousVolume)
		withVolume = withVolume || e.VolumeKg > 0
	}
	if withVolume {
		series := []viz.BarSeries{{Label: "This session", Values: now}}
		if r.HasComparison {
			series = append(series, viz.BarSeries{Label: "Last time", Values: before})
		}
		out.Chart = viz.GroupedBar(id, labels, series)
		out.Chart.ShowLegend = r.HasComparison
		out.HasChart = true
	}
	return out
}

func recapTitle(r lift.Recap) string {
	switch {
	case r.PlanWeekday != "" && r.Focus != "":
		return r.PlanWeekday + " · " + r.Focus
	case r.PlanWeekday != "":
		return r.PlanWeekday
	default:
		return "Last workout"
	}
}

func setsLabel(done, prescribed int) string {
	if prescribed > 0 {
		return fmt.Sprintf("%d / %d", done, prescribed)
	}
	return fmt.Sprintf("%d", done)
}

func minutes(d time.Duration) string {
	m := int(d.Round(time.Minute).Minutes())
	if m < 60 {
		return fmt.Sprintf("%d min", m)
	}
	return fmt.Sprintf("%dh %02dm", m/60, m%60)
}

func kg(v float64) string {
	if v >= 1000 {
		return thousands(int(math.Round(v))) + " kg"
	}
	if v == math.Trunc(v) {
		return fmt.Sprintf("%.0f kg", v)
	}
	return fmt.Sprintf("%.1f kg", v)
}

func thousands(n int) string {
	s := fmt.Sprintf("%d", n)
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return b.String()
}

// ChangeLabel reads "+2.5 kg" or "−1 kg".
func ChangeLabel(v float64) string {
	sign := "+"
	if v < 0 {
		sign = "−"
		v = -v
	}
	return sign + kg(v)
}

// Week is a plan's Monday–Sunday week as a strip of days.
type Week struct {
	Sentence string
	Done     int
	Planned  int
	Days     []WeekDay
}

// WeekDay is one plan day in the strip.
type WeekDay struct {
	Short string
	Focus string
	Done  bool
	Today bool
	Past  bool
}

// NewWeek shapes this week's adherence for the strip.
func NewWeek(a activity.Adherence, now time.Time) Week {
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	out := Week{Sentence: a.Sentence, Done: a.Done, Planned: a.Planned}
	for _, d := range a.Days {
		date := d.Date.In(now.Location())
		out.Days = append(out.Days, WeekDay{
			Short: date.Format("Mon"),
			Focus: d.Focus,
			Done:  d.Done,
			Today: date.Equal(today),
			Past:  date.Before(today),
		})
	}
	return out
}

// Percent is how much of the week is done, for the bar.
func (w Week) Percent() int {
	if w.Planned == 0 {
		return 0
	}
	return w.Done * 100 / w.Planned
}
