// Package workoutsummary renders a finished workout and a plan's week — the
// parts the plan page and Insights → Training both show.
//
// A shared package rather than one page importing the other's templates, for
// the reason web/shared/exerciserow gives: web/workouts owns the plan page,
// web/insights owns Insights, and neither should reach into the other.
package workoutsummary

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/NorthAIProject/north-client/internal/activity/activity"
	"github.com/NorthAIProject/north-client/internal/lifts/lift"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/shared/viz"
	"github.com/NorthAIProject/north-client/web/shared/ui/chart"
)

// Recap is one finished workout as the card draws it.
//
// The title and the start time are kept as data rather than as sentences, so
// the card can say them in the reader's language: NewRecap has no request to
// read a locale from.
type Recap struct {
	PlanWeekday string
	Focus       string
	// StartedAt is already in the reader's zone; zero when unknown.
	StartedAt time.Time
	Sentence  string

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
		PlanWeekday: r.PlanWeekday,
		Focus:       r.Focus,
		Sentence:    r.Sentence,
		Duration:    minutes(r.Duration),
		Sets:        setsLabel(r.SetsDone, r.SetsPrescribed),
		Volume:      kg(r.VolumeKg),
		Calories:    "—",
	}
	if !r.StartedAt.IsZero() {
		out.StartedAt = r.StartedAt.In(loc)
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

// TitleIn names the workout by its plan day and focus, in the reader's
// language.
func (r Recap) TitleIn(ctx context.Context) string {
	day := weekdayIn(ctx, r.PlanWeekday)
	switch {
	case day != "" && r.Focus != "":
		return day + " · " + r.Focus
	case day != "":
		return day
	default:
		return i18n.T(ctx, "layout.recap.last")
	}
}

// WhenIn is the start time, with the weekday and month in the reader's
// language. Empty when the start is unknown.
func (r Recap) WhenIn(ctx context.Context) string {
	if r.StartedAt.IsZero() {
		return ""
	}
	return i18n.Date(ctx, r.StartedAt, "Mon 2 Jan, 15:04")
}

// ChartIn is the chart with its series named in the reader's language. The
// datasets are copied: Recap is a value, and two renders must not share one
// translated slice.
func (r Recap) ChartIn(ctx context.Context) chart.Props {
	out := r.Chart
	keys := []string{"layout.recap.series.now", "layout.recap.series.before"}
	out.Data.Datasets = append([]chart.Dataset(nil), r.Chart.Data.Datasets...)
	for i := range out.Data.Datasets {
		if i < len(keys) {
			out.Data.Datasets[i].Label = i18n.T(ctx, keys[i])
		}
	}
	return out
}

// weekdayIn translates an English weekday name ("Monday") through the
// calendar names the i18n package already holds, capitalised because it opens
// a title. Anything that is not a weekday name is returned unchanged.
func weekdayIn(ctx context.Context, english string) string {
	// 1 January 2023 was a Sunday, so adding the weekday's index lands on it.
	sunday := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)
	for wd := time.Sunday; wd <= time.Saturday; wd++ {
		if wd.String() == english {
			return capitalise(i18n.Date(ctx, sunday.AddDate(0, 0, int(wd)), "Monday"))
		}
	}
	return english
}

func capitalise(s string) string {
	r, size := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError {
		return s
	}
	return string(unicode.ToUpper(r)) + s[size:]
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
	// Date is the day itself; the card names it in the reader's language.
	Date  time.Time
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
			Date:  date,
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
