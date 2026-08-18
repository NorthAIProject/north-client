package notifications_test

import (
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/notifications"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

func at(hour, minute int) time.Time {
	return time.Date(2026, 8, 17, hour, minute, 0, 0, time.UTC)
}

func TestInQuietHoursAcrossMidnight(t *testing.T) {
	p := notifications.Prefs{QuietHoursEnabled: true, QuietStart: "22:00", QuietEnd: "07:00"}

	cases := []struct {
		when time.Time
		want bool
	}{
		{at(21, 59), false},
		{at(22, 0), true},
		{at(23, 30), true},
		{at(0, 0), true},
		{at(6, 59), true},
		{at(7, 0), false},
		{at(12, 0), false},
	}

	for _, c := range cases {
		if got := p.InQuietHours(c.when); got != c.want {
			t.Errorf("InQuietHours(%s) = %v, want %v", c.when.Format("15:04"), got, c.want)
		}
	}
}

// A window inside one day must not be read as its own inverse.
func TestInQuietHoursWithinOneDay(t *testing.T) {
	p := notifications.Prefs{QuietHoursEnabled: true, QuietStart: "09:00", QuietEnd: "17:00"}

	if !p.InQuietHours(at(12, 0)) {
		t.Error("midday should be inside 09:00-17:00")
	}
	if p.InQuietHours(at(8, 59)) || p.InQuietHours(at(17, 0)) {
		t.Error("edges of 09:00-17:00 read as inside")
	}
}

func TestInQuietHoursDisabled(t *testing.T) {
	p := notifications.Prefs{QuietHoursEnabled: false, QuietStart: "22:00", QuietEnd: "07:00"}
	if p.InQuietHours(at(23, 0)) {
		t.Error("a disabled window silenced a nudge")
	}
}

// A zero-value Prefs is what a caller gets from an unconfigured struct
// literal; it must not silence anything by accident.
func TestInQuietHoursZeroValue(t *testing.T) {
	var p notifications.Prefs
	if p.InQuietHours(at(3, 0)) {
		t.Error("zero-value prefs reported quiet hours")
	}
}

func TestAllowsNudge(t *testing.T) {
	p := notifications.Prefs{NudgeMissedCheckIn: true, NudgeGoalDeadline: false}

	if !p.AllowsNudge("missed_checkin") {
		t.Error("missed_checkin should be allowed")
	}
	if p.AllowsNudge("goal_deadline") {
		t.Error("goal_deadline should be refused")
	}
	// A kind with no switch on the settings page is not something anybody
	// asked to be silent about.
	if p.AllowsNudge("workout_today") {
		t.Fatal("zero TrainingReminders should refuse workout_today")
	}
	p.TrainingReminders = true
	if !p.AllowsNudge("workout_today") {
		t.Fatal("training reminders should allow workout_today")
	}
	p.CoachActivity = false
	if p.AllowsNudge("first_week_check") {
		t.Fatal("coach activity off should silence first-week notes")
	}
	if !p.AllowsNudge("some_future_kind") {
		t.Error("unknown kind should be allowed")
	}
}

func TestDefaultsMatchTheMigration(t *testing.T) {
	d := notifications.Defaults()

	if !d.NudgeMissedCheckIn || !d.NudgeGoalDeadline {
		t.Error("nudges should default on, as the sweep already behaves")
	}
	if d.WeeklyReportAuto {
		t.Error("the weekly review costs a model call; it must be opt-in")
	}
	if d.QuietHoursEnabled {
		t.Error("quiet hours should default off")
	}
	if d.QuietStart != "22:00" || d.QuietEnd != "07:00" {
		t.Errorf("quiet window = %s-%s, want 22:00-07:00", d.QuietStart, d.QuietEnd)
	}
}

func TestValidateRejectsMalformedTimes(t *testing.T) {
	_, err := notifications.Validate(notifications.Input{
		QuietHoursEnabled: true, QuietStart: "10pm", QuietEnd: "07:00",
	})

	var fieldErrs apperr.FieldErrors
	if !apperr.As(err, &fieldErrs) {
		t.Fatalf("expected field errors, got %v", err)
	}
	if fieldErrs.Messages()["quiet_start"] == "" {
		t.Fatalf("no message for quiet_start: %v", fieldErrs.Messages())
	}
}

func TestValidateRefusesAWindowOfNoLength(t *testing.T) {
	_, err := notifications.Validate(notifications.Input{
		QuietHoursEnabled: true, QuietStart: "22:00", QuietEnd: "22:00",
	})

	var fieldErrs apperr.FieldErrors
	if !apperr.As(err, &fieldErrs) {
		t.Fatalf("expected field errors, got %v", err)
	}
}

func TestValidateFillsEmptyTimes(t *testing.T) {
	in, err := notifications.Validate(notifications.Input{NudgeGoalDeadline: true})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if in.QuietStart != "22:00" || in.QuietEnd != "07:00" {
		t.Fatalf("times = %s-%s, want the defaults", in.QuietStart, in.QuietEnd)
	}
}
