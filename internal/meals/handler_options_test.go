package meals_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/meals"
	"github.com/NorthAIProject/north-client/internal/users"
)

func (f planFixture) postForm(t *testing.T, target string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	meals.NewHandler(meals.HandlerOptions{Plans: f.svc}).Routes(r)
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(auth.ContextWithUser(req.Context(), users.User{ID: f.userID, Timezone: "UTC"}))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestAddOptionFormAddsAnOptionAndOpensIt(t *testing.T) {
	f := newPlanFixture(t, "options-web@north.test")
	plan := f.lunchWithOption(t)
	slot := plan.Days[0].Meals[0]

	// A blank label takes the next position in the slot: lunch has two.
	rec := f.postForm(t, "/nutrition/meals/"+slot.ID.String()+"/options", url.Values{"option_label": {"  "}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	alts := f.reload(t, plan.ID).Days[0].Meals[0].Alternatives
	if len(alts) != 2 || alts[1].OptionLabel != "Option 3" {
		t.Fatalf("alternatives = %+v, want a second one labelled Option 3", alts)
	}
	if want := "/app/nutrition/plans/" + plan.ID.String() + "?option=" + alts[1].ID.String(); rec.Header().Get("Location") != want {
		t.Fatalf("location = %q, want %q", rec.Header().Get("Location"), want)
	}

	// A label the person typed is kept.
	rec = f.postForm(t, "/nutrition/meals/"+alts[0].ID.String()+"/options", url.Values{"option_label": {"Jantar leve"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	alts = f.reload(t, plan.ID).Days[0].Meals[0].Alternatives
	if len(alts) != 3 || alts[2].OptionLabel != "Jantar leve" {
		t.Fatalf("alternatives = %+v", alts)
	}
}
