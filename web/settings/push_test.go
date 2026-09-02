package settings

import (
	"strings"
	"testing"
)

// The row is the one control on the settings page whose state the server
// cannot know. What the server does know — whether push is configured at all
// — decides whether the row, its key, and its script are rendered.
func TestPushRowRendersOnlyWhenPushIsConfigured(t *testing.T) {
	off := renderSettings(t, ProfileForm{Timezone: "UTC"}, NotificationsForm{})
	for _, unwanted := range []string{`data-push`, `push.js`, `Nudges on this device`} {
		if strings.Contains(off, unwanted) {
			t.Errorf("push row rendered without keys: found %q", unwanted)
		}
	}

	on := renderSettings(t, ProfileForm{Timezone: "UTC"}, NotificationsForm{
		PushEnabled:   true,
		PushPublicKey: "BPublicKeyForTheBrowser",
		PushDevices:   2,
	})
	for _, want := range []string{
		`data-push-key="BPublicKeyForTheBrowser"`,
		`data-csrf=`,
		`data-push-enable`,
		`data-push-disable`,
		`data-push-hint`,
		`/assets/js/shared/push.js`,
		`2 devices get nudges.`,
	} {
		if !strings.Contains(on, want) {
			t.Errorf("push row missing %q", want)
		}
	}
}

// The row is not a field. Rendering it inside the notifications form would
// make "Save notifications" post the push buttons along with the toggles.
func TestPushRowSitsOutsideTheNotificationsForm(t *testing.T) {
	html := renderSettings(t, ProfileForm{Timezone: "UTC"}, NotificationsForm{PushEnabled: true, PushPublicKey: "k"})

	form := strings.Index(html, `action="/app/settings/notifications"`)
	end := strings.Index(html[form:], `</form>`) + form
	row := strings.Index(html, `data-push-key=`)
	if form < 0 || end < 0 || row < 0 {
		t.Fatal("could not find the form and the row")
	}
	if row < end {
		t.Fatal("push row is inside the notifications form")
	}
}
