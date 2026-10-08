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
		IntakeID:  uuid.MustParse("cdcdcdcd-cdcd-cdcd-cdcd-cdcdcdcdcdcd"),
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
	// Monday is done this week, so Thursday is next.
	monday := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
	day := func(index, offset int, done bool) WeekDay {
		d := stored.Plan.Days[index]
		return WeekDay{
			Slot: Slot{Weekday: d.Weekday, IntakeID: stored.IntakeID, DayIndex: index},
			Date: monday.AddDate(0, 0, offset), PlanID: stored.ID, PlanName: stored.Plan.Name,
			Day: d, Completed: done,
		}
	}
	progress := WeekProgress{
		Start: monday, Completed: []string{"Monday"},
		Days: []WeekDay{day(0, 0, true), day(1, 3, false)},
		Next: stored.Plan.Days[1], NextDay: day(1, 3, false), HasNext: true,
	}
	apitest.AssertGolden(t, "plan.golden.json", projectDetail(stored, []string{"Thursday has no exercises."}, progress, stored.IntakeID))
	summary := projectSummary(stored)
	summary.Active = true
	apitest.AssertGolden(t, "plans.golden.json", PlanList{Plans: []PlanSummary{summary}})
	apitest.AssertGolden(t, "week.golden.json", projectWeek(progress))
}
