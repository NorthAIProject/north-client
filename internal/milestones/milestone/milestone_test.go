package milestone

import (
	"testing"
	"time"
)

func TestMonthsSinceCountsWholeMonths(t *testing.T) {
	d := func(y, m, day int) time.Time { return time.Date(y, time.Month(m), day, 0, 0, 0, 0, time.UTC) }
	six := 6
	tr := Tracker{LastDoneOn: d(2026, 3, 15), IntervalMonths: &six}
	cases := map[time.Time]int{d(2026, 3, 20): 0, d(2026, 4, 14): 0, d(2026, 4, 15): 1, d(2026, 9, 26): 6, d(2027, 1, 1): 9}
	for on, want := range cases {
		if got := tr.MonthsSince(on); got != want {
			t.Errorf("MonthsSince(%s) = %d, want %d", on.Format("2006-01-02"), got, want)
		}
	}
	if !tr.Due(d(2026, 9, 26)) || tr.Due(d(2026, 8, 1)) {
		t.Error("due should flip at six months")
	}
	if tr.Fraction(d(2027, 6, 1)) != 1 {
		t.Error("fraction caps at 1")
	}
}
