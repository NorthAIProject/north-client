package main

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/NorthAIProject/north-client/internal/config"
)

// A goal's whole life through the API: created, given a milestone that is
// then completed, noted, checked in against, and deleted.
func TestGoalsAndCheckInsAPI(t *testing.T) {
	handler, pool := testRoutesAndPool(t, func(*config.Config) {})
	api := apiClient{t: t, handler: handler, bearer: "Bearer " + signIn(t, pool).Value}

	rec := api.call(http.MethodPost, "/api/v1/goals",
		`{"title":"Run a half marathon","motivation":"Keep a plan","success":"Under 2:10","category":"fitness","targetDate":"2026-12-06"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	var goal struct {
		ID         string
		TargetDate string
		Milestones []struct{ ID, Status string }
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &goal)
	if goal.TargetDate != "2026-12-06" {
		t.Errorf("targetDate = %q", goal.TargetDate)
	}

	if r1 := api.call(http.MethodPost, "/api/v1/goals", `{"title":"x","motivation":"","success":"","category":"fitness","targetDate":"next week"}`); r1.Code != http.StatusUnprocessableEntity {
		t.Errorf("unreadable date: %d, want 422", r1.Code)
	}

	rec = api.call(http.MethodPost, "/api/v1/goals/"+goal.ID+"/milestones", `{"title":"Run 10 km"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("milestone: %d %s", rec.Code, rec.Body)
	}
	var milestone struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &milestone)
	if r2 := api.call(http.MethodPut, "/api/v1/goals/"+goal.ID+"/milestones/"+milestone.ID+"/status", `{"status":"completed"}`); r2.Code != http.StatusOK {
		t.Fatalf("complete milestone: %d %s", r2.Code, r2.Body)
	}
	if r3 := api.call(http.MethodPost, "/api/v1/goals/"+goal.ID+"/updates", `{"note":"Ran 12 km","progress":40}`); r3.Code != http.StatusCreated {
		t.Fatalf("note: %d %s", r3.Code, r3.Body)
	}

	var detail struct {
		MilestoneTotal, MilestoneDone int
		LatestUpdate                  struct{ Progress int }
	}
	_ = json.Unmarshal(api.call(http.MethodGet, "/api/v1/goals/"+goal.ID, "").Body.Bytes(), &detail)
	if detail.MilestoneTotal != 1 || detail.MilestoneDone != 1 || detail.LatestUpdate.Progress != 40 {
		t.Errorf("detail = %+v, want 1/1 milestones and a 40%% note", detail)
	}

	// Today's check-in, about the goal; filing again replaces it.
	for _, mood := range []int{3, 4} {
		body := `{"mood":` + string(rune('0'+mood)) + `,"energy":3,"wins":"Long run","relatedGoalId":"` + goal.ID + `"}`
		if r4 := api.call(http.MethodPut, "/api/v1/check-ins/today", body); r4.Code != http.StatusOK {
			t.Fatalf("check in: %d %s", r4.Code, r4.Body)
		}
	}
	var list struct {
		Today struct {
			Mood             int
			RelatedGoalTitle string
		}
		Recent []struct{}
		Streak int
	}
	_ = json.Unmarshal(api.call(http.MethodGet, "/api/v1/check-ins", "").Body.Bytes(), &list)
	if list.Today.Mood != 4 || list.Today.RelatedGoalTitle != "Run a half marathon" || len(list.Recent) != 1 || list.Streak != 1 {
		t.Errorf("check-ins = %+v, want one of mood 4 about the goal, streak 1", list)
	}

	if r5 := api.call(http.MethodPut, "/api/v1/check-ins/today", `{"mood":9,"energy":3}`); r5.Code != http.StatusUnprocessableEntity {
		t.Errorf("mood 9: %d, want 422", r5.Code)
	}

	if r6 := api.call(http.MethodDelete, "/api/v1/goals/"+goal.ID, ""); r6.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", r6.Code, r6.Body)
	}
	if r7 := api.call(http.MethodGet, "/api/v1/goals/"+goal.ID, ""); r7.Code != http.StatusNotFound {
		t.Errorf("deleted goal: %d, want 404", r7.Code)
	}
}
