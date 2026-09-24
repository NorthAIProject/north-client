package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/config"
)

// A sync from the phone stores readings and workouts; replaying it adds
// nothing, and forgetting removes the readings while the workouts stay in
// activity history.
func TestHealthAPISyncIsReplayableAndForgettable(t *testing.T) {
	handler, pool := testRoutesAndPool(t, func(*config.Config) {})
	api := apiClient{t: t, handler: handler, bearer: "Bearer " + signIn(t, pool).Value}

	start := time.Now().Add(-3 * time.Hour).UTC().Truncate(time.Second)
	body := fmt.Sprintf(`{
		"readings": [
			{"metric": "resting_heart_rate", "value": 52, "unit": "count/min", "startedAt": %q},
			{"metric": "steps", "value": 8421, "unit": "count", "startedAt": %q, "endedAt": %q}
		],
		"workouts": [
			{"activityCode": "running_9_8kmh", "externalId": "hk-run-1", "startedAt": %q, "endedAt": %q, "calories": 410}
		]
	}`, start.Format(time.RFC3339), start.Format(time.RFC3339), start.Add(2*time.Hour).Format(time.RFC3339),
		start.Format(time.RFC3339), start.Add(40*time.Minute).Format(time.RFC3339))

	for attempt := 1; attempt <= 2; attempt++ {
		rec := api.call(http.MethodPost, "/api/v1/health/samples", body)
		if rec.Code != http.StatusOK {
			t.Fatalf("sync %d: %d %s", attempt, rec.Code, rec.Body)
		}
	}

	var overview struct {
		Recent []struct {
			Source         string
			CaloriesBurned float64
		}
	}
	_ = json.Unmarshal(api.call(http.MethodGet, "/api/v1/activity", "").Body.Bytes(), &overview)
	if len(overview.Recent) != 1 || overview.Recent[0].Source != "apple_health" || overview.Recent[0].CaloriesBurned != 410 {
		t.Fatalf("activity after two syncs = %+v, want one apple_health run of 410 kcal", overview.Recent)
	}

	if rec := api.call(http.MethodPost, "/api/v1/health/samples", `{}`); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("empty sync: %d, want 422", rec.Code)
	}

	if rec := api.call(http.MethodDelete, "/api/v1/health/samples", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("forget: %d %s", rec.Code, rec.Body)
	}
	_ = json.Unmarshal(api.call(http.MethodGet, "/api/v1/activity", "").Body.Bytes(), &overview)
	if len(overview.Recent) != 1 {
		t.Errorf("forgetting readings removed the workout from activity history")
	}
}
