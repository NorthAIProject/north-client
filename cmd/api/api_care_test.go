package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/config"
)

type careBody struct {
	Water struct {
		TotalML int
		Entries []struct{ ID string }
	}
	LastNight *struct {
		DurationMinutes int
		Quality         *int
	}
	Habits []struct {
		ID        string
		DoneToday bool
	}
	Reminders []struct {
		ID      string
		Enabled bool
	}
}

// Each change answers with the page as it now stands.
func TestCareAPI(t *testing.T) {
	handler, pool := testRoutesAndPool(t, func(*config.Config) {})
	api := apiClient{t: t, handler: handler, bearer: "Bearer " + signIn(t, pool).Value}
	read := func(code int, method, path, body string) careBody {
		t.Helper()
		rec := api.call(method, path, body)
		if rec.Code != code {
			t.Fatalf("%s %s: %d %s", method, path, rec.Code, rec.Body)
		}
		var out careBody
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return out
	}

	care := read(http.StatusCreated, http.MethodPost, "/api/v1/care/water", `{"amountMl":250}`)
	if care.Water.TotalML != 250 || len(care.Water.Entries) != 1 {
		t.Fatalf("after one glass: %+v", care.Water)
	}
	care = read(http.StatusOK, http.MethodDelete, "/api/v1/care/water/"+care.Water.Entries[0].ID, "")
	if care.Water.TotalML != 0 {
		t.Errorf("after undo, total = %d", care.Water.TotalML)
	}

	care = read(http.StatusOK, http.MethodPut, "/api/v1/care/sleep", `{"durationMinutes":452,"quality":4,"bedtime":"23:10","wakeTime":"06:45"}`)
	if care.LastNight == nil || care.LastNight.DurationMinutes != 452 || care.LastNight.Quality == nil || *care.LastNight.Quality != 4 {
		t.Errorf("last night = %+v", care.LastNight)
	}

	noDays := api.call(http.MethodPost, "/api/v1/care/habits", `{"name":"Stretch","domain":"health","daysOfWeek":[]}`)
	if noDays.Code != http.StatusUnprocessableEntity || !strings.Contains(noDays.Body.String(), `"daysOfWeek"`) {
		t.Errorf("habit with no days: %d %s, want 422 naming daysOfWeek", noDays.Code, noDays.Body)
	}
	care = read(http.StatusCreated, http.MethodPost, "/api/v1/care/habits", `{"name":"Stretch 10 minutes","domain":"health","daysOfWeek":[0,1,2,3,4,5,6]}`)
	if len(care.Habits) != 1 {
		t.Fatalf("habits = %+v", care.Habits)
	}
	care = read(http.StatusOK, http.MethodPut, "/api/v1/care/habits/"+care.Habits[0].ID+"/done", `{"value":true}`)
	if !care.Habits[0].DoneToday {
		t.Error("habit not done after marking it")
	}

	care = read(http.StatusCreated, http.MethodPost, "/api/v1/care/reminders", `{"label":"Protein after training","timeOfDay":"19:30","daysOfWeek":[]}`)
	if len(care.Reminders) != 1 || !care.Reminders[0].Enabled {
		t.Fatalf("reminders = %+v", care.Reminders)
	}
	care = read(http.StatusOK, http.MethodPut, "/api/v1/care/reminders/"+care.Reminders[0].ID+"/enabled", `{"value":false}`)
	if care.Reminders[0].Enabled {
		t.Error("reminder still enabled")
	}

	if bad := api.call(http.MethodPost, "/api/v1/care/water", `{"amountMl":0}`); bad.Code != http.StatusUnprocessableEntity {
		t.Errorf("zero ml: %d, want 422", bad.Code)
	}
}

func TestJournalAPI(t *testing.T) {
	handler, pool := testRoutesAndPool(t, func(*config.Config) {})
	api := apiClient{t: t, handler: handler, bearer: "Bearer " + signIn(t, pool).Value}

	if rec := api.call(http.MethodPost, "/api/v1/mind/journal", `{"content":"Hard week, but the long run felt good.","mood":4}`); rec.Code != http.StatusCreated {
		t.Fatalf("write: %d %s", rec.Code, rec.Body)
	}
	var journal struct {
		Entries []struct {
			Content string
			Mood    *int
		}
	}
	_ = json.Unmarshal(api.call(http.MethodGet, "/api/v1/mind/journal", "").Body.Bytes(), &journal)
	if len(journal.Entries) != 1 || journal.Entries[0].Mood == nil || *journal.Entries[0].Mood != 4 {
		t.Errorf("journal = %+v", journal)
	}
	if empty := api.call(http.MethodPost, "/api/v1/mind/journal", `{"content":"  "}`); empty.Code != http.StatusUnprocessableEntity {
		t.Errorf("empty entry: %d, want 422", empty.Code)
	}
}
