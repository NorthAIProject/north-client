package planimport

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Cell parsers shared by the spreadsheet and JSON readers, and by the model's
// reply. Each takes what a person typed and either reads it exactly or leaves
// it empty with a flag that quotes the original. None of them guesses.

var (
	leadingInt     = regexp.MustCompile(`^(\d+)\s*(?:sets?)?$`)
	setsByReps     = regexp.MustCompile(`^(\d+)\s*[x×]\s*(.+)$`)
	restClock      = regexp.MustCompile(`^(\d+):([0-5]\d)$`)
	restWithUnit   = regexp.MustCompile(`^(\d+(?:[.,]\d+)?)\s*(s|sec|secs|second|seconds|m|min|mins|minute|minutes|'|")?$`)
	numberWithUnit = regexp.MustCompile(`^(\d+(?:[.,]\d+)?|\d+\s*/\s*\d+|\d+\s+\d+\s*/\s*\d+)\s*([a-zA-Z].*)?$`)
)

// parseSets reads a set count. "3" and "3 sets" are three sets. "3x10" is three
// sets of ten, returned as reps so the caller can fill an empty reps cell. A
// range such as "3-4" is not a number of sets anyone can do, so it is flagged
// rather than rounded.
func parseSets(raw string) (sets *int, reps string, flag string) {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return nil, "", ""
	}
	if m := leadingInt.FindStringSubmatch(raw); m != nil {
		n, _ := strconv.Atoi(m[1])
		return &n, "", ""
	}
	if m := setsByReps.FindStringSubmatch(raw); m != nil {
		n, _ := strconv.Atoi(m[1])
		return &n, strings.TrimSpace(m[2]), ""
	}
	return nil, "", fmt.Sprintf("Sets %q isn't a single number — enter one.", raw)
}

// parseRest reads a rest period into seconds: "90", "90s", "2 min", "1:30",
// "1.5 min". A bare number is seconds, which is how rest is written in every
// plan that does not say otherwise.
func parseRest(raw string) (*int, string) {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return nil, ""
	}
	if m := restClock.FindStringSubmatch(raw); m != nil {
		min, _ := strconv.Atoi(m[1])
		sec, _ := strconv.Atoi(m[2])
		n := min*60 + sec
		return &n, ""
	}
	if m := restWithUnit.FindStringSubmatch(raw); m != nil {
		v, _ := strconv.ParseFloat(strings.ReplaceAll(m[1], ",", "."), 64)
		switch m[2] {
		case "m", "min", "mins", "minute", "minutes", "'":
			v *= 60
		}
		n := int(math.Round(v))
		return &n, ""
	}
	return nil, fmt.Sprintf("Rest %q couldn't be read as a time.", raw)
}

// parseAmount reads "150", "150g", "1/2", "1 1/2 cups", "2,5 kg" into a number
// and whatever unit followed it.
func parseAmount(raw string) (*float64, string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, "", true
	}
	m := numberWithUnit.FindStringSubmatch(raw)
	if m == nil {
		return nil, "", false
	}
	v, ok := parseNumber(m[1])
	if !ok {
		return nil, "", false
	}
	return &v, strings.TrimSpace(m[2]), true
}

func parseNumber(s string) (float64, bool) {
	s = strings.TrimSpace(strings.ReplaceAll(s, ",", "."))
	whole := 0.0
	if parts := strings.Fields(s); len(parts) == 2 {
		w, err := strconv.ParseFloat(parts[0], 64)
		if err != nil {
			return 0, false
		}
		whole, s = w, parts[1]
	}
	if num, den, found := strings.Cut(s, "/"); found {
		n, err1 := strconv.ParseFloat(strings.TrimSpace(num), 64)
		d, err2 := strconv.ParseFloat(strings.TrimSpace(den), 64)
		if err1 != nil || err2 != nil || d == 0 {
			return 0, false
		}
		return whole + n/d, true
	}
	v, err := strconv.ParseFloat(s, 64)
	return whole + v, err == nil
}

// parseGrams reads a macro cell: "30", "30g", "30 g". Anything else is flagged.
func parseGrams(label, raw string) (*float64, string) {
	v, unit, ok := parseAmount(raw)
	if !ok || (unit != "" && !strings.EqualFold(unit, "g")) || (v != nil && *v < 0) {
		return nil, fmt.Sprintf("%s %q isn't a number of grams.", label, strings.TrimSpace(raw))
	}
	return v, ""
}

// weekdayNames maps every common way of writing a day to its time.Weekday.
var weekdayNames = func() map[string]time.Weekday {
	out := map[string]time.Weekday{}
	for d := time.Sunday; d <= time.Saturday; d++ {
		full := strings.ToLower(d.String())
		out[full] = d
		out[full[:3]] = d
	}
	for k, d := range map[string]time.Weekday{"tues": time.Tuesday, "weds": time.Wednesday, "thur": time.Thursday, "thurs": time.Thursday} {
		out[k] = d
	}
	return out
}()

// weekdayOf reads a day label that names a day of the week. "Day 1" or "Push"
// does not, and is left for the person to assign: day one of a plan is
// whatever day they start it.
func weekdayOf(label string) (time.Weekday, bool) {
	key := strings.Trim(strings.ToLower(strings.TrimSpace(label)), ".:")
	d, ok := weekdayNames[key]
	return d, ok
}

// dayKey compares day labels the way a person would: "monday", "Monday " and
// "MONDAY" are one day.
func dayKey(label string) string {
	return strings.Join(strings.Fields(strings.ToLower(label)), " ")
}
