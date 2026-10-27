package checkinreply

import (
	"fmt"
	"math"

	"github.com/NorthAIProject/north-client/internal/checkins/checkin"
)

const (
	// patternMinSide is how many check-ins each side of a comparison needs.
	// Two days against two days is a coincidence, not a pattern.
	patternMinSide = 3
	// patternMinGap is the smallest difference between two averages, on the
	// 1–5 scales, worth putting in front of somebody.
	patternMinGap = 0.75
)

// strongestPattern is the clearest difference in this person's check-ins, as
// one plain English sentence for the model to put in their language, or false
// when nothing is clear enough to name.
//
// Go finds the pattern and the model only phrases it. Asked to find one
// itself, a model finds one every time.
func strongestPattern(list []checkin.CheckIn) (string, bool) {
	best, bestGap := "", 0.0
	consider := func(sentence string, gap float64) {
		if math.Abs(gap) >= patternMinGap && math.Abs(gap) > math.Abs(bestGap) {
			best, bestGap = sentence, gap
		}
	}

	sleepHigh, sleepLow := split(list, func(c checkin.CheckIn) (bool, bool) { return rated(c.SleepQuality) })
	if a, b, ok := averages(sleepHigh, sleepLow, energyOf); ok {
		consider(fmt.Sprintf("On days they rated sleep 4 or 5, energy averaged %.1f; on days they rated it 1 or 2, %.1f.", a, b), a-b)
	}
	if a, b, ok := averages(sleepHigh, sleepLow, moodOf); ok {
		consider(fmt.Sprintf("On days they rated sleep 4 or 5, mood averaged %.1f; on days they rated it 1 or 2, %.1f.", a, b), a-b)
	}

	stressHigh, stressLow := split(list, func(c checkin.CheckIn) (bool, bool) { return rated(c.Stress) })
	if a, b, ok := averages(stressHigh, stressLow, moodOf); ok {
		consider(fmt.Sprintf("On high-stress days (4 or 5) mood averaged %.1f; on low-stress days (1 or 2), %.1f.", a, b), a-b)
	}

	if tag, ok := frequentTag(list); ok {
		with, without := split(list, func(c checkin.CheckIn) (bool, bool) {
			has := hasTag(c, tag)
			return has, !has
		})
		if a, b, ok := averages(with, without, moodOf); ok {
			consider(fmt.Sprintf("On days tagged %q mood averaged %.1f; on other days, %.1f.", tag, a, b), a-b)
		}
	}

	return best, best != ""
}

func moodOf(c checkin.CheckIn) int   { return c.Mood }
func energyOf(c checkin.CheckIn) int { return c.Energy }

// rated sorts an optional 1–5 rating into high (4–5) or low (1–2). A 3 and a
// missing rating are neither.
func rated(v *int) (high, low bool) {
	if v == nil {
		return false, false
	}
	return *v >= 4, *v <= 2
}

func split(list []checkin.CheckIn, side func(checkin.CheckIn) (bool, bool)) (a, b []checkin.CheckIn) {
	for _, c := range list {
		inA, inB := side(c)
		switch {
		case inA:
			a = append(a, c)
		case inB:
			b = append(b, c)
		}
	}
	return a, b
}

func averages(a, b []checkin.CheckIn, of func(checkin.CheckIn) int) (avgA, avgB float64, ok bool) {
	if len(a) < patternMinSide || len(b) < patternMinSide {
		return 0, 0, false
	}
	return mean(a, of), mean(b, of), true
}

func mean(list []checkin.CheckIn, of func(checkin.CheckIn) int) float64 {
	sum := 0
	for _, c := range list {
		sum += of(c)
	}
	return float64(sum) / float64(len(list))
}

// frequentTag is the tag on the most check-ins, if it is on enough of them to
// compare. Ties go to the tag seen first, newest check-in first.
func frequentTag(list []checkin.CheckIn) (string, bool) {
	counts := map[string]int{}
	var order []string
	for _, c := range list {
		for _, tag := range c.Tags {
			if counts[tag] == 0 {
				order = append(order, tag)
			}
			counts[tag]++
		}
	}
	best := ""
	for _, tag := range order {
		if counts[tag] > counts[best] {
			best = tag
		}
	}
	return best, counts[best] >= patternMinSide
}

func hasTag(c checkin.CheckIn, tag string) bool {
	for _, t := range c.Tags {
		if t == tag {
			return true
		}
	}
	return false
}
