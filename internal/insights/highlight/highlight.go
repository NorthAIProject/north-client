// Package highlight reads a window of charted data and says, in a sentence,
// what is worth noticing in it.
//
// A leaf of pure functions, so the same sentences can be produced for a web
// page and for a message without either importing the other.
//
// Every rule can decline. That is the design: a section that always finds
// three things to say about a flat week teaches people to stop reading it, so
// a rule that has nothing to report returns nothing rather than dressing up
// noise. Nothing here calls a model — an observation that costs a generation
// could not be shown on every page load, and one that changed between two
// renders of the same window would not be an observation.
package highlight

import (
	"fmt"
	"math"
	"sort"
)

// Series is one metric over a window, already bucketed for its chart.
type Series struct {
	Label string
	Unit  string

	// Decimals is how precise the number deserves to sound. Calories are
	// counted whole; the difference between 7.0h and 7.4h of sleep is the
	// whole point of the metric.
	Decimals int

	// Period is the singular noun for one bucket — "day", "week", "month".
	// Without it a run or a gap reads "unlogged for 10 in a row", and ten of
	// what is exactly what the reader wanted to know.
	Period string

	Values []float64
	Labels []string
}

// Comparison is one metric's total against the window before it.
type Comparison struct {
	Label   string
	Current float64
	Prior   float64
}

// Input is everything the rules read.
type Input struct {
	Series      []Series
	Comparisons []Comparison
}

// finding is a candidate sentence and how much it deserves the space.
type finding struct {
	text string
	rank float64
}

// Thresholds below which a rule says nothing. Each is the point where the
// observation stops being about the person and starts being about rounding.
const (
	// peakMargin is how far above the window's mean a bucket must stand.
	peakMargin = 1.25

	// minRun and minGap are in buckets.
	minRun = 3
	minGap = 3

	// minChange is the share a total must move against the prior window.
	minChange = 0.10
)

// Find returns at most limit sentences, most notable first.
func Find(in Input, limit int) []string {
	var found []finding

	for _, s := range in.Series {
		found = append(found, s.peak()...)
		found = append(found, s.run()...)
		found = append(found, s.gap()...)
	}
	for _, c := range in.Comparisons {
		found = append(found, c.change()...)
	}

	sort.SliceStable(found, func(i, j int) bool { return found[i].rank > found[j].rank })

	out := make([]string, 0, limit)
	for _, f := range found {
		if len(out) == limit {
			break
		}
		out = append(out, f.text)
	}
	return out
}

// peak names the standout bucket, when there is one.
func (s Series) peak() []finding {
	best, at := 0.0, -1
	var total float64
	var n int
	for i, v := range s.Values {
		if v > 0 {
			total += v
			n++
		}
		if v > best {
			best, at = v, i
		}
	}
	if n < 2 || at < 0 || at >= len(s.Labels) {
		return nil
	}

	mean := total / float64(n)
	if mean <= 0 || best < mean*peakMargin {
		return nil
	}

	return []finding{{
		text: fmt.Sprintf("Your best %s was %s, at %s.",
			lower(s.Label), s.Labels[at], s.amount(best)),
		rank: best / mean,
	}}
}

// run reports the longest unbroken stretch of logged buckets.
func (s Series) run() []finding {
	longest, current := 0, 0
	for _, v := range s.Values {
		if v > 0 {
			current++
			if current > longest {
				longest = current
			}
			continue
		}
		current = 0
	}
	if longest < minRun {
		return nil
	}

	return []finding{{
		text: fmt.Sprintf("You logged %s %d %s in a row.",
			lower(s.Label), longest, s.periods(longest)),
		rank: float64(longest),
	}}
}

// gap reports the longest stretch with nothing recorded.
func (s Series) gap() []finding {
	longest, current := 0, 0
	for _, v := range s.Values {
		if v == 0 {
			current++
			if current > longest {
				longest = current
			}
			continue
		}
		current = 0
	}
	if longest < minGap {
		return nil
	}

	return []finding{{
		text: fmt.Sprintf("%s went unlogged for %d %s.", s.Label, longest, s.periods(longest)),
		rank: float64(longest) / 2, // a gap is worth saying, but less than a run
	}}
}

// change compares a total with the window before it.
func (c Comparison) change() []finding {
	// No prior window means no claim, which is the rule the whole section
	// follows: a first week is not an improvement on anything.
	if c.Prior <= 0 {
		return nil
	}

	delta := (c.Current - c.Prior) / c.Prior
	if math.Abs(delta) < minChange {
		return nil
	}

	direction := "higher"
	if delta < 0 {
		direction = "lower"
	}
	return []finding{{
		text: fmt.Sprintf("%s is %.0f%% %s than the window before.",
			c.Label, math.Abs(delta)*100, direction),
		rank: math.Abs(delta) * 10,
	}}
}

// amount renders a value at the precision the metric deserves.
func (s Series) amount(v float64) string {
	return fmt.Sprintf("%.*f%s", s.Decimals, v, s.Unit)
}

// periods names n buckets: "3 days", "1 month". Falls back to "times" for a
// series that did not say what its buckets are, so the sentence still reads.
func (s Series) periods(n int) string {
	if s.Period == "" {
		return "times"
	}
	if n == 1 {
		return s.Period
	}
	return s.Period + "s"
}

// lower makes a label fit mid-sentence without lowercasing an acronym.
func lower(label string) string {
	if label == "" {
		return label
	}
	for _, r := range label[1:] {
		if r >= 'A' && r <= 'Z' {
			return label
		}
	}
	return string(label[0]|0x20) + label[1:]
}
