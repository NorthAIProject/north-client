package health

import (
	"testing"

	"github.com/NorthAIProject/north-client/internal/shared/apitest"
)

func TestHealthShapes(t *testing.T) {
	t.Parallel()

	apitest.AssertGolden(t, "health-sync.golden.json", SyncResult{Readings: 1240, Workouts: 3})
}
