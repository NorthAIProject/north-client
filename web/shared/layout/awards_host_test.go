package layout

import (
	"context"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/users"
)

// The unlock moment depends on three strings agreeing with other files: the
// endpoint, the htmx 4 event name (not htmx 2's afterRequest) and the dialog
// id in web/awards. A rename in any of them silently ends the celebration.
func TestAppShellHostsTheAwardsUnlock(t *testing.T) {
	var b strings.Builder
	if err := App("T", users.User{}, nil).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{
		`id="awards-host"`, `hx-get="/app/awards/unseen"`, `hx-trigger="load, awards-check from:body"`,
		`htmx:after:request`, `awards-check`, `dialog#awards-unlock-dialog[open]`, `window.tui.dialog.open`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("app shell lacks %q", want)
		}
	}
}
