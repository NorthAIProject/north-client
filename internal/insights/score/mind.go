package score

import (
	"github.com/NorthAIProject/north-client/internal/checkins/checkin"
	"github.com/NorthAIProject/north-client/internal/mind/journal"
)

// Weights for the mind domain. They sum to 100 — see the weights test.
const (
	mindWeightCoverage = 40
	mindWeightMood     = 30
	mindWeightJournal  = 30
)

// What a well-tended window looks like, as a share of its days. Neither is a
// demand for perfection: checking in five days in seven and writing three
// times a week both earn full marks.
const (
	checkInTargetShare = 5.0 / 7.0
	journalTargetShare = 3.0 / 7.0
)

// moodScale is the 1-5 range check-ins record, as a span. A mood of 1 is the
// bottom of the scale, not zero, so the share is measured across the four
// steps between the ends.
const moodScale = 4.0

// Mind reads how often somebody checked in, how they rated it, and how much
// they wrote, over a window of windowDays.
//
// Every component is unknown until something is logged. Somebody who has never
// checked in has not had a bad month — they have had an unmeasured one, and
// scoring them Low would be a claim the data does not support.
func Mind(checkIns []checkin.CheckIn, entries []journal.Entry, windowDays int) Score {
	return New("mind", []Component{
		checkInCoverageComponent(checkIns, windowDays),
		moodComponent(checkIns),
		journalComponent(entries, windowDays),
	})
}

func checkInCoverageComponent(checkIns []checkin.CheckIn, windowDays int) Component {
	c := Component{Key: "checkin_coverage", Weight: mindWeightCoverage}
	if len(checkIns) == 0 || windowDays <= 0 {
		return c
	}

	c.Known = true
	share := float64(len(checkIns)) / float64(windowDays)
	c.Earned = award(c.Weight, share/checkInTargetShare)
	return c
}

func moodComponent(checkIns []checkin.CheckIn) Component {
	c := Component{Key: "mood", Weight: mindWeightMood}

	var total, n float64
	for _, ci := range checkIns {
		// Mood is 1-5 when recorded. A zero is a check-in filed without a
		// rating, which is a missing measurement rather than the worst one.
		if ci.Mood <= 0 {
			continue
		}
		total += float64(ci.Mood)
		n++
	}
	if n == 0 {
		return c
	}

	c.Known = true
	c.Earned = award(c.Weight, (total/n-1)/moodScale)
	return c
}

func journalComponent(entries []journal.Entry, windowDays int) Component {
	c := Component{Key: "journal", Weight: mindWeightJournal}
	if len(entries) == 0 || windowDays <= 0 {
		return c
	}

	c.Known = true
	share := float64(len(entries)) / float64(windowDays)
	c.Earned = award(c.Weight, share/journalTargetShare)
	return c
}
