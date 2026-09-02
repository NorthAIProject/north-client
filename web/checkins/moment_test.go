package checkins

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/checkins/checkin"
	"github.com/NorthAIProject/north-client/internal/moments"
)

// The seventh check-in in a row used to be acknowledged exactly like the
// first. When a moment is passed, the panel leads with it; when none is, the
// receipt reads as before.
func TestSavedPanelLeadsWithTheMomentWhenThereIsOne(t *testing.T) {
	m, ok := moments.ForStreak(7)
	if !ok {
		t.Fatal("day 7 should be a moment")
	}

	var with bytes.Buffer
	if err := SavedPanel(checkin.CheckIn{Mood: 4, Energy: 3}, nil, 7, &m).Render(context.Background(), &with); err != nil {
		t.Fatal(err)
	}
	html := with.String()
	// The gesture lands in an escaped x-data attribute, so the name is the
	// stable thing to look for.
	for _, want := range []string{`data-moment="streak"`, m.Title, m.Body, `celebrate`} {
		if !strings.Contains(html, want) {
			t.Errorf("saved panel with a moment is missing %q", want)
		}
	}
	if strings.Index(html, `data-moment=`) > strings.Index(html, `Logged`) {
		t.Error("moment renders below the receipt; it should lead")
	}

	var without bytes.Buffer
	if err := SavedPanel(checkin.CheckIn{Mood: 4, Energy: 3}, nil, 8, nil).Render(context.Background(), &without); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(without.String(), `data-moment=`) {
		t.Error("day 8 rendered a moment")
	}
	if !strings.Contains(without.String(), "8-day streak. Same time tomorrow.") {
		t.Error("plain receipt lost the streak line")
	}
}
