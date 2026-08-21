package settings

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/notifications"
	"github.com/NorthAIProject/north-client/internal/users"
)

func renderSettings(t *testing.T, profile ProfileForm, notif NotificationsForm) string {
	t.Helper()

	var b strings.Builder
	page := Page(
		users.User{DisplayName: "Test"},
		SettingsSummary{Timezone: profile.Timezone, Units: "metric"},
		profile,
		PreferencesForm{UnitsSystem: "metric"},
		notif,
		nil, nil, "", "",
	)
	if err := page.Render(context.Background(), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	return b.String()
}

// The timezone is a select, not a free-text field: a value Khepri cannot
// resolve is stored as UTC without complaint, so typing must not be possible.
func TestTimezoneRendersAsASelect(t *testing.T) {
	html := renderSettings(t, ProfileForm{Timezone: "Europe/Lisbon"}, NotificationsForm{})

	if strings.Contains(html, `name="timezone" type="text"`) {
		t.Error("timezone is still a text input")
	}
	for _, want := range []string{"Europe/Lisbon", "Pacific/Auckland", "America/New_York"} {
		if !strings.Contains(html, want) {
			t.Errorf("timezone select is missing %q", want)
		}
	}
}

// Somebody on a zone the shortlist forgot must still see their own setting.
func TestTimezoneSelectKeepsAnUnlistedZone(t *testing.T) {
	html := renderSettings(t, ProfileForm{Timezone: "America/Argentina/Ushuaia"}, NotificationsForm{})

	if !strings.Contains(html, "America/Argentina/Ushuaia") {
		t.Error("the stored zone is not offered, so saving would silently move this person")
	}
}

func TestToneSelectShowsEveryToneAndMarksTheChosenOne(t *testing.T) {
	html := renderSettings(t, ProfileForm{CoachingTone: users.ToneToughLove}, NotificationsForm{})

	for _, tone := range users.Tones {
		if !strings.Contains(html, tone.Label()) {
			t.Errorf("tone %q is missing from the select", tone)
		}
	}
	if !strings.Contains(html, `data-tui-selectbox-value="tough_love" data-tui-selectbox-selected="true"`) {
		t.Error("the chosen tone is not marked selected")
	}
	if strings.Contains(html, `data-tui-selectbox-value="direct" data-tui-selectbox-selected="true"`) {
		t.Error("the default tone is selected alongside the chosen one")
	}
}

// An account from before the column, or one with a value this build does not
// know, must still show a tone rather than an empty select.
func TestToneSelectFallsBackToTheDefault(t *testing.T) {
	for _, tone := range []users.Tone{"", "shouty"} {
		html := renderSettings(t, ProfileForm{CoachingTone: tone}, NotificationsForm{})
		if !strings.Contains(html, `data-tui-selectbox-value="direct" data-tui-selectbox-selected="true"`) {
			t.Errorf("tone %q: the default is not preselected", tone)
		}
	}
}

func TestNotificationsCardRendersEveryToggle(t *testing.T) {
	html := renderSettings(t, ProfileForm{}, NotificationsFormFor(notifications.Defaults(), notifications.DefaultPhoto(uuid.Nil)))

	for _, name := range []string{
		"nudge_missed_checkin", "nudge_goal_deadline",
		"weekly_report_auto", "quiet_hours_enabled",
		"quiet_start", "quiet_end",
		"photo_ask_enabled", "photo_every_days", "photo_reminder_days",
	} {
		if !strings.Contains(html, `name="`+name+`"`) {
			t.Errorf("notifications form is missing %q", name)
		}
	}
	if !strings.Contains(html, `action="/app/settings/notifications"`) {
		t.Error("notifications form posts somewhere unexpected")
	}
}

// A rejected quiet-hours time must come back with its message, and without
// losing the toggles the person just set.
func TestNotificationsCardShowsFieldErrors(t *testing.T) {
	html := renderSettings(t, ProfileForm{}, NotificationsForm{
		WeeklyReportAuto: true,
		QuietStart:       "10pm",
		Errors:           map[string]string{"quiet_start": "Use a time like 22:00."},
	})

	if !strings.Contains(html, "Use a time like 22:00.") {
		t.Error("the quiet_start error was not rendered")
	}
	if !strings.Contains(html, "10pm") {
		t.Error("the rejected value was not returned to the form")
	}
}
