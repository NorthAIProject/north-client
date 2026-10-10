package stat

import (
	"time"

	"github.com/FACorreiaa/go-utils/pkg/util"
)

// ActiveDay is one day of the consistency grid.
type ActiveDay struct {
	Day       time.Time
	Trained   bool // a finished workout or lifted sets
	CheckedIn bool
}

// Active reports whether anything happened that day.
func (d ActiveDay) Active() bool { return d.Trained || d.CheckedIn }

// WeekCount is how many days of a Monday-to-Sunday week were active.
type WeekCount struct {
	Start time.Time
	Days  int
}

// Consistency is how steadily someone shows up, over whole weeks: the day
// grid, active days per week, streaks, and the longest gap.
type Consistency struct {
	// Days runs from the Monday weeks-1 weeks back to today, oldest first.
	Days  []ActiveDay
	Weeks []WeekCount

	// CurrentStreak counts active days ending today, or ending yesterday
	// while today is still open.
	CurrentStreak int
	LongestStreak int
	// LongestGap is the most days in a row with nothing, between two
	// active days. A gap still open today does not count until it closes.
	LongestGap int

	BestWeek WeekCount
	ThisWeek int
	// UsualPerWeek averages the finished weeks, leaving out this one.
	UsualPerWeek float64
}

// ConsistencyOf reads which days were active over the last weeks weeks.
// trained and checkedIn are keyed by the start of each day; today is the
// start of today, in the same zone.
func ConsistencyOf(trained, checkedIn map[time.Time]bool, today time.Time, weeks int) Consistency {
	monday := today.AddDate(0, 0, -((int(today.Weekday()) + 6) % 7))
	first := monday.AddDate(0, 0, -7*(weeks-1))

	var c Consistency
	for d := first; !d.After(today); d = d.AddDate(0, 0, 1) {
		c.Days = append(c.Days, ActiveDay{Day: d, Trained: trained[d], CheckedIn: checkedIn[d]})
	}

	for w := 0; w < weeks; w++ {
		week := WeekCount{Start: first.AddDate(0, 0, 7*w)}
		for i := 7 * w; i < 7*w+7 && i < len(c.Days); i++ {
			if c.Days[i].Active() {
				week.Days++
			}
		}
		c.Weeks = append(c.Weeks, week)
		if week.Days > c.BestWeek.Days {
			c.BestWeek = week
		}
	}
	c.ThisWeek = c.Weeks[len(c.Weeks)-1].Days
	if finished := c.Weeks[:len(c.Weeks)-1]; len(finished) > 0 {
		total := 0
		for _, w := range finished {
			total += w.Days
		}
		c.UsualPerWeek = util.RoundHalfUpToScale(float64(total)/float64(len(finished)), 1)
	}

	run, gap, seenActive := 0, 0, false
	for _, d := range c.Days {
		if d.Active() {
			if seenActive {
				c.LongestGap = max(c.LongestGap, gap)
			}
			seenActive, gap = true, 0
			run++
			c.LongestStreak = max(c.LongestStreak, run)
		} else {
			run = 0
			gap++
		}
	}

	// Walk back from today; an empty today does not break the streak yet.
	for i := len(c.Days) - 1; i >= 0; i-- {
		if c.Days[i].Active() {
			c.CurrentStreak++
			continue
		}
		if i == len(c.Days)-1 {
			continue
		}
		break
	}
	return c
}
