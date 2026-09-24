package fitness

import (
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/shared/apitest"
)

func TestFitnessShapes(t *testing.T) {
	t.Parallel()

	synced := time.Date(2026, 9, 24, 6, 30, 0, 0, time.UTC)
	apitest.AssertGolden(t, "strava-status.golden.json", StravaStatus{
		Configured: true, Connected: true, LastSyncedAt: &synced, LastSyncAttemptedAt: &synced,
	})
	apitest.AssertGolden(t, "strava-connect.golden.json", StravaConnect{
		AuthorizeURL: "https://www.strava.com/oauth/authorize?client_id=123&state=abc",
	})
}
