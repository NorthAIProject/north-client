package workouts_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/workouts"
)

// Until 2026-09-07 the daily training nudge linked to /app/workouts/{id}, which
// has never been a route. Those links were written into user_nudges.href and
// delivered as push payloads, so some of them live on devices where nothing can
// rewrite them. This redirect is what they land on.
func TestLegacyWorkoutsPathRedirectsToTraining(t *testing.T) {
	r := chi.NewRouter()
	r.Route("/app", workouts.NewHandler(nil).Routes)

	const id = "0457ef81-87ec-48fe-9daa-cf606a501bab"
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/app/workouts/"+id, nil))

	if rec.Code != http.StatusMovedPermanently {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMovedPermanently)
	}
	if got, want := rec.Header().Get("Location"), "/app/training/"+id; got != want {
		t.Errorf("Location = %q, want %q", got, want)
	}
}
