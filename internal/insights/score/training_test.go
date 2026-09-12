package score

import (
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/activity/activity"
)

func trained(day int) activity.Session {
	return activity.Session{StartedAt: time.Date(2026, 9, day, 18, 0, 0, 0, time.UTC)}
}

func TestTrainingWeightsSumToOneHundred(t *testing.T) {
	total := 0
	for _, c := range Training(nil, 0, 0, 7).Components {
		total += c.Weight
	}
	if total != 100 {
		t.Errorf("weights sum to %d, want 100", total)
	}
}

func TestTrainingWithNoSessionsHasNoData(t *testing.T) {
	got := Training(nil, 0, 0, 7)

	if got.HasData {
		t.Errorf("HasData = true, want false (coverage %d)", got.Coverage)
	}
}

func TestTrainingScoresAConsistentWeek(t *testing.T) {
	sessions := []activity.Session{trained(1), trained(3), trained(5)}

	got := Training(sessions, 1200, 1000, 7)

	if !got.HasData {
		t.Fatal("HasData = false, want true")
	}
	if got.Points != 100 {
		t.Errorf("Points = %d, want 100", got.Points)
	}
}

func TestTrainingVolumeIsUnknownWithoutAPriorWindow(t *testing.T) {
	// No previous window means no comparison. The dashboard makes the same
	// call: no prior, no claim.
	got := Training([]activity.Session{trained(1), trained(3)}, 900, 0, 7)

	if component(t, got, "volume").Known {
		t.Error("volume is Known with no prior window to compare against")
	}
	if !component(t, got, "frequency").Known {
		t.Error("frequency should be Known — the sessions happened")
	}
}

func TestTrainingClusteredSessionsScoreBelowSpreadOnes(t *testing.T) {
	// Six sessions either way. Three in one week then a long silence is not
	// the same training month as one every few days.
	spread := []activity.Session{trained(1), trained(4), trained(7), trained(10), trained(13), trained(16)}
	clustered := []activity.Session{trained(1), trained(2), trained(3), trained(4), trained(5), trained(6)}

	spreadC := component(t, Training(spread, 0, 0, 28), "consistency")
	clusteredC := component(t, Training(clustered, 0, 0, 28), "consistency")

	if !spreadC.Known || !clusteredC.Known {
		t.Fatal("consistency should be Known with six sessions")
	}
	if clusteredC.Earned >= spreadC.Earned {
		t.Errorf("clustered earned %d, spread earned %d — want clustered to score lower",
			clusteredC.Earned, spreadC.Earned)
	}
}

func TestTrainingConsistencyNeedsTwoSessionDays(t *testing.T) {
	// One session is not a rhythm, and two on the same day are not either.
	got := Training([]activity.Session{trained(1), trained(1)}, 0, 0, 7)

	if component(t, got, "consistency").Known {
		t.Error("consistency is Known from a single day of training")
	}
}
