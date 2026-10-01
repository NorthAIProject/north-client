package workouts

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

// The exercise page adds to a plan with a plain form. Answering that with the
// htmx plan-body fragment would leave the browser on a bare, unstyled page, so
// a non-htmx edit is sent to the plan instead.
func TestPlainFormEditRedirectsToThePlan(t *testing.T) {
	id := uuid.New()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/app/training/"+id.String()+"/days/0/exercises", nil)

	NewHandler(nil).afterEdit(rec, req, id)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "/app/training/"+id.String() {
		t.Errorf("Location = %q", got)
	}
}
