// Package hevy reads the workout CSV that Hevy exports (Profile → Settings →
// Export & Import Data → Export Workouts): one row per set, the workout's
// title and times repeated on every row.
//
// It only parses. Matching exercise names to the catalog and writing
// sessions and sets belong to the lifts service, so this package can be
// tested against a file and nothing else.
package hevy

import (
	"bufio"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"
)

// Set kinds, as the lifts slice names them.
const (
	KindWarmup = "warmup"
	KindWork   = "work"
	KindDrop   = "drop"
)

// The bounds the lifts slice accepts. A row outside them is skipped and
// counted rather than failing the whole file: one typo in three years of
// history should not block the other ten thousand sets.
const (
	maxWeightKg = 600
	maxReps     = 100
	maxSetNo    = 50
	maxNameLen  = 120
	lbToKg      = 0.45359237
)

// Workout is one session from the export.
type Workout struct {
	Title string
	Start time.Time
	End   time.Time
	Sets  []Set
}

// Set is one lifted set. Rows with no reps — cardio, timed holds — are not
// sets in this sense and are counted in Parsed.Skipped instead.
type Set struct {
	Exercise string
	// Number counts the exercise's sets within the workout from 1, in the
	// order the file lists them.
	Number   int
	Kind     string
	WeightKg float64
	Reps     int
	// RIR is reps in reserve, from a failure set (0) or an RPE (10 − RPE);
	// nil when the row says neither.
	RIR *int
}

// Parsed is a whole export.
type Parsed struct {
	// Workouts in file order. Hevy writes newest first.
	Workouts []Workout
	// Skipped counts rows that are not importable sets: no reps (cardio,
	// timed), or a weight or rep count outside what the app accepts.
	Skipped int
}

// ErrNotHevy is a file that does not have Hevy's columns.
var ErrNotHevy = errors.New("this does not look like a Hevy workout export")

// required are the columns without which a row means nothing.
var required = []string{"title", "start_time", "exercise_title", "set_type", "reps"}

// timeLayouts are the start/end time formats Hevy has written: the app's
// "26 Mar 2024, 07:15", and an ISO-like fallback. Times have no zone; they
// are the person's local wall clock.
var timeLayouts = []string{"2 Jan 2006, 15:04", "2 Jan 2006 15:04", "2006-01-02 15:04:05", "2006-01-02T15:04:05", "2006-01-02 15:04"}

// Parse reads an export, reading wall-clock times in loc.
func Parse(r io.Reader, loc *time.Location) (Parsed, error) {
	reader := csv.NewReader(skipBOM(r))
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = true

	header, err := reader.Read()
	if err != nil {
		return Parsed{}, fmt.Errorf("%w: %v", ErrNotHevy, err)
	}
	col := columns(header)
	for _, name := range required {
		if _, ok := col[name]; !ok {
			return Parsed{}, fmt.Errorf("%w: no %q column", ErrNotHevy, name)
		}
	}
	_, hasKg := col["weight_kg"]
	_, hasLbs := col["weight_lbs"]
	if !hasKg && !hasLbs {
		return Parsed{}, fmt.Errorf("%w: no weight column", ErrNotHevy)
	}

	var out Parsed
	index := map[string]int{}   // title + start → position in out.Workouts
	numbers := map[string]int{} // workout + exercise → sets so far
	for line := 2; ; line++ {
		row, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Parsed{}, fmt.Errorf("line %d: %w", line, err)
		}
		get := func(name string) string {
			i, ok := col[name]
			if !ok || i >= len(row) {
				return ""
			}
			return strings.TrimSpace(row[i])
		}

		start, err := parseTime(get("start_time"), loc)
		if err != nil {
			return Parsed{}, fmt.Errorf("line %d: start_time: %w", line, err)
		}
		key := get("title") + "\x00" + get("start_time")
		w, seen := index[key]
		if !seen {
			end, err := parseTime(get("end_time"), loc)
			if err != nil || end.Before(start) {
				end = start
			}
			w = len(out.Workouts)
			index[key] = w
			out.Workouts = append(out.Workouts, Workout{Title: get("title"), Start: start, End: end})
		}

		set, ok := parseSet(get)
		if !ok {
			out.Skipped++
			continue
		}
		numberKey := key + "\x00" + strings.ToLower(set.Exercise)
		numbers[numberKey]++
		if numbers[numberKey] > maxSetNo {
			out.Skipped++
			continue
		}
		set.Number = numbers[numberKey]
		out.Workouts[w].Sets = append(out.Workouts[w].Sets, set)
	}
	return out, nil
}

// parseSet reads one row's set, false when it is not one the app can hold.
func parseSet(get func(string) string) (Set, bool) {
	name := get("exercise_title")
	if name == "" || len(name) > maxNameLen {
		return Set{}, false
	}
	reps, err := strconv.Atoi(get("reps"))
	if err != nil || reps < 1 || reps > maxReps {
		return Set{}, false
	}
	weight, ok := weightKg(get)
	if !ok || weight < 0 || weight > maxWeightKg {
		return Set{}, false
	}

	set := Set{Exercise: name, Reps: reps, WeightKg: weight, Kind: KindWork}
	switch strings.ToLower(get("set_type")) {
	case "warmup", "warm_up", "warm-up":
		set.Kind = KindWarmup
	case "dropset", "drop_set", "drop":
		set.Kind = KindDrop
	case "failure":
		zero := 0
		set.RIR = &zero
	}
	if set.RIR == nil {
		if rpe, err := strconv.ParseFloat(get("rpe"), 64); err == nil && rpe >= 1 && rpe <= 10 {
			rir := int(math.Round(10 - rpe))
			set.RIR = &rir
		}
	}
	return set, true
}

// weightKg reads the row's load in kilograms. An empty weight is bodyweight
// (0); a file exported in pounds is converted.
func weightKg(get func(string) string) (float64, bool) {
	if raw := get("weight_kg"); raw != "" {
		kg, err := strconv.ParseFloat(raw, 64)
		return math.Round(kg*10) / 10, err == nil
	}
	if raw := get("weight_lbs"); raw != "" {
		lbs, err := strconv.ParseFloat(raw, 64)
		return math.Round(lbs*lbToKg*10) / 10, err == nil
	}
	return 0, true
}

func parseTime(raw string, loc *time.Location) (time.Time, error) {
	for _, layout := range timeLayouts {
		if t, err := time.ParseInLocation(layout, raw, loc); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognised time %q", raw)
}

// columns indexes a header by lower-cased, trimmed name.
func columns(header []string) map[string]int {
	out := make(map[string]int, len(header))
	for i, h := range header {
		out[strings.ToLower(strings.TrimSpace(h))] = i
	}
	return out
}

// skipBOM drops a UTF-8 byte-order mark, which spreadsheet tools write and
// which would otherwise glue itself to the first column's quotes.
func skipBOM(r io.Reader) io.Reader {
	br := bufio.NewReader(r)
	if mark, err := br.Peek(3); err == nil && string(mark) == "\xef\xbb\xbf" {
		_, _ = br.Discard(3)
	}
	return br
}
