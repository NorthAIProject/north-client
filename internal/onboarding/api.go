package onboarding

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// API exposes the questionnaire without coupling it to browser forms.
type API struct {
	svc *Service
}

// NewAPI builds the routes; mount them behind auth.RequireBearer.
func NewAPI(svc *Service) *API {
	return &API{svc: svc}
}

func (a *API) Routes(r chi.Router) {
	r.Post("/onboarding", a.complete)
}

type Request struct {
	FocusAreas          []string `json:"focusAreas"`
	CoachingStyle       string   `json:"coachingStyle"`
	CoachingStyleCustom string   `json:"coachingStyleCustom"`
	NearTermGoal        string   `json:"nearTermGoal"`
}

type Response struct {
	User     auth.APIUser `json:"user"`
	ThreadID string       `json:"threadId,omitempty"`
}

func (a *API) complete(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	var req Request
	if err := httpx.ReadJSON(w, r, &req, httpx.ReadOptions{MaxBytes: 64 << 10}); err != nil {
		httpx.Error(w, err, "The request body must be a valid onboarding request.")
		return
	}

	preset := strings.TrimSpace(req.CoachingStyle)
	custom := strings.TrimSpace(req.CoachingStyleCustom)
	if preset != StyleDirect && preset != StyleSupportive && preset != StyleSocratic && preset != StyleCustom {
		preset = StyleCustom
		if custom == "" {
			custom = strings.TrimSpace(req.CoachingStyle)
		}
	}
	answers, err := ValidateAnswers(req.FocusAreas, preset, custom, req.NearTermGoal)
	if err != nil {
		httpx.Error(w, err, "Please complete the required onboarding fields.")
		return
	}

	onboarded, thread, err := a.svc.Complete(r.Context(), user, answers)
	if err != nil {
		httpx.Error(w, err, "Onboarding could not be completed.")
		return
	}

	response := Response{User: auth.ProjectUser(onboarded)}
	if thread != uuid.Nil {
		response.ThreadID = thread.String()
	}
	httpx.WriteJSON(w, http.StatusOK, response)
}
