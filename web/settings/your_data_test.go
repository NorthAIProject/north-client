package settings

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/meals"
	"github.com/NorthAIProject/north-client/internal/users"
)

// renderSettingsFor is the account-card view of this page. Named apart from
// profile_test's renderSettings because the two fix different arguments: that
// one varies the forms for one user, this one varies the user and the deletion
// refusal.
func renderSettingsFor(t *testing.T, user users.User, deleteErr string) string {
	t.Helper()

	var b strings.Builder
	page := Page(
		user,
		SettingsSummary{},
		ProfileFormFor(user),
		PreferencesForm{},
		NotificationsForm{},
		[]meals.Diet{},
		map[uuid.UUID]bool{},
		"",
		deleteErr,
	)
	if err := page.Render(context.Background(), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	return b.String()
}

// Both halves of the promise have to be reachable from the page a person goes
// to when they want out. An export nobody can find is not an export.
func TestSettingsOffersBothLeavingWithYourDataAndLeaving(t *testing.T) {
	html := renderSettingsFor(t, users.User{DisplayName: "Sam", Email: "sam@north.test"}, "")

	for _, want := range []string{
		"/app/settings/export.zip",
		"/app/settings/account/delete",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("settings page does not link %s", want)
		}
	}
}

// The address is what the person has to type, so it has to be on the page in
// front of them — a confirmation you cannot read is a guessing game.
func TestTheDeleteDialogShowsTheAddressToType(t *testing.T) {
	html := renderSettingsFor(t, users.User{DisplayName: "Sam", Email: "sam@north.test"}, "")

	if !strings.Contains(html, "sam@north.test") {
		t.Error("the delete dialog does not show the account's email address")
	}
	if !strings.Contains(html, `name="confirm_email"`) {
		t.Error("the delete form has no confirmation field")
	}
	if !strings.Contains(html, "csrf_token") {
		t.Error("the delete form is not CSRF-protected")
	}
}

func TestAFailedConfirmationIsShownBackToThePerson(t *testing.T) {
	html := renderSettingsFor(t,
		users.User{DisplayName: "Sam", Email: "sam@north.test"},
		"Type your email address exactly as it appears above to confirm.")

	if !strings.Contains(html, "Type your email address exactly") {
		t.Error("a rejected deletion says nothing about why")
	}
}
