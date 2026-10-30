package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/config"
	"github.com/NorthAIProject/north-client/internal/workouts"
	"github.com/NorthAIProject/north-client/internal/workouts/plan"
)

// Setting a start time is an edit like any other: it stores a new version and
// answers with it. Editing the version it replaced is refused with the newest
// one, and a time that is not a clock time is refused in the plan's words.
func TestTrainingAPIStartTimeEditsAreVersioned(t *testing.T) {
	handler, pool := testRoutesAndPool(t, func(*config.Config) {})
	api := apiClient{t: t, handler: handler, bearer: "Bearer " + signIn(t, pool).Value}

	var me struct{ User struct{ ID uuid.UUID } }
	_ = json.Unmarshal(api.call(http.MethodGet, "/api/v1/me", "").Body.Bytes(), &me)

	repo := workouts.NewRepository(pool)
	intake, err := repo.CreateIntake(t.Context(), me.User.ID, workouts.Intake{
		Goal: "strength", Experience: "beginner", DaysPerWeek: 2, SessionMinutes: 45, Equipment: []string{"dumbbell"},
	})
	if err != nil {
		t.Fatal(err)
	}
	seeded, err := repo.CreatePlan(t.Context(), workouts.StoredPlan{
		UserID: me.User.ID, IntakeID: intake.ID, Source: workouts.SourceAI,
		Plan: plan.Plan{Name: "Base", WeeksTotal: 4, Days: []plan.PlanDay{{Weekday: "Monday", Focus: "Full body"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	original := seeded.ID.String()

	rec := api.call(http.MethodPut, "/api/v1/training/plans/"+original+"/days/0/start-time", `{"startTime":"7:30"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("set start time: %d %s", rec.Code, rec.Body)
	}
	var edited struct {
		ID   string
		Days []struct{ StartTime string }
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &edited)
	if edited.ID == original || edited.Days[0].StartTime != "07:30" {
		t.Fatalf("edited = %+v, want a new version starting 07:30", edited)
	}

	// The superseded version is refused, with the newest one to show.
	rec = api.call(http.MethodPut, "/api/v1/training/plans/"+original+"/days/0/start-time", `{"startTime":"08:00"}`)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), edited.ID) {
		t.Fatalf("stale edit: %d %s, want 409 carrying %s", rec.Code, rec.Body, edited.ID)
	}

	rec = api.call(http.MethodPut, "/api/v1/training/plans/"+edited.ID+"/days/0/start-time", `{"startTime":"7pm"}`)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "24-hour clock") {
		t.Fatalf("bad time: %d %s, want 422 in the plan's words", rec.Code, rec.Body)
	}
}

// A workout on the phone starts an activity session. Without a recorded
// weight there are no calories to compute, and the refusal says so.
func TestActivityAPIExplainsWhyItCannotStart(t *testing.T) {
	api := newAPIClient(t)

	rec := api.call(http.MethodPost, "/api/v1/activity/start", `{"activityCode":"strength_training"}`)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "biometrics") {
		t.Fatalf("start without biometrics: %d %s", rec.Code, rec.Body)
	}

	rec = api.call(http.MethodGet, "/api/v1/activity", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "strength_training") {
		t.Fatalf("overview: %d, want the activity kinds listed", rec.Code)
	}
}
