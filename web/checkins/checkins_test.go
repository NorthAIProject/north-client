package checkins

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/users"
)

// A rejected submission re-renders the whole guided form. If it reopens on the
// wrong pane the user has to walk forward through the wizard to reach the field
// that failed, which is the difference between a 30-second flow and an
// abandoned one.
func TestCheckInFormFirstErrorStep(t *testing.T) {
	tests := []struct {
		name   string
		errors map[string]string
		want   int
	}{
		{name: "no errors opens at the start", errors: nil, want: 1},
		{name: "empty map opens at the start", errors: map[string]string{}, want: 1},
		{name: "mood", errors: map[string]string{"mood": "Pick a mood from 1 to 5."}, want: 1},
		{name: "energy", errors: map[string]string{"energy": "Pick an energy from 1 to 5."}, want: 2},
		{name: "wins", errors: map[string]string{"wins": "Too long."}, want: 3},
		{name: "challenges", errors: map[string]string{"challenges": "Too long."}, want: 4},
		{name: "notes", errors: map[string]string{"notes": "Too long."}, want: 5},
		{name: "related goal shares the notes pane", errors: map[string]string{"related_goal_id": "That goal is not available."}, want: 5},
		{
			name:   "earliest failing pane wins",
			errors: map[string]string{"notes": "Too long.", "energy": "Pick an energy from 1 to 5."},
			want:   2,
		},
		{
			name:   "unknown field does not hide a real one",
			errors: map[string]string{"nope": "?", "challenges": "Too long."},
			want:   4,
		},
		{name: "unknown field alone opens at the start", errors: map[string]string{"nope": "?"}, want: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := CheckInForm{Errors: tt.errors}
			// Map iteration is unordered; a single pass could pass by luck.
			for i := 0; i < 20; i++ {
				if got := f.FirstErrorStep(); got != tt.want {
					t.Fatalf("FirstErrorStep() = %d, want %d", got, tt.want)
				}
			}
		})
	}
}

func renderIndex(t *testing.T) string {
	t.Helper()
	var buf bytes.Buffer
	err := IndexPage(
		users.User{DisplayName: "Fernando"},
		nil,
		CheckInForm{Mood: 3, Energy: 3},
		nil,
		0,
		false,
		Instruments{},
	).Render(context.Background(), &buf)
	if err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

// The job of this page is today's check-in. Charts above the form push the
// first thumb action off a phone screen.
func TestIndexPagePutsTheFormBeforeTheCharts(t *testing.T) {
	body := renderIndex(t)
	panel := strings.Index(body, `id="checkin-panel"`)
	charts := strings.Index(body, "Mood &amp; energy")
	if charts < 0 {
		charts = strings.Index(body, "Mood & energy")
	}
	switch {
	case panel < 0:
		t.Fatal("missing check-in panel")
	case charts < 0:
		t.Fatal("missing mood chart heading")
	case panel > charts:
		t.Error("charts appear before today's form; the wizard must come first")
	}
}

func TestIndexPageFirstCheckInFraming(t *testing.T) {
	body := renderIndex(t)
	if !strings.Contains(body, "Your first check-in") {
		t.Fatal("empty check-ins page has no first-time framing")
	}
}

func TestStepNavClearsTheHomeIndicator(t *testing.T) {
	body := renderIndex(t)
	for _, want := range []string{
		"env(safe-area-inset-bottom)",
		"min-h-11",
		"flex-1",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("check-in form missing %q", want)
		}
	}
}
