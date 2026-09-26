package fitness

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/NorthAIProject/north-client/web/shared/ui/chart"
)

// WeekBar is one day of the weekly strip.
type WeekBar struct {
	Label   string
	Minutes float64
	IsToday bool
}

// WeekView is the "this week" card: three totals over a strip of days.
type WeekView struct {
	Bars       []WeekBar
	Sessions   int
	Minutes    float64
	DistanceKm float64
	Calories   float64
}

// RecentRow is one completed session in the recent list.
type RecentRow struct {
	Name       string
	Category   string
	Code       string
	Source     string
	EndedAt    time.Time
	Duration   time.Duration
	DistanceKm float64
	Calories   float64
}

// TrendDay is one day of a device metric.
type TrendDay struct {
	Day   time.Time
	Value float64
}

// Trend is a device metric card: a chart and the days behind it, oldest first.
type Trend struct {
	Chart chart.Props
	Days  []TrendDay
}

// Weight is the latest weigh-in and its change from the one before.
type Weight struct {
	Kg       float64
	DeltaKg  float64
	HasDelta bool
	At       time.Time
}

// maxMinutes is the tallest bar, so the others are drawn against it.
func (w WeekView) maxMinutes() float64 {
	var top float64
	for _, b := range w.Bars {
		top = math.Max(top, b.Minutes)
	}
	return top
}

// barHeight is a bar's height as a CSS percentage of the strip. A day with any
// activity is never shorter than a sliver, so ten minutes beside a two-hour
// ride still reads as "something happened".
func (w WeekView) barHeight(b WeekBar) string {
	top := w.maxMinutes()
	if top <= 0 || b.Minutes <= 0 {
		return "6%"
	}
	pct := math.Max(14, b.Minutes/top*100)
	return fmt.Sprintf("%.0f%%", pct)
}

func (w WeekView) hasActivity() bool { return w.Sessions > 0 }

func (w WeekView) avgMinutesPerDay() string {
	if len(w.Bars) == 0 {
		return "0"
	}
	return fmt.Sprintf("%.0f", w.Minutes/float64(len(w.Bars)))
}

// compact renders a count the way a glance reads it: 950, 6.5k, 30.9k, 1.2M.
func compact(v float64) string {
	switch a := math.Abs(v); {
	case a >= 1_000_000:
		return trimZero(fmt.Sprintf("%.1f", v/1_000_000)) + "M"
	case a >= 1000:
		return trimZero(fmt.Sprintf("%.1f", v/1000)) + "k"
	default:
		return fmt.Sprintf("%.0f", v)
	}
}

func trimZero(s string) string { return strings.TrimSuffix(s, ".0") }

func oneDecimal(v float64) string { return fmt.Sprintf("%.1f", v) }

// shortDuration renders a session length as 1h 51m or 48m.
func shortDuration(d time.Duration) string {
	minutes := int(d.Round(time.Minute).Minutes())
	if minutes < 60 {
		return fmt.Sprintf("%dm", minutes)
	}
	return fmt.Sprintf("%dh %02dm", minutes/60, minutes%60)
}

// pace is minutes per kilometre, only for sessions that moved somewhere.
func pace(r RecentRow) string {
	if r.DistanceKm < 0.1 || r.Duration <= 0 {
		return ""
	}
	secPerKm := r.Duration.Seconds() / r.DistanceKm
	return fmt.Sprintf("%d:%02d/km", int(secPerKm)/60, int(secPerKm)%60)
}

// recentDetail is the line under a session's name: only the parts it has.
func recentDetail(r RecentRow) []string {
	parts := []string{shortDuration(r.Duration)}
	if r.DistanceKm >= 0.1 {
		parts = append(parts, oneDecimal(r.DistanceKm)+" km")
	}
	if p := pace(r); p != "" {
		parts = append(parts, p)
	}
	if r.Calories > 0 {
		parts = append(parts, fmt.Sprintf("%.0f kcal", r.Calories))
	}
	return parts
}

// recentTitle shortens the MET table's names, which carry intensity in
// brackets, to what a list row needs: "Walking (brisk pace)" reads as Walking.
func recentTitle(r RecentRow) string {
	name, _, _ := strings.Cut(r.Name, " (")
	name = strings.ReplaceAll(name, "_", " ")
	if name == "" {
		return "Session"
	}
	return strings.ToUpper(name[:1]) + name[1:]
}

// recentIcon picks a glyph from the activity code first, since the category
// alone would draw a swim and a run with the same icon.
func recentIcon(r RecentRow) string {
	code := r.Code
	switch {
	case strings.Contains(code, "walk") || strings.Contains(code, "hik"):
		return "footprints"
	case strings.Contains(code, "run") || strings.Contains(code, "jog"):
		return "person-standing"
	case strings.Contains(code, "cycl") || strings.Contains(code, "bik"):
		return "bike"
	case strings.Contains(code, "swim") || strings.Contains(code, "row") || strings.Contains(code, "canoe") || strings.Contains(code, "kayak"):
		return "waves"
	case strings.Contains(code, "ski") || strings.Contains(code, "climb"):
		return "mountain"
	case r.Category == "strength":
		return "dumbbell"
	case r.Category == "flexibility":
		return "heart"
	default:
		return "activity"
	}
}

// when renders a timestamp as the list does: 25 Sep · 17:25.
func when(t time.Time, loc *time.Location) string {
	return t.In(loc).Format("2 Jan · 15:04")
}

// ago renders how long since a sync, in the one unit that matters.
func ago(t time.Time, now time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

// latest is the newest day with a reading. Today's steps are usually still
// counting, so a trailing zero is skipped rather than shown as the headline.
func (t Trend) latest() (TrendDay, bool) {
	for i := len(t.Days) - 1; i >= 0; i-- {
		if t.Days[i].Value > 0 {
			return t.Days[i], true
		}
	}
	return TrendDay{}, false
}

// perDay is the mean over days that have a reading.
func (t Trend) perDay() float64 {
	var sum float64
	var n int
	for _, d := range t.Days {
		if d.Value > 0 {
			sum += d.Value
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}

func (t Trend) first() TrendDay { return t.Days[0] }
func (t Trend) last() TrendDay  { return t.Days[len(t.Days)-1] }

// TrendRow is one day in a trend's list, with its change from the day before.
type TrendRow struct {
	Day      time.Time
	Value    float64
	Delta    float64
	HasDelta bool
}

// recentRows is the last few days with a reading, newest first, each with its
// change from the reading before it.
func (t Trend) recentRows(limit int) []TrendRow {
	var withData []TrendDay
	for _, d := range t.Days {
		if d.Value > 0 {
			withData = append(withData, d)
		}
	}
	var out []TrendRow
	for i := len(withData) - 1; i >= 0 && len(out) < limit; i-- {
		row := TrendRow{Day: withData[i].Day, Value: withData[i].Value}
		if i > 0 {
			row.Delta = withData[i].Value - withData[i-1].Value
			row.HasDelta = true
		}
		out = append(out, row)
	}
	return out
}

func signed(v float64, format string) string {
	s := fmt.Sprintf(format, v)
	if v > 0 {
		return "+" + s
	}
	return s
}

func deltaClass(v float64) string {
	switch {
	case v > 0:
		return "text-emerald-500"
	case v < 0:
		return "text-destructive"
	default:
		return "text-muted-foreground"
	}
}

// vo2Band is a coarse reading of a VO2 max estimate. Deliberately not age or
// sex adjusted: the hub does not know enough to grade someone precisely, and a
// falsely exact label is worse than a rough one.
func vo2Band(v float64) string {
	switch {
	case v >= 50:
		return "Excellent"
	case v >= 43:
		return "Good"
	case v >= 36:
		return "Fair"
	default:
		return "Low"
	}
}
