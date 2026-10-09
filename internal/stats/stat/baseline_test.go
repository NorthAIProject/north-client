package stat_test

import (
	"math"
	"testing"

	"github.com/NorthAIProject/north-client/internal/stats/stat"
)

func TestBaselineBeforeUsesOnlyTheWindowBeforeTheDay(t *testing.T) {
	t.Parallel()
	var values []stat.DayValue
	// Sep 1-28: alternating 8,000 and 10,000 steps. Sep 29: a 20,000 day that
	// must not count towards its own baseline.
	for d := 1; d <= 28; d++ {
		v := 8000.0
		if d%2 == 0 {
			v = 10000
		}
		values = append(values, stat.DayValue{Day: day(d), Value: v})
	}
	values = append(values, stat.DayValue{Day: day(29), Value: 20000})

	b, ok := stat.BaselineBefore(values, day(29))
	if !ok || b.Days != 28 || b.Mean != 9000 {
		t.Fatalf("baseline %+v ok %v", b, ok)
	}
	if math.Abs(b.SD-1018.5) > 1 {
		t.Errorf("sd %v", b.SD)
	}
	if z, state := b.Place(20000); state != stat.Above || z < 10 {
		t.Errorf("20000 placed %v %s", z, state)
	}
	if _, state := b.Place(9500); state != stat.Usual {
		t.Errorf("9500 placed %s", state)
	}
	if _, state := b.Place(7000); state != stat.Below {
		t.Errorf("7000 placed %s", state)
	}

	// Day 29's own baseline ignores days older than the window.
	values = append([]stat.DayValue{{Day: day(1).AddDate(0, 0, -1), Value: 1e6}}, values...)
	if b2, _ := stat.BaselineBefore(values, day(29)); b2.Mean != 9000 {
		t.Errorf("a reading older than the window counted: %+v", b2)
	}
}

func TestBaselineNeedsAWeekOfDays(t *testing.T) {
	t.Parallel()
	var values []stat.DayValue
	for d := 1; d <= 6; d++ {
		values = append(values, stat.DayValue{Day: day(d), Value: 50})
	}
	if _, ok := stat.BaselineBefore(values, day(10)); ok {
		t.Error("six days made a baseline")
	}
	values = append(values, stat.DayValue{Day: day(7), Value: 50})
	b, ok := stat.BaselineBefore(values, day(10))
	if !ok {
		t.Fatal("seven days made no baseline")
	}
	// No spread at all: nothing can be called unusual.
	if z, state := b.Place(80); state != stat.Usual || z != 0 {
		t.Errorf("flat baseline placed 80 as %v %s", z, state)
	}
}
