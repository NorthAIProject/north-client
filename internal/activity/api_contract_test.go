package activity

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/shared/apitest"
)

func TestActivityShapes(t *testing.T) {
	t.Parallel()

	started := time.Date(2026, 9, 24, 7, 0, 0, 0, time.UTC)
	paused := started.Add(20 * time.Minute)
	kcal := 212.5
	apitest.AssertGolden(t, "activity-overview.golden.json", Overview{
		Active: &SessionView{
			ID: uuid.MustParse("cdcdcdcd-cdcd-cdcd-cdcd-cdcdcdcdcdcd"), ActivityCode: "strength_training", ActivityName: "Strength training (general)",
			Source: SourceManual, Status: StatusPaused, StartedAt: started, PausedAt: &paused, TotalPausedSeconds: 60, ElapsedSeconds: 1140,
		},
		Recent: []SessionView{{
			ID: uuid.MustParse("dededede-dede-dede-dede-dededededede"), ActivityCode: "running_9_8kmh", ActivityName: "Running (9.8 km/h)",
			Source: SourceManual, Status: StatusCompleted, StartedAt: started.AddDate(0, 0, -1), ElapsedSeconds: 1800, CaloriesBurned: &kcal,
		}},
		Kinds: []ActivityKind{{Code: "strength_training", Name: "Strength training (general)", Category: "strength"}},
	})
}
