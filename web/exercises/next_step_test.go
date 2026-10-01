package exercises

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/exercises/exercise"
	"github.com/NorthAIProject/north-client/internal/users"
)

func renderView(t *testing.T, v DetailView) string {
	t.Helper()
	var b strings.Builder
	if err := Detail(users.User{DisplayName: "Ada"}, v).Render(context.Background(), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	return b.String()
}

func TestDetailWithoutAPlanPointsAtBuildingOne(t *testing.T) {
	html := renderView(t, DetailView{Exercise: illustrated()})
	if !strings.Contains(html, "data-exercise-next") {
		t.Fatal("detail page has no next-step section")
	}
	if !strings.Contains(html, `href="/app/training"`) {
		t.Error("no plan, but no link to build one")
	}
}

// Every day that does not already have the exercise is a form that posts the
// slug to that day; the day that has it is named instead of offered.
func TestDetailOffersTheDaysThatDoNotHaveIt(t *testing.T) {
	planID := uuid.New()
	e := illustrated()
	html := renderView(t, DetailView{
		Exercise: e,
		PlanID:   planID,
		Days: []PlanDayOption{
			{Index: 0, Weekday: "Monday", Focus: "Push", Includes: true},
			{Index: 1, Weekday: "Thursday", Focus: "Upper"},
		},
	})
	if !strings.Contains(html, "Already in your plan on Monday.") {
		t.Error("does not say where it already is")
	}
	if !strings.Contains(html, `action="/app/training/`+planID.String()+`/days/1/exercises"`) {
		t.Error("no add form for Thursday")
	}
	if strings.Contains(html, `/days/0/exercises"`) {
		t.Error("offered to add it to a day that already has it")
	}
	if !strings.Contains(html, `name="catalog_slug" value="`+e.Slug+`"`) {
		t.Error("add form does not carry the slug")
	}
}

func TestDetailListsSimilarExercises(t *testing.T) {
	html := renderView(t, DetailView{
		Exercise: illustrated(),
		Similar:  []exercise.Exercise{{Slug: "push-up", Name: "Push-Up", Primary: []string{"chest"}}},
	})
	if !strings.Contains(html, `href="/app/exercises/push-up"`) {
		t.Error("similar exercise not linked")
	}
	if !strings.Contains(html, "See all") {
		t.Error("no way to the full list for the muscle")
	}
}
