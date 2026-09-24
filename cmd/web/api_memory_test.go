package main

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/config"
	"github.com/NorthAIProject/north-client/internal/reports"
)

// A memory written by hand is used at once, and can be pinned, hidden from
// the coach and forgotten.
func TestMemoriesAPI(t *testing.T) {
	handler, pool := testRoutesAndPool(t, func(*config.Config) {})
	api := apiClient{t: t, handler: handler, bearer: "Bearer " + signIn(t, pool).Value}

	created := api.call(http.MethodPost, "/api/v1/memories", `{"category":"preference","content":"Trains before work, around 07:00."}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", created.Code, created.Body)
	}
	var memory struct{ ID, Status string }
	_ = json.Unmarshal(created.Body.Bytes(), &memory)

	// Pinned and excluded are opposite answers to "should the coach see
	// this?", so setting one clears the other.
	pinned := api.call(http.MethodPut, "/api/v1/memories/"+memory.ID+"/pinned", `{"value":true}`)
	if !json.Valid(pinned.Body.Bytes()) || pinned.Code != http.StatusOK {
		t.Fatalf("pin: %d %s", pinned.Code, pinned.Body)
	}
	excluded := api.call(http.MethodPut, "/api/v1/memories/"+memory.ID+"/excluded", `{"value":true}`)
	if excluded.Code != http.StatusOK {
		t.Fatalf("exclude: %d %s", excluded.Code, excluded.Body)
	}

	var list struct {
		Approved []struct {
			ID               string
			Pinned, Excluded bool
		}
		Categories []string
	}
	_ = json.Unmarshal(api.call(http.MethodGet, "/api/v1/memories", "").Body.Bytes(), &list)
	if len(list.Approved) != 1 || list.Approved[0].Pinned || !list.Approved[0].Excluded || len(list.Categories) == 0 {
		t.Fatalf("memories = %+v, want one approved, excluded and so no longer pinned", list)
	}

	if empty := api.call(http.MethodPost, "/api/v1/memories", `{"category":"preference","content":"  "}`); empty.Code != http.StatusUnprocessableEntity {
		t.Errorf("empty memory: %d, want 422", empty.Code)
	}
	if gone := api.call(http.MethodDelete, "/api/v1/memories/"+memory.ID, ""); gone.Code != http.StatusNoContent {
		t.Errorf("delete: %d", gone.Code)
	}
}

// A report the coach wrote is listed, read, rated and archived.
func TestReportsAPI(t *testing.T) {
	handler, pool := testRoutesAndPool(t, func(*config.Config) {})
	api := apiClient{t: t, handler: handler, bearer: "Bearer " + signIn(t, pool).Value}

	var me struct{ User struct{ ID uuid.UUID } }
	_ = json.Unmarshal(api.call(http.MethodGet, "/api/v1/me", "").Body.Bytes(), &me)
	repo := reports.NewRepository(pool)
	rep, err := repo.Create(t.Context(), me.User.ID, reports.Period{Start: mustDay(t, "2026-09-14"), End: mustDay(t, "2026-09-20"), Title: "Week of 14 September"}, reports.KindWeekly)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SaveGenerated(t.Context(), rep.ID, me.User.ID, "## The week\n\nFour sessions."); err != nil {
		t.Fatal(err)
	}

	var list struct{ Reports []struct{ ID, Status string } }
	_ = json.Unmarshal(api.call(http.MethodGet, "/api/v1/reports", "").Body.Bytes(), &list)
	if len(list.Reports) != 1 || list.Reports[0].Status != "ready" {
		t.Fatalf("reports = %+v, want one ready", list)
	}

	rated := api.call(http.MethodPut, "/api/v1/reports/"+rep.ID.String()+"/helpful", `{"helpful":true}`)
	var detail struct {
		Body    string
		Helpful *bool
	}
	_ = json.Unmarshal(rated.Body.Bytes(), &detail)
	if rated.Code != http.StatusOK || detail.Helpful == nil || !*detail.Helpful || detail.Body == "" {
		t.Fatalf("rate: %d %s", rated.Code, rated.Body)
	}

	if archived := api.call(http.MethodPost, "/api/v1/reports/"+rep.ID.String()+"/archive", ""); archived.Code != http.StatusNoContent {
		t.Fatalf("archive: %d %s", archived.Code, archived.Body)
	}
	_ = json.Unmarshal(api.call(http.MethodGet, "/api/v1/reports", "").Body.Bytes(), &list)
	if len(list.Reports) != 0 {
		t.Errorf("archived report still listed: %+v", list)
	}
	_ = json.Unmarshal(api.call(http.MethodGet, "/api/v1/reports?archived=true", "").Body.Bytes(), &list)
	if len(list.Reports) != 1 {
		t.Errorf("archived=true lists %d, want 1", len(list.Reports))
	}
}

func mustDay(t *testing.T, day string) time.Time {
	t.Helper()
	d, err := time.Parse("2006-01-02", day)
	if err != nil {
		t.Fatal(err)
	}
	return d
}
