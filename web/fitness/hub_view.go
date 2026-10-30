package fitness

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/NorthAIProject/north-client/internal/shared/i18n"
)

// maxMinutes is the tallest bar, so the others are drawn against it.
func maxMinutes(w WeekView) float64 {
	var top float64
	for _, b := range w.Bars {
		top = math.Max(top, b.Minutes)
	}
	return top
}

// barHeight is a bar's height as a CSS percentage of the strip. A day with any
// activity is never shorter than a sliver, so ten minutes beside a two-hour
// ride still reads as "something happened".
func barHeight(w WeekView, b WeekBar) string {
	top := maxMinutes(w)
	if top <= 0 || b.Minutes <= 0 {
		return "6%"
	}
	pct := math.Max(14, b.Minutes/top*100)
	return fmt.Sprintf("%.0f%%", pct)
}

func hasActivity(w WeekView) bool { return w.Sessions > 0 }

func avgMinutesPerDay(w WeekView) string {
	if len(w.Bars) == 0 {
		return "0"
	}
	return fmt.Sprintf("%.0f", w.Minutes/float64(len(w.Bars)))
}

// compact renders a count the way a glance reads it: 950, 6.5k, 30.9k, 1.2M.
func compact(ctx context.Context, v float64) string {
	switch a := math.Abs(v); {
	case a >= 1_000_000:
		return trimZero(i18n.Decimal(ctx, v/1_000_000, 1)) + "M"
	case a >= 1000:
		return trimZero(i18n.Decimal(ctx, v/1000, 1)) + "k"
	default:
		return fmt.Sprintf("%.0f", v)
	}
}

// trimZero drops a trailing zero decimal in either locale's mark: "10.0" and
// "10,0" both read "10".
func trimZero(s string) string {
	return strings.TrimSuffix(strings.TrimSuffix(s, ".0"), ",0")
}

func oneDecimal(ctx context.Context, v float64) string { return i18n.Decimal(ctx, v, 1) }

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
func recentDetail(ctx context.Context, r RecentRow) []string {
	parts := []string{shortDuration(r.Duration)}
	if r.DistanceKm >= 0.1 {
		parts = append(parts, oneDecimal(ctx, r.DistanceKm)+" km")
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
func recentTitle(ctx context.Context, r RecentRow) string {
	name, _, _ := strings.Cut(r.Name, " (")
	name = strings.ReplaceAll(name, "_", " ")
	if name == "" {
		return i18n.T(ctx, "fitness.recent.session")
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
func when(ctx context.Context, t time.Time, loc *time.Location) string {
	return i18n.Date(ctx, t.In(loc), "2 Jan · 15:04")
}

// ago renders how long since a sync, in the one unit that matters.
func ago(ctx context.Context, t time.Time, now time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return i18n.T(ctx, "fitness.ago.now")
	case d < time.Hour:
		return i18n.Tf(ctx, "fitness.ago.min", int(d.Minutes()))
	case d < 48*time.Hour:
		return i18n.Tf(ctx, "fitness.ago.hours", int(d.Hours()))
	default:
		return i18n.Tf(ctx, "fitness.ago.days", int(d.Hours()/24))
	}
}

// latestReading is the newest day with a reading. Today's steps are usually
// still counting, so a trailing zero is skipped rather than shown as the
// headline.
func latestReading(t Trend) (TrendDay, bool) {
	for i := len(t.Days) - 1; i >= 0; i-- {
		if t.Days[i].Value > 0 {
			return t.Days[i], true
		}
	}
	return TrendDay{}, false
}

// perDay is the mean over days that have a reading.
func perDay(t Trend) float64 {
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

func firstDay(t Trend) TrendDay { return t.Days[0] }
func lastDay(t Trend) TrendDay  { return t.Days[len(t.Days)-1] }

// TrendRow is one day in a trend's list, with its change from the day before.
type TrendRow struct {
	Day      time.Time
	Value    float64
	Delta    float64
	HasDelta bool
}

// recentRows is the last few days with a reading, newest first, each with its
// change from the reading before it.
func recentRows(t Trend, limit int) []TrendRow {
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

func signed(ctx context.Context, v float64, prec int) string {
	s := i18n.Decimal(ctx, v, prec)
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
func vo2Band(ctx context.Context, v float64) string {
	switch {
	case v >= 50:
		return i18n.T(ctx, "fitness.vo2.excellent")
	case v >= 43:
		return i18n.T(ctx, "fitness.vo2.good")
	case v >= 36:
		return i18n.T(ctx, "fitness.vo2.fair")
	default:
		return i18n.T(ctx, "fitness.vo2.low")
	}
}

// aSunday anchors the weekday helpers below: adding n days to it lands on
// time.Weekday(n), which i18n.Date can then name in the reader's language.
var aSunday = time.Date(2024, time.January, 7, 12, 0, 0, 0, time.UTC)

// weekdayName names a plan day ("Wednesday", as the plan stores it) in the
// request's language, lower case as the Romance languages write it mid-sentence.
// A name that is not an English weekday is returned as it came.
func weekdayName(ctx context.Context, english string) string {
	for n := range 7 {
		if time.Weekday(n).String() == english {
			return i18n.Date(ctx, aSunday.AddDate(0, 0, n), "Monday")
		}
	}
	return english
}

// barLabel is the weekly strip's day label. The hub builds those as English
// two-letter abbreviations ("Mo", "Tu"); English keeps them, other languages
// get their own three-letter short names, since two letters are ambiguous in
// Portuguese (seg/sex, qua/qui).
func barLabel(ctx context.Context, label string) string {
	if i18n.LocaleFrom(ctx) == i18n.DefaultLocale {
		return label
	}
	for n := range 7 {
		if strings.HasPrefix(time.Weekday(n).String(), label) && len(label) >= 2 {
			return capitalize(i18n.Date(ctx, aSunday.AddDate(0, 0, n), "Mon"))
		}
	}
	return label
}

func capitalize(s string) string {
	r, size := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError {
		return s
	}
	return string(unicode.ToUpper(r)) + s[size:]
}
