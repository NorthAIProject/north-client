package score

import (
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/habits/habit"
	"github.com/NorthAIProject/north-client/internal/hydration/hydration"
	"github.com/NorthAIProject/north-client/internal/sleep/sleep"
)

func night(day int, minutes int, bedtime string) sleep.Log {
	return sleep.Log{
		LocalDate:       time.Date(2026, 9, day, 0, 0, 0, 0, time.UTC),
		DurationMinutes: minutes,
		Bedtime:         bedtime,
	}
}

func drank(day, ml int) hydration.Day {
	return hydration.Day{
		Date:     time.Date(2026, 9, day, 0, 0, 0, 0, time.UTC),
		TotalML:  ml,
		Entries:  1,
		TargetML: hydration.DefaultDailyTargetML,
	}
}

func component(t *testing.T, s Score, key string) Component {
	t.Helper()
	for _, c := range s.Components {
		if c.Key == key {
			return c
		}
	}
	t.Fatalf("no component %q in %+v", key, s.Components)
	return Component{}
}

func TestBodyScoresAWellLoggedWindow(t *testing.T) {
	nights := []sleep.Log{
		night(1, 480, "23:00"),
		night(2, 480, "23:00"),
		night(3, 480, "23:00"),
	}
	days := []hydration.Day{drank(1, 2000), drank(2, 2000), drank(3, 2000)}
	hs := []habit.Stats{{Kept: 3, Scheduled: 3}}

	got := Body(nights, days, hs, 3)

	if !got.HasData {
		t.Fatal("HasData = false, want true")
	}
	if got.Points != 100 {
		t.Errorf("Points = %d, want 100", got.Points)
	}
	if got.Coverage != 100 {
		t.Errorf("Coverage = %d, want 100", got.Coverage)
	}
}

func TestBodyWeightsSumToOneHundred(t *testing.T) {
	// Coverage is read as a percentage, which is only true while the weights
	// of a domain sum to 100. A reweighting that forgets this would silently
	// make every coverage figure wrong.
	total := 0
	for _, c := range Body(nil, nil, nil, 7).Components {
		total += c.Weight
	}
	if total != 100 {
		t.Errorf("weights sum to %d, want 100", total)
	}
}

func TestBodyWithNothingLoggedHasNoData(t *testing.T) {
	got := Body(nil, nil, nil, 7)

	if got.HasData {
		t.Errorf("HasData = true, want false (coverage %d)", got.Coverage)
	}
	if got.Points != 0 {
		t.Errorf("Points = %d, want 0", got.Points)
	}
}

func TestBodyMarksUnmeasuredComponentsUnknown(t *testing.T) {
	// Sleep logged without a bedtime, and no habits at all. Neither may be
	// counted as a zero.
	nights := []sleep.Log{night(1, 480, ""), night(2, 480, "")}
	days := []hydration.Day{drank(1, 2000), drank(2, 2000)}

	got := Body(nights, days, nil, 2)

	if c := component(t, got, "bedtime_consistency"); c.Known {
		t.Error("bedtime_consistency is Known with no bedtimes logged")
	}
	if c := component(t, got, "habits"); c.Known {
		t.Error("habits is Known with no habits tracked")
	}
	if got.Coverage != 65 {
		t.Errorf("Coverage = %d, want 65 (sleep 40 + hydration 25)", got.Coverage)
	}
	if got.Points != 100 {
		t.Errorf("Points = %d, want 100 — the measured parts were perfect", got.Points)
	}
}

func TestBodyShortSleepScoresBelowTheTargetBand(t *testing.T) {
	short := Body([]sleep.Log{night(1, 300, ""), night(2, 300, "")}, nil, nil, 2) // 5h
	good := Body([]sleep.Log{night(1, 480, ""), night(2, 480, "")}, nil, nil, 2)  // 8h

	shortC := component(t, short, "sleep_duration")
	goodC := component(t, good, "sleep_duration")

	if shortC.Earned >= goodC.Earned {
		t.Errorf("5h nights earned %d, 8h nights earned %d — want short to score lower",
			shortC.Earned, goodC.Earned)
	}
	if goodC.Earned != goodC.Weight {
		t.Errorf("8h nights earned %d of %d, want full marks", goodC.Earned, goodC.Weight)
	}
}

func TestBodyErraticBedtimeScoresBelowAConsistentOne(t *testing.T) {
	steady := Body([]sleep.Log{
		night(1, 480, "23:00"), night(2, 480, "23:10"), night(3, 480, "22:50"),
	}, nil, nil, 3)
	erratic := Body([]sleep.Log{
		night(1, 480, "21:00"), night(2, 480, "01:30"), night(3, 480, "23:45"),
	}, nil, nil, 3)

	steadyC := component(t, steady, "bedtime_consistency")
	erraticC := component(t, erratic, "bedtime_consistency")

	if !steadyC.Known || !erraticC.Known {
		t.Fatal("bedtime_consistency should be Known with three bedtimes logged")
	}
	if erraticC.Earned >= steadyC.Earned {
		t.Errorf("erratic earned %d, steady earned %d — want erratic to score lower",
			erraticC.Earned, steadyC.Earned)
	}
}
