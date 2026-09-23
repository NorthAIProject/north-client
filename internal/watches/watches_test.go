package watches_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/watches"
)

func TestDailyNextIsTodayIfStillAheadElseTomorrow(t *testing.T) {
	lisbon, _ := time.LoadLocation("Europe/Lisbon")
	s := watches.Schedule{Cadence: watches.CadenceDaily, Minute: 8 * 60}

	before := time.Date(2026, 9, 23, 7, 30, 0, 0, lisbon)
	if got := s.Next(before, lisbon); !got.Equal(time.Date(2026, 9, 23, 8, 0, 0, 0, lisbon)) {
		t.Errorf("before 8:00: next = %v", got)
	}

	// Exactly at the slot is not "after" it: the run that just happened must
	// not be scheduled again for the same instant.
	at := time.Date(2026, 9, 23, 8, 0, 0, 0, lisbon)
	if got := s.Next(at, lisbon); !got.Equal(time.Date(2026, 9, 24, 8, 0, 0, 0, lisbon)) {
		t.Errorf("at 8:00: next = %v", got)
	}
}

func TestWeeklyNextLandsOnTheWeekday(t *testing.T) {
	s := watches.Schedule{Cadence: watches.CadenceWeekly, Weekday: time.Monday, Minute: 18*60 + 30}

	// Wednesday 23 Sept 2026 → Monday 28 Sept.
	wed := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	want := time.Date(2026, 9, 28, 18, 30, 0, 0, time.UTC)
	if got := s.Next(wed, time.UTC); !got.Equal(want) {
		t.Errorf("next = %v, want %v", got, want)
	}

	// On the Monday itself, after the slot: a week later.
	mondayLate := time.Date(2026, 9, 28, 19, 0, 0, 0, time.UTC)
	if got := s.Next(mondayLate, time.UTC); !got.Equal(want.AddDate(0, 0, 7)) {
		t.Errorf("monday after the slot: next = %v", got)
	}
}

func TestNextKeepsLocalTimeAcrossADSTChange(t *testing.T) {
	lisbon, _ := time.LoadLocation("Europe/Lisbon")
	s := watches.Schedule{Cadence: watches.CadenceDaily, Minute: 8 * 60}

	// Clocks go back on 25 Oct 2026. 8:00 is still 8:00 the day after.
	got := s.Next(time.Date(2026, 10, 25, 9, 0, 0, 0, lisbon), lisbon)
	if got.In(lisbon).Hour() != 8 || got.In(lisbon).Day() != 26 {
		t.Errorf("next = %v, want 26 Oct 8:00 local", got.In(lisbon))
	}
}

func TestParseProposal(t *testing.T) {
	p, err := watches.ParseProposal(json.RawMessage(`{"watch":"your training week","notify_when":"you missed two sessions",
		"spec":"Review the week.","cadence":"weekly","weekday":"sun","time":"19:05"}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if p.Schedule.Cadence != watches.CadenceWeekly || p.Schedule.Weekday != time.Sunday || p.Schedule.Minute != 19*60+5 {
		t.Errorf("schedule = %+v", p.Schedule)
	}
	if p.Schedule.Clock() != "19:05" {
		t.Errorf("clock = %q", p.Schedule.Clock())
	}
	if got := p.Confirmation(); got != "Got it — I'll watch your training week and ping you when you missed two sessions." {
		t.Errorf("confirmation = %q", got)
	}
}

func TestParseProposalRefusesWhatCannotBeScheduled(t *testing.T) {
	for name, raw := range map[string]string{
		"no time":        `{"watch":"x","notify_when":"y","spec":"z","cadence":"daily","time":""}`,
		"bad time":       `{"watch":"x","notify_when":"y","spec":"z","cadence":"daily","time":"25:00"}`,
		"weekly no day":  `{"watch":"x","notify_when":"y","spec":"z","cadence":"weekly","weekday":"","time":"08:00"}`,
		"hourly":         `{"watch":"x","notify_when":"y","spec":"z","cadence":"hourly","time":"08:00"}`,
		"missing fields": `{"cadence":"daily","time":"08:00"}`,
		"not json":       `nope`,
	} {
		if _, err := watches.ParseProposal(json.RawMessage(raw)); err == nil {
			t.Errorf("%s: parsed, want a refusal", name)
		}
	}
}
