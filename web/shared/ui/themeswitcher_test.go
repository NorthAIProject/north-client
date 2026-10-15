package ui

import (
	"context"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/shared/theme"
)

// The cycle the server renders must be the one the script continues:
// System → Light → Dark → System.
func TestTheSwitcherCyclesSystemLightDark(t *testing.T) {
	want := map[theme.Appearance]theme.Appearance{
		theme.System: theme.Light,
		theme.Light:  theme.Dark,
		theme.Dark:   theme.System,
	}
	for from, to := range want {
		if got := nextAppearance(from); got != to {
			t.Errorf("after %s comes %s, want %s", from, got, to)
		}
	}
	// The script's order, as written in the template.
	if !strings.Contains(scriptSource(t), `["system", "light", "dark"]`) {
		t.Error("the script's cycle no longer matches nextAppearance")
	}
}

func TestTheSwitcherPostsTheNextStepAndNamesTheCurrentOne(t *testing.T) {
	var b strings.Builder
	ctx := theme.WithAppearance(context.Background(), theme.Light)
	if err := ThemeSwitcher().Render(ctx, &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := b.String()
	for _, want := range []string{
		`action="/appearance"`,
		`name="appearance" value="dark"`,
		`aria-label="Appearance: light. Switch to dark."`,
		`data-theme-icon="light" class="inline-flex"`,
		`data-theme-icon="dark" class="hidden"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("switcher is missing %s", want)
		}
	}
	if strings.Contains(out, "localStorage") {
		t.Error("the switcher still keeps a copy in localStorage")
	}
}

func scriptSource(t *testing.T) string {
	t.Helper()
	var b strings.Builder
	if err := themeSwitcherScript().Render(context.Background(), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	return b.String()
}
