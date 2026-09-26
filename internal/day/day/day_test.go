package day

import (
	"testing"
	"time"
)

func TestParseClock(t *testing.T) {
	cases := map[string]int{"00:00": 0, "20:00": 1200, "23:59": 1439, "07:05": 425}
	for in, want := range cases {
		got, err := ParseClock(in)
		if err != nil || got != want {
			t.Errorf("ParseClock(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "7:00", "24:00", "12:60", "ab:cd", "12-00"} {
		if _, err := ParseClock(bad); err == nil {
			t.Errorf("ParseClock(%q) accepted a bad time", bad)
		}
	}
}

func TestRuleValidateAndPlace(t *testing.T) {
	if err := (Rule{Kind: "nap_time", At: "13:00"}).Validate(); err == nil {
		t.Error("unknown kind was accepted")
	}
	r := Rule{Kind: RuleKitchenCloses, At: "20:00", Enabled: true}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	loc := time.FixedZone("x", 3600)
	got := r.On(time.Date(2026, 9, 26, 15, 4, 0, 0, loc))
	want := time.Date(2026, 9, 26, 20, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Errorf("On = %v, want %v", got, want)
	}
}

func TestRingCapsFractionButNotPercent(t *testing.T) {
	r := Ring{Value: 750, Goal: 500}
	if r.Fraction() != 1 {
		t.Errorf("Fraction = %v, want 1", r.Fraction())
	}
	if r.Percent() != 150 {
		t.Errorf("Percent = %d, want 150", r.Percent())
	}
	if (Ring{Value: 10}).Fraction() != 0 {
		t.Error("a ring without a goal must read empty, not divide by zero")
	}
}

func TestCategoryFor(t *testing.T) {
	cases := map[float64]BMICategory{17: BMIUnderweight, 22: BMIHealthy, 25.6: BMIOverweight, 31: BMIObese}
	for bmi, want := range cases {
		if got := CategoryFor(bmi); got != want {
			t.Errorf("CategoryFor(%v) = %s, want %s", bmi, got, want)
		}
	}
}

func TestSleepFromBlocksExcludesAwake(t *testing.T) {
	at := func(h, m int) time.Time { return time.Date(2026, 9, 26, h, m, 0, 0, time.UTC) }
	blocks := []SleepBlock{
		{Stage: StageCore, Start: at(1, 0), End: at(3, 0)},
		{Stage: StageAwake, Start: at(3, 0), End: at(3, 20)},
		{Stage: StageDeep, Start: at(0, 0), End: at(1, 0)},
		{Stage: StageREM, Start: at(3, 20), End: at(4, 0)},
	}
	s, ok := SleepFromBlocks(blocks, "apple_health")
	if !ok {
		t.Fatal("expected a night")
	}
	if s.TotalMinutes != 60+120+40 {
		t.Errorf("TotalMinutes = %d, want 220", s.TotalMinutes)
	}
	if s.StageMinutes[StageAwake] != 20 {
		t.Errorf("awake = %d, want 20", s.StageMinutes[StageAwake])
	}
	if !s.Start.Equal(at(0, 0)) || !s.End.Equal(at(4, 0)) {
		t.Errorf("span = %v-%v", s.Start, s.End)
	}
	if s.Blocks[0].Stage != StageDeep {
		t.Error("blocks should be sorted by start")
	}
	if _, ok := SleepFromBlocks(nil, ""); ok {
		t.Error("no blocks must mean no night")
	}
}

func TestEnergy(t *testing.T) {
	now := time.Date(2026, 9, 26, 14, 46, 0, 0, time.UTC)
	if _, ok := Energy(EnergyInputs{Now: now}); ok {
		t.Error("no signals must mean no estimate")
	}

	full := 8 * 60
	woke := time.Date(2026, 9, 26, 8, 0, 0, 0, time.UTC)
	rested, _ := Energy(EnergyInputs{SleepMinutes: &full, WokeAt: &woke, Now: woke.Add(time.Hour)})
	if rested < 80 {
		t.Errorf("a full night, an hour after waking, gave %d%%", rested)
	}

	short := 5 * 60
	tired, _ := Energy(EnergyInputs{SleepMinutes: &short, WokeAt: &woke, Now: now, ActiveKcal: 500})
	if tired >= rested {
		t.Errorf("a short night and an afternoon (%d%%) should be below a rested morning (%d%%)", tired, rested)
	}

	late := time.Date(2026, 9, 27, 3, 0, 0, 0, time.UTC)
	if drained, _ := Energy(EnergyInputs{SleepMinutes: &short, WokeAt: &woke, Now: late, ActiveKcal: 2000}); drained != 0 {
		t.Errorf("energy must floor at 0, got %d", drained)
	}
}

func TestFormatMinutes(t *testing.T) {
	cases := map[int]string{45: "45m", 60: "1h", 433: "7h 13m"}
	for in, want := range cases {
		if got := FormatMinutes(in); got != want {
			t.Errorf("FormatMinutes(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestLevelAndGoal(t *testing.T) {
	if LevelFor(0) != 1 || LevelFor(91) != 19 {
		t.Errorf("levels: %d, %d", LevelFor(0), LevelFor(91))
	}
	w, target := 83.9, 80.0
	if d, ok := (Body{WeightKg: &w, TargetWeightKg: &target}).ToGoal(); !ok || d != 3.9 {
		t.Errorf("to goal = %v, %v", d, ok)
	}
	if _, ok := (Body{WeightKg: &w}).ToGoal(); ok {
		t.Error("no target, no distance")
	}
}
