package app

import (
	"context"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/users"
)

func renderDashboard(t *testing.T, data DashboardData) string {
	t.Helper()
	var b strings.Builder
	if err := Dashboard(users.User{DisplayName: "Ada"}, data).Render(context.Background(), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	return b.String()
}

func TestDashboardShowsNextStepForAFreshAccount(t *testing.T) {
	html := renderDashboard(t, DashboardData{
		HasNextStep: true,
		NextStep: NextStep{
			Eyebrow: "Start here",
			Title:   "Name one thing you are working toward",
			CTA:     "Add a goal",
			Href:    "/app/goals",
		},
	})
	if !strings.Contains(html, "Name one thing you are working toward") {
		t.Fatal("fresh dashboard has no next step")
	}
	if !strings.Contains(html, `href="/app/goals"`) {
		t.Fatal("next step CTA missing")
	}
}

func TestDashboardHidesNextStepOnceActivated(t *testing.T) {
	html := renderDashboard(t, DashboardData{})
	if strings.Contains(html, "Name one thing you are working toward") {
		t.Fatal("activated dashboard still shows the first-run card")
	}
}

func TestDashboardNextStepSitsOutsideTheRangeSwap(t *testing.T) {
	html := renderDashboard(t, DashboardData{
		HasNextStep: true,
		NextStep: NextStep{
			Title: "How did today go?",
			CTA:   "Check in",
			Href:  "/app/check-ins",
		},
	})
	step := strings.Index(html, "How did today go?")
	panels := strings.Index(html, `id="dashboard-panels"`)
	if step < 0 || panels < 0 {
		t.Fatal("missing next step or panels")
	}
	if step > panels {
		t.Fatal("next step is inside the range-swap fragment")
	}
}
