package stat_test

import (
	"strings"
	"testing"
	"time"

	"github.com/FACorreiaa/go-utils/pkg/util"

	"github.com/NorthAIProject/north-client/internal/stats/stat"
)

func day(d int) time.Time { return time.Date(2026, 9, d, 0, 0, 0, 0, time.UTC) } // Sep 1 2026 is a Tuesday

func at(d, h, m int) *time.Time {
	t := time.Date(2026, 9, d, h, m, 0, 0, time.UTC)
	return &t
}

func TestSleepAveragesDebtAndConsistency(t *testing.T) {
	t.Parallel()
	nights := []stat.Night{
		{Date: day(2), Minutes: 420, Start: at(1, 23, 30), End: at(2, 6, 30), Stages: map[string]int{"deep": 60, "rem": 90, "core": 270, "awake": 20}},
		{Date: day(3), Minutes: 480, Start: at(3, 0, 30), End: at(3, 8, 30)},
		{Date: day(5), Minutes: 360, Start: at(4, 23, 0), End: at(5, 5, 0)}, // Saturday
	}
	st := stat.Sleep(nights, 480)
	if st.AvgMinutes != 420 || st.DebtMinutes != 60+120 || st.NightsOnTgt != 1 {
		t.Errorf("avg %d debt %d on target %d", st.AvgMinutes, st.DebtMinutes, st.NightsOnTgt)
	}
	if st.AvgBedtime != "23:40" { // 23:30, 00:30 and 23:00 average across midnight
		t.Errorf("avg bedtime %s, want 23:40", st.AvgBedtime)
	}
	if st.Best.Minutes != 480 || st.Worst.Minutes != 360 {
		t.Errorf("best %d worst %d", st.Best.Minutes, st.Worst.Minutes)
	}
	if st.WeekendAvg != 360 || st.WeekdayAvg != 450 {
		t.Errorf("weekend %d weekday %d", st.WeekendAvg, st.WeekdayAvg)
	}
	if st.StageShare["deep"] != 0.14 || st.StageShare["awake"] != 0 {
		t.Errorf("stage share %v", st.StageShare)
	}
}

func TestCardioPacesAndRecords(t *testing.T) {
	t.Parallel()
	st := stat.Cardio([]stat.Session{
		{Code: "running", Name: "Running", At: day(2), Seconds: 1800, DistanceM: 6000}, // 5:00/km
		{Code: "running", Name: "Running", At: day(9), Seconds: 1650, DistanceM: 5000}, // 5:30/km
		{Code: "cycling", Name: "Cycling", At: day(10), Seconds: 3600, DistanceM: 25000},
	}, day(1), day(15))
	if st.Sessions != 3 || st.DistanceKm != 36 || st.Runs.Count != 2 {
		t.Fatalf("totals %+v", st)
	}
	if st.Runs.BestPace != 300 || st.Runs.Best5K != 1500 || st.Runs.LongestKm != 6 {
		t.Errorf("runs %+v", st.Runs)
	}
	if st.ByKind[0].Name != "Cycling" || len(st.WeeklyKm) != 3 {
		t.Errorf("kinds %+v weekly %+v", st.ByKind, st.WeeklyKm)
	}
	if st.Recent[0].Name != "Cycling" {
		t.Error("recent is newest first")
	}
}

func TestEatingAdherenceAndSlots(t *testing.T) {
	t.Parallel()
	e := func(d, h int, label string, kcal, protein float64) stat.FoodEntry {
		return stat.FoodEntry{At: *at(d, h, 0), Date: day(d), Label: label, Kcal: kcal, Protein: protein}
	}
	st := stat.Eating([]stat.FoodEntry{
		e(1, 8, "Oats", 400, 20), e(1, 13, "Chicken", 800, 70), e(1, 19, "Salmon", 800, 60),
		e(2, 8, "Oats", 400, 20), e(2, 22, "Pizza", 1400, 40),
	}, 2000, 150, 80, 21)
	if st.DaysLogged != 2 || st.OnTargetDays != 2 || st.ProteinDays != 1 || st.LateDays != 1 {
		t.Errorf("adherence %+v", st)
	}
	if st.TopFoods[0].Label != "Oats" || st.TopFoods[0].Count != 2 {
		t.Errorf("top foods %+v", st.TopFoods)
	}
	if st.ProteinPerKg != 1.3 {
		t.Errorf("protein per kg %v", st.ProteinPerKg)
	}
	var late stat.Slot
	for _, s := range st.BySlot {
		if s.Key == "late" {
			late = s
		}
	}
	if late.Kcal != 1400 || late.Share != 0.37 {
		t.Errorf("late slot %+v", late)
	}
}

func TestPatternsNeedEnoughDaysAndRankByStrength(t *testing.T) {
	t.Parallel()
	var days []stat.DayFacts
	for d := 1; d <= 14; d++ {
		late := d%2 == 0
		f := stat.DayFacts{Date: day(d), LateCaffeine: late, HadCaffeine: true, Trained: d%3 == 0}
		// The night after a late-caffeine day is an hour shorter.
		sleep := 480
		if (d-1)%2 == 0 && d > 1 {
			sleep = 420
		}
		f.SleepMinutes = util.Ptr(sleep)
		mood := 3
		if f.Trained {
			mood = 4
		}
		f.Mood = util.Ptr(mood)
		days = append(days, f)
	}
	found := stat.Patterns(days)
	if len(found) < 2 {
		t.Fatalf("found %+v", found)
	}
	if found[0].Key != "sleep_late_caffeine" || found[0].Diff != -60 || !strings.Contains(found[0].Title, "1h 00m less") {
		t.Errorf("strongest = %+v", found[0])
	}
	var mood *stat.Finding
	for i := range found {
		if found[i].Key == "mood_training" {
			mood = &found[i]
		}
	}
	if mood == nil || mood.Diff != 1 {
		t.Errorf("mood finding %+v", mood)
	}
	if got := stat.Patterns(days[:4]); len(got) != 0 {
		t.Errorf("four days reported %+v", got)
	}
}
