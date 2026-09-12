package insights

import (
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/notifications"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
)

func at(t *testing.T, spec string) time.Time {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Lisbon")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	when, err := time.ParseInLocation("2006-01-02 15:04", spec, loc)
	if err != nil {
		t.Fatalf("parse %q: %v", spec, err)
	}
	return when
}

func TestDigestIsNotDueBeforeTheMorning(t *testing.T) {
	// 2026-09-14 is a Monday.
	if _, _, ok := due(at(t, "2026-09-14 07:59"), notifications.CadenceDaily); ok {
		t.Error("a daily digest fired before the morning hour")
	}
}

func TestDailyDigestCoversTheDayThatClosed(t *testing.T) {
	key, period, ok := due(at(t, "2026-09-14 08:00"), notifications.CadenceDaily)

	if !ok {
		t.Fatal("a daily digest is not due at the morning hour")
	}
	// Yesterday, not today: a digest sent at 08:00 about today would be a
	// report on eight hours, most of them asleep.
	if key != timerange.KeyYesterday {
		t.Errorf("key = %q, want %q", key, timerange.KeyYesterday)
	}
	if got := period.Format("2006-01-02"); got != "2026-09-14" {
		t.Errorf("period = %s, want the day it was sent", got)
	}
}

func TestWeeklyDigestIsDueOnMonday(t *testing.T) {
	key, period, ok := due(at(t, "2026-09-14 09:00"), notifications.CadenceWeekly)

	if !ok {
		t.Fatal("a weekly digest is not due on Monday morning")
	}
	if key != timerange.KeyWeek {
		t.Errorf("key = %q, want %q", key, timerange.KeyWeek)
	}
	if got := period.Format("2006-01-02"); got != "2026-09-14" {
		t.Errorf("period = %s, want the Monday", got)
	}
}

func TestWeeklyDigestStillCatchesUpMidweek(t *testing.T) {
	// A worker that was down all Monday must not skip the week. The gate is
	// loose on purpose; the ledger is what stops a second send, and it can
	// only do that if every day in the window claims the same period.
	monday, _, _ := due(at(t, "2026-09-14 09:00"), notifications.CadenceWeekly)
	_ = monday

	for _, day := range []string{"2026-09-15 09:00", "2026-09-16 09:00"} {
		_, period, ok := due(at(t, day), notifications.CadenceWeekly)
		if !ok {
			t.Errorf("%s: not due", day)
			continue
		}
		if got := period.Format("2006-01-02"); got != "2026-09-14" {
			t.Errorf("%s: period = %s, want the same Monday", day, got)
		}
	}
}

func TestWeeklyDigestStopsBeingDueLaterInTheWeek(t *testing.T) {
	// Thursday onward is a week nobody is catching up on any more; sending
	// then would arrive closer to the next week than to the one it describes.
	if _, _, ok := due(at(t, "2026-09-17 09:00"), notifications.CadenceWeekly); ok {
		t.Error("a weekly digest is still due on Thursday")
	}
}

func TestMonthlyDigestIsDueInTheFirstDays(t *testing.T) {
	for _, day := range []string{"2026-10-01 09:00", "2026-10-02 09:00", "2026-10-03 09:00"} {
		key, period, ok := due(at(t, day), notifications.CadenceMonthly)
		if !ok {
			t.Errorf("%s: not due", day)
			continue
		}
		if key != timerange.KeyMonth {
			t.Errorf("%s: key = %q, want %q", day, key, timerange.KeyMonth)
		}
		if got := period.Format("2006-01-02"); got != "2026-10-01" {
			t.Errorf("%s: period = %s, want the first of the month", day, got)
		}
	}
}

func TestMonthlyDigestStopsBeingDueAfterTheFirstDays(t *testing.T) {
	if _, _, ok := due(at(t, "2026-10-04 09:00"), notifications.CadenceMonthly); ok {
		t.Error("a monthly digest is still due on the fourth")
	}
}

func TestDigestIsNeverDueWhenSwitchedOff(t *testing.T) {
	for _, when := range []string{"2026-09-14 09:00", "2026-10-01 09:00"} {
		if _, _, ok := due(at(t, when), notifications.CadenceOff); ok {
			t.Errorf("%s: a switched-off digest is due", when)
		}
	}
}

func TestUnknownCadenceIsNeverDue(t *testing.T) {
	// A cadence this build stops understanding must go quiet, not fire hourly.
	if _, _, ok := due(at(t, "2026-09-14 09:00"), "fortnightly"); ok {
		t.Error("an unknown cadence is due")
	}
}
