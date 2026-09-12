package insights

import (
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/conversations"
	insightpages "github.com/NorthAIProject/north-client/web/insights"
)

func mustCoachView(t *testing.T, data CoachData) insightpages.CoachView {
	t.Helper()
	view, err := buildCoachView(data)
	if err != nil {
		t.Fatalf("buildCoachView: %v", err)
	}
	return view
}

func yes() *bool { b := true; return &b }
func no() *bool  { b := false; return &b }

func talked(t *testing.T) CoachData {
	t.Helper()
	rg := weekRange(t)
	at := func(d int) time.Time { return rg.Since.AddDate(0, 0, d).Add(10 * time.Hour) }

	return CoachData{
		Range: rg,
		Messages: []conversations.MessageStat{
			{At: at(0), Role: ai.RoleUser},
			{At: at(0), Role: ai.RoleModel, Helpful: yes()},
			{At: at(1), Role: ai.RoleUser},
			{At: at(1), Role: ai.RoleModel, Helpful: no()},
			{At: at(3), Role: ai.RoleUser},
			{At: at(3), Role: ai.RoleModel, Helpful: yes()},
			{At: at(3), Role: ai.RoleModel},
		},
	}
}

func TestCoachViewCountsBothSidesOfTheConversation(t *testing.T) {
	view := mustCoachView(t, talked(t))

	if view.YourMessages != 3 {
		t.Errorf("YourMessages = %d, want 3", view.YourMessages)
	}
	if view.CoachReplies != 4 {
		t.Errorf("CoachReplies = %d, want 4", view.CoachReplies)
	}
}

func TestCoachViewReportsHelpfulRateOverRatedRepliesOnly(t *testing.T) {
	// Three replies were rated, two of them up. The unrated one is not a
	// thumbs-down, and counting it as one would make the coach look worse the
	// more it is used.
	view := mustCoachView(t, talked(t))

	if !view.HasRatings {
		t.Fatal("HasRatings = false despite three rated replies")
	}
	if view.HelpfulRate != 67 {
		t.Errorf("HelpfulRate = %d, want 67 (two of three rated)", view.HelpfulRate)
	}
	if view.Rated != 3 {
		t.Errorf("Rated = %d, want 3", view.Rated)
	}
}

func TestCoachViewMakesNoClaimWhenNothingWasRated(t *testing.T) {
	data := talked(t)
	for i := range data.Messages {
		data.Messages[i].Helpful = nil
	}

	view := mustCoachView(t, data)

	if view.HasRatings {
		t.Error("HasRatings = true with nothing rated")
	}
}

func TestCoachViewOfAnEmptyWindowSaysSo(t *testing.T) {
	view := mustCoachView(t, CoachData{Range: weekRange(t)})

	if view.HasData {
		t.Error("HasData = true with no messages")
	}
}

func TestCoachViewFlagsATruncatedWindow(t *testing.T) {
	// Charting the first five thousand turns of a heavier year without
	// saying so would quietly understate it.
	data := talked(t)
	data.Truncated = true

	if !mustCoachView(t, data).Truncated {
		t.Error("Truncated did not reach the view")
	}
}

func TestCoachViewChartsBothRoles(t *testing.T) {
	view := mustCoachView(t, talked(t))

	if len(view.Chart.Data.Datasets) != 2 {
		t.Errorf("chart has %d datasets, want 2 (you and the coach)", len(view.Chart.Data.Datasets))
	}
}
