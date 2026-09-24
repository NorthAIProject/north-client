package workouts

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/shared/apitest"
	"github.com/NorthAIProject/north-client/internal/workouts/plan"
)

func contractPlan() StoredPlan {
	return StoredPlan{
		ID:        uuid.MustParse("abababab-abab-abab-abab-abababababab"),
		Source:    SourceEdited,
		CreatedAt: time.Date(2026, 9, 24, 7, 15, 0, 0, time.UTC),
		Plan: plan.Plan{
			Name: "Strength base", Rationale: "Three full-body days to build the habit first.", WeeksTotal: 8,
			Days: []plan.PlanDay{{
				Weekday: "Monday", StartTime: "07:00", Focus: "Lower body",
				Exercises: []plan.Exercise{{
					Name: "Goblet squat", Sets: 3, Reps: "8-12", RestSeconds: 90, Equipment: "dumbbell",
					FormCues: "Knees track over toes.", CatalogSlug: "goblet-squat", IllustrationSlug: "goblet-squat",
					Primary: []string{"quads"}, Secondary: []string{"glutes"},
				}},
			}, {Weekday: "Thursday", Focus: "Upper body"}},
		},
	}
}

func TestTrainingShapes(t *testing.T) {
	t.Parallel()

	stored := contractPlan()
	apitest.AssertGolden(t, "plan.golden.json", projectDetail(stored, []string{"Thursday has no exercises."}))
	apitest.AssertGolden(t, "plans.golden.json", PlanList{Plans: []PlanSummary{projectSummary(stored)}})
}
