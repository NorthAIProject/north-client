package goals

import (
	"context"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/goals/goal"
	"github.com/NorthAIProject/north-client/internal/moments"
	"github.com/NorthAIProject/north-client/internal/users"
)

func renderDetail(t *testing.T, m *moments.Moment) string {
	t.Helper()
	var b strings.Builder
	g := goal.Goal{Title: "Run a half marathon", Status: goal.StatusAchieved, Category: "fitness"}
	if err := DetailPage(users.User{DisplayName: "Ada"}, g, nil, FormFor(g), m).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// Marking a goal achieved used to be a redirect and nothing else. The detail
// page now shows the card once, above the goal, when the redirect says so.
func TestDetailPageShowsTheMomentOnce(t *testing.T) {
	m := moments.ForGoalAchieved("Run a half marathon")
	html := renderDetail(t, &m)
	for _, want := range []string{`data-moment="goal_achieved"`, "Run a half marathon", `celebrate`} {
		if !strings.Contains(html, want) {
			t.Errorf("detail page with a moment is missing %q", want)
		}
	}

	if strings.Contains(renderDetail(t, nil), `data-moment=`) {
		t.Error("detail page rendered a moment nobody earned")
	}
}
