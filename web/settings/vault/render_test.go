package vault

import (
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"

	"github.com/NorthAIProject/north-client/internal/users"
)

func render(t *testing.T, c templ.Component) string {
	t.Helper()
	var b strings.Builder
	if err := c.Render(context.Background(), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	return b.String()
}

func TestVaultPageShowsWorkerRequirement(t *testing.T) {
	html := render(t, Page(users.User{DisplayName: "Test"}, View{}, Form{}))

	if !strings.Contains(html, "worker must run") {
		t.Errorf("vault page does not explain worker filesystem access:\n%s", html)
	}
	if !strings.Contains(html, `/app/settings/vault/connect`) {
		t.Error("vault page missing connect form action")
	}
}

func TestVaultPageShowsConnectedState(t *testing.T) {
	html := render(t, Page(users.User{DisplayName: "Test"}, View{
		RootPath: "/Users/me/Vault",
	}, Form{}))

	if !strings.Contains(html, "/Users/me/Vault") {
		t.Error("connected vault path not shown")
	}
	if !strings.Contains(html, "Sync now") {
		t.Error("connected state missing sync button")
	}
	if !strings.Contains(html, "Disconnect") {
		t.Error("connected state missing disconnect button")
	}
}

func TestVaultPageShowsPathValidationError(t *testing.T) {
	html := render(t, Page(users.User{DisplayName: "Test"}, View{}, Form{
		RootPath: "/missing",
		Errors:   map[string]string{"path": "Khepri could not find that folder on this machine."},
	}))

	if !strings.Contains(html, "could not find that folder") {
		t.Errorf("validation error not rendered:\n%s", html)
	}
}
