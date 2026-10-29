package onboarding_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/onboarding"
	"github.com/NorthAIProject/north-client/internal/users"
)

func apiAs(user users.User) http.Handler {
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			next.ServeHTTP(w, req.WithContext(auth.ContextWithUser(req.Context(), user)))
		})
	})
	onboarding.NewAPI(onboarding.NewService(nil, nil, nil)).Routes(r)
	return r
}

// The form offers exactly what ValidateAnswers accepts, and no first-week
// templates when no first week will start.
func TestOnboardingFormOffersTheValidatedChoices(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	apiAs(users.User{ID: uuid.New()}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/onboarding", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got onboarding.FormResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.NeedsOnboarding {
		t.Error("a new account should need onboarding")
	}
	if len(got.FocusAreas) != len(onboarding.FocusAreas) {
		t.Errorf("focus areas = %v", got.FocusAreas)
	}
	if len(got.CoachingStyles) != 4 || got.CoachingStyles[0] != onboarding.StyleDirect {
		t.Errorf("coaching styles = %v", got.CoachingStyles)
	}
	if got.FirstWeek || len(got.FirstWeekTemplates) != 0 {
		t.Errorf("first week offered without a first week service: %+v", got)
	}
	// An empty list, never null: a decoder that wants an array gets one.
	if !strings.Contains(rec.Body.String(), `"firstWeekTemplates":[]`) {
		t.Errorf("firstWeekTemplates not an empty array: %s", rec.Body)
	}
}

// Skipping an account that already finished changes nothing and needs no
// database, the same early return the Go page's skip takes.
func TestSkipOnboardingIsIdempotent(t *testing.T) {
	t.Parallel()

	done := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	user := users.User{ID: uuid.New(), Email: "ana@example.com", OnboardedAt: &done}

	rec := httptest.NewRecorder()
	apiAs(user).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/onboarding/skip", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	var got onboarding.SkipResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.User.NeedsOnboarding || got.User.ID != user.ID {
		t.Fatalf("user = %+v", got.User)
	}
}
