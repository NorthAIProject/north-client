package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/config"
)

func TestDecisionsAPI(t *testing.T) {
	handler, pool := testRoutesAndPool(t, func(*config.Config) {})
	api := apiClient{t: t, handler: handler, bearer: "Bearer " + signIn(t, pool).Value}

	created := api.call(http.MethodPost, "/api/v1/decisions", `{"title":"December or March","options":"Sooner, or more base","rationale":"The knee"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", created.Code, created.Body)
	}
	var d struct{ ID, Outcome string }
	_ = json.Unmarshal(created.Body.Bytes(), &d)

	updated := api.call(http.MethodPut, "/api/v1/decisions/"+d.ID, `{"title":"December or March","options":"Sooner, or more base","rationale":"The knee","outcome":"March; PB by four minutes"}`)
	_ = json.Unmarshal(updated.Body.Bytes(), &d)
	if updated.Code != http.StatusOK || d.Outcome != "March; PB by four minutes" {
		t.Fatalf("update: %d %s", updated.Code, updated.Body)
	}
	if gone := api.call(http.MethodDelete, "/api/v1/decisions/"+d.ID, ""); gone.Code != http.StatusNoContent {
		t.Errorf("delete: %d", gone.Code)
	}
}

func TestNudgesAndExportAPI(t *testing.T) {
	handler, pool := testRoutesAndPool(t, func(*config.Config) {})
	api := apiClient{t: t, handler: handler, bearer: "Bearer " + signIn(t, pool).Value}

	var list struct {
		Nudges []struct{}
		Unread int
	}
	if rec := api.call(http.MethodGet, "/api/v1/nudges", ""); rec.Code != http.StatusOK {
		t.Fatalf("nudges: %d %s", rec.Code, rec.Body)
	} else {
		_ = json.Unmarshal(rec.Body.Bytes(), &list)
	}
	if missing := api.call(http.MethodPost, "/api/v1/nudges/00000000-0000-0000-0000-000000000000/open", ""); missing.Code != http.StatusNotFound {
		t.Errorf("unknown nudge: %d, want 404", missing.Code)
	}

	archive := api.call(http.MethodGet, "/api/v1/account/export", "")
	if archive.Code != http.StatusOK || !strings.HasPrefix(archive.Header().Get("Content-Type"), "application/zip") ||
		!strings.HasPrefix(archive.Body.String(), "PK") {
		t.Errorf("export: %d %q, want a zip", archive.Code, archive.Header().Get("Content-Type"))
	}
}

// Measurements first, then a goal from them; the goal is what nutrition's
// progress and workout calories read.
func TestCalculatorAPI(t *testing.T) {
	handler, pool := testRoutesAndPool(t, func(*config.Config) {})
	api := apiClient{t: t, handler: handler, bearer: "Bearer " + signIn(t, pool).Value}

	var calc struct {
		Biometrics *struct{ WeightKg float64 }
		Goal       *struct{ CalorieGoal float64 }
		Options    struct{ Goals []string }
	}
	if rec := api.call(http.MethodPut, "/api/v1/calculator/biometrics", `{"weightKg":72,"heightCm":175,"dateOfBirth":"1990-05-01","sex":"female"}`); rec.Code != http.StatusOK {
		t.Fatalf("biometrics: %d %s", rec.Code, rec.Body)
	} else {
		_ = json.Unmarshal(rec.Body.Bytes(), &calc)
	}
	if calc.Biometrics == nil || calc.Biometrics.WeightKg != 72 || len(calc.Options.Goals) == 0 {
		t.Fatalf("after recording: %+v", calc)
	}
	rec := api.call(http.MethodPost, "/api/v1/calculator/plan", `{"activityLevel":"`+"moderate"+`","goal":"`+calc.Options.Goals[0]+`","macroSplit":"moderate_carb"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("goal: %d %s", rec.Code, rec.Body)
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &calc)
	if calc.Goal == nil || calc.Goal.CalorieGoal <= 0 {
		t.Errorf("goal = %+v", calc.Goal)
	}
	if bad := api.call(http.MethodPut, "/api/v1/calculator/biometrics", `{"weightKg":72,"heightCm":175,"dateOfBirth":"May 1990","sex":"female"}`); bad.Code != http.StatusUnprocessableEntity {
		t.Errorf("unreadable date: %d, want 422", bad.Code)
	}

	news := api.call(http.MethodGet, "/api/v1/news", "")
	if news.Code != http.StatusOK || !strings.Contains(news.Body.String(), `"items":[`) {
		t.Errorf("news: %d %s", news.Code, news.Body)
	}
}
