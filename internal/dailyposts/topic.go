package dailyposts

import (
	"fmt"
	"slices"
	"time"
)

// Theme is one thing a post can be about. Brief steers the model toward the
// everyday, practical side of it, which is where the app lives.
type Theme struct {
	Key   string
	Brief string
}

// themes is the rotation. Everyday habits a person can act on today, nothing
// that needs a clinician to interpret.
var themes = []Theme{
	{"sleep", "getting enough sleep and a steadier bedtime"},
	{"hydration", "drinking enough water through the day"},
	{"protein", "spreading protein across meals from ordinary foods"},
	{"fibre", "vegetables, fruit, beans and whole grains on the plate"},
	{"walking", "walking more in an ordinary day"},
	{"mobility", "a few minutes of moving joints through their range"},
	{"strength", "keeping strength training simple and regular"},
	{"stress", "noticing stress and taking a short pause"},
	{"recovery", "rest days and easier sessions as part of training"},
	{"meal-prep", "planning or preparing a few meals ahead"},
	{"mindfulness", "a few minutes of quiet attention"},
	{"consistency", "showing up on an ordinary day rather than a perfect one"},
	{"screens", "screens late in the evening and winding down"},
	{"social", "time with friends and family as part of wellbeing"},
	{"daylight", "getting outside and into daylight"},
	{"cooking", "cooking simple meals at home"},
	{"snacks", "snacks that fit the day rather than fight it"},
	{"posture", "breaking up long stretches of sitting"},
}

const (
	// repeatWindow is how many earlier posts a theme must not appear in.
	repeatWindow = 14
	// signalRepeatWindow is shorter: what people are going through this week
	// may bring a theme back sooner than the rotation would.
	signalRepeatWindow = 3
	// minSignalPeople is how many people a weekly figure must come from before
	// a post leans on it. Below it, an average describes a handful of people
	// and says more about them than about anyone.
	minSignalPeople = 10
)

// Aggregate is everyone's logs over one week, as counts and averages only.
type Aggregate struct {
	CheckInPeople   int
	AvgEnergy       float64
	AvgMood         float64
	SleepPeople     int
	AvgSleepMinutes float64
}

// Signal is something the week's logs suggest the post could speak to.
type Signal struct {
	Theme string
	// Note is the line the model reads. Never a number of people, never anyone.
	Note string
}

// SignalFrom reads the week. ok is false when nothing stands out or too few
// people logged for an average to mean anything.
func SignalFrom(a Aggregate) (Signal, bool) {
	if a.SleepPeople >= minSignalPeople && a.AvgSleepMinutes > 0 && a.AvgSleepMinutes < 6.5*60 {
		return Signal{Theme: "sleep", Note: "Across the app this week, people logged shorter nights than usual."}, true
	}
	if a.CheckInPeople >= minSignalPeople && a.AvgEnergy > 0 && a.AvgEnergy < 2.75 {
		return Signal{Theme: "recovery", Note: "Across the app this week, people's check-ins report low energy."}, true
	}
	if a.CheckInPeople >= minSignalPeople && a.AvgMood > 0 && a.AvgMood < 2.75 {
		return Signal{Theme: "stress", Note: "Across the app this week, people's check-ins report a heavier mood than usual."}, true
	}
	return Signal{}, false
}

// Topic is what one post is about.
type Topic struct {
	Theme Theme
	// Moment is the day in words: the weekday, and a calendar hook when there
	// is one. Never the weather or a season, because readers live on both
	// sides of the equator.
	Moment string
	// Signal is empty unless the week's logs chose the theme.
	Signal string
}

// PickTopic chooses the day's theme. A signal wins when its theme has not
// been used in the last few posts; otherwise the rotation picks a theme not
// used in the last two weeks. skip holds themes already tried today.
// Deterministic for a given day, so a retry or a test sees the same choice.
func PickTopic(day time.Time, earlier []Earlier, signal *Signal, skip []string) Topic {
	topic := Topic{Moment: moment(day)}

	if signal != nil && !slices.Contains(skip, signal.Theme) && !usedIn(earlier, signal.Theme, signalRepeatWindow) {
		if t, ok := themeByKey(signal.Theme); ok {
			topic.Theme = t
			topic.Signal = signal.Note
			return topic
		}
	}

	var candidates []Theme
	for _, t := range themes {
		if !slices.Contains(skip, t.Key) && !usedIn(earlier, t.Key, repeatWindow) {
			candidates = append(candidates, t)
		}
	}
	if len(candidates) == 0 {
		// Every theme was used in the window: anything but yesterday's.
		for _, t := range themes {
			if !slices.Contains(skip, t.Key) && !usedIn(earlier, t.Key, 1) {
				candidates = append(candidates, t)
			}
		}
	}
	if len(candidates) == 0 {
		candidates = themes
	}
	topic.Theme = candidates[day.YearDay()%len(candidates)]
	return topic
}

func usedIn(earlier []Earlier, key string, window int) bool {
	for i, e := range earlier {
		if i >= window {
			return false
		}
		if e.Theme == key {
			return true
		}
	}
	return false
}

func themeByKey(key string) (Theme, bool) {
	for _, t := range themes {
		if t.Key == key {
			return t, true
		}
	}
	return Theme{}, false
}

func moment(day time.Time) string {
	s := day.Weekday().String()
	switch {
	case day.Month() == time.January && day.Day() <= 7:
		s += ", the first week of the year"
	case day.Month() == time.December && day.Day() >= 20:
		s += ", the end-of-year holidays"
	case day.Weekday() == time.Monday:
		s += ", the start of the week"
	case day.Weekday() == time.Friday:
		s += ", heading into the weekend"
	case day.Weekday() == time.Saturday || day.Weekday() == time.Sunday:
		s += ", the weekend"
	}
	return fmt.Sprintf("%s, %s", s, day.Format("2 January"))
}
