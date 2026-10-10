package meals_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/meals"
	"github.com/NorthAIProject/north-client/internal/users"
)

func (f planFixture) postJSON(t *testing.T, userID uuid.UUID, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	meals.NewAPI(meals.HandlerOptions{Plans: f.svc}).Routes(r)
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(auth.ContextWithUser(req.Context(), users.User{ID: userID, Timezone: "UTC"}))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestAPIAddsAMealOption(t *testing.T) {
	f := newPlanFixture(t, "options-api@north.test")
	plan := f.lunchWithOption(t)
	slot := plan.Days[0].Meals[0]
	target := "/nutrition/meals/" + slot.ID.String() + "/options"

	// No label: the first "Option N" free. The answer is the whole plan, so a
	// client refreshes in one call.
	rec := f.postJSON(t, f.userID, target, `{}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	var detail meals.PlanDetail
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.ID != plan.ID || len(detail.Days) != 1 || len(detail.Days[0].Meals) != 1 {
		t.Fatalf("answer = %+v, want the plan", detail)
	}
	alts := f.reload(t, plan.ID).Days[0].Meals[0].Alternatives
	if len(alts) != 2 || alts[1].OptionLabel != "Option 2" {
		t.Fatalf("alternatives = %+v, want a second one labelled Option 2", alts)
	}

	// A label is kept.
	if rec = f.postJSON(t, f.userID, target, `{"label":"Peixe"}`); rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	if alts = f.reload(t, plan.ID).Days[0].Meals[0].Alternatives; len(alts) != 3 || alts[2].OptionLabel != "Peixe" {
		t.Fatalf("alternatives = %+v", alts)
	}

	// Too long a label is refused, and nothing is added.
	long := strings.Repeat("a", meals.MaxOptionLabelRunes+1)
	if rec = f.postJSON(t, f.userID, target, `{"label":"`+long+`"}`); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a long label: status = %d, body %s", rec.Code, rec.Body)
	}
	if alts = f.reload(t, plan.ID).Days[0].Meals[0].Alternatives; len(alts) != 3 {
		t.Fatalf("a refused label left %d alternatives", len(alts))
	}

	// Somebody else's meal is not found, and is left alone.
	stranger := newUser(t, f.pool, "options-api-stranger@north.test")
	if rec = f.postJSON(t, stranger.ID, target, `{}`); rec.Code != http.StatusNotFound {
		t.Fatalf("another person's meal: status = %d, body %s", rec.Code, rec.Body)
	}
	if alts = f.reload(t, plan.ID).Days[0].Meals[0].Alternatives; len(alts) != 3 {
		t.Fatalf("a stranger's request left %d alternatives", len(alts))
	}
}
