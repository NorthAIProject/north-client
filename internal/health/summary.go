package health

import (
	"fmt"
	"math"
	"strings"
)

// aggregation decides which number off a window of readings means something.
type aggregation int

const (
	// mean is for rates — a resting heart rate, an HRV. Totalling seven
	// mornings' heart rate produces a number with no interpretation.
	mean aggregation = iota

	// perDay is for counts — steps, active calories. Averaging the raw rows
	// would make the answer depend on how often the phone happened to sync,
	// so the day's readings are totalled and the days are averaged.
	perDay
)

// headline is one metric the coach is told about, and how to say it.
//
// This list is presentation, not storage: health_metrics accepts any metric a
// provider sends, and this decides which of them are worth spending prompt
// space on and what to call them in English. A metric missing from here is
// still stored and still queryable — it simply is not narrated.
//
// Kept short on purpose. The coach reads these before interpreting anything
// else, and twenty ambient numbers would crowd out the conversation they exist
// to inform.
type headline struct {
	metric string
	label  string
	agg    aggregation

	// decimals is how precise the number deserves to sound. A heart rate of
	// "58 bpm" is honest; "58.34 bpm" implies a measurement nobody made.
	decimals int
}

var headlines = []headline{
	{metric: "resting_heart_rate", label: "Resting heart rate", agg: mean, decimals: 0},
	{metric: "heart_rate", label: "Heart rate", agg: mean, decimals: 0},
	{metric: "hrv_sdnn", label: "HRV", agg: mean, decimals: 0},
	{metric: "vo2max", label: "VO2 max", agg: mean, decimals: 1},
	{metric: "spo2", label: "Blood oxygen", agg: mean, decimals: 0},
	{metric: "steps", label: "Steps", agg: perDay, decimals: 0},
	{metric: "active_calories", label: "Active calories", agg: perDay, decimals: 0},
	{metric: "body_fat_pct", label: "Body fat", agg: mean, decimals: 1},
}

// describe renders one metric's window as a sentence, or reports that there is
// nothing to say.
func (h headline) describe(stats metricStats, days int) (string, bool) {
	if stats.readings == 0 {
		return "", false
	}

	value := stats.average
	if h.agg == perDay {
		// Guarded even though readings > 0 implies days > 0: a divide by zero
		// here would render "+Inf steps" into somebody's prompt.
		if stats.days == 0 {
			return "", false
		}
		value = stats.total / float64(stats.days)
	}

	unit := stats.unit
	if unit == "count" {
		// The label already says what is being counted.
		unit = ""
	}

	text := fmt.Sprintf("%s — %s", h.label, formatNumber(value, h.decimals))
	if unit != "" {
		text += " " + unit
	}
	if h.agg == perDay {
		text += " per day"
	}
	return text + fmt.Sprintf(" (%s over %d days)", plural(int(stats.readings), "reading"), days), true
}

func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// formatNumber renders a value with thousands separators, because a coach
// reading "8432" and a person reading "8,432" should see the same number.
func formatNumber(value float64, decimals int) string {
	rounded := roundToString(value, decimals)

	whole, frac, hasFrac := strings.Cut(rounded, ".")
	negative := strings.HasPrefix(whole, "-")
	whole = strings.TrimPrefix(whole, "-")

	var grouped strings.Builder
	for i, digit := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			grouped.WriteByte(',')
		}
		grouped.WriteRune(digit)
	}

	out := grouped.String()
	if negative {
		out = "-" + out
	}
	if hasFrac {
		out += "." + frac
	}
	return out
}

func roundToString(value float64, decimals int) string {
	// Rounded away from zero so a half never disappears into the format verb's
	// banker's rounding, which would make two adjacent summaries disagree.
	factor := math.Pow(10, float64(decimals))
	rounded := math.Round(math.Abs(value)*factor) / factor
	if value < 0 {
		rounded = -rounded
	}
	return fmt.Sprintf("%.*f", decimals, rounded)
}
