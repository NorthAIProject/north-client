package onboarding

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/firstweek"
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
	r.Get("/onboarding", a.form)
	r.Post("/onboarding", a.complete)
	r.Post("/onboarding/skip", a.skip)
}

// FormResponse is what the questionnaire offers, so a client renders the same
// choices the server validates against.
type FormResponse struct {
	// NeedsOnboarding is false once the account finished or skipped; a client
	// then has nothing to show and goes home.
	NeedsOnboarding bool `json:"needsOnboarding"`
	// FocusAreas and CoachingStyles are open strings, in the order offered.
	FocusAreas     []string `json:"focusAreas"`
	CoachingStyles []string `json:"coachingStyles"`
	// FirstWeek is true when completing starts a first week, which is when
	// the form offers the pick of FirstWeekTemplates.
	FirstWeek          bool     `json:"firstWeek"`
	FirstWeekTemplates []string `json:"firstWeekTemplates"`
}

// SkipResponse is the account after skipping.
type SkipResponse struct {
	User auth.APIUser `json:"user"`
}

func (a *API) form(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	response := FormResponse{
		NeedsOnboarding:    user.NeedsOnboarding(),
		FocusAreas:         FocusAreas,
		CoachingStyles:     []string{StyleDirect, StyleSupportive, StyleSocratic, StyleCustom},
		FirstWeek:          a.svc.FirstWeekEnabled(),
		FirstWeekTemplates: []string{},
	}
	if response.FirstWeek {
		response.FirstWeekTemplates = firstweek.Templates
	}
	httpx.WriteJSON(w, http.StatusOK, response)
}

// skip finishes onboarding without answers, as the Go form's skip button does.
// Idempotent: an onboarded account comes back unchanged.
func (a *API) skip(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	skipped, err := a.svc.Skip(r.Context(), user)
	if err != nil {
		httpx.Error(w, err, "Onboarding could not be skipped.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, SkipResponse{User: auth.ProjectUser(skipped)})
}

type Request struct {
	FocusAreas          []string `json:"focusAreas"`
	CoachingStyle       string   `json:"coachingStyle"`
	CoachingStyleCustom string   `json:"coachingStyleCustom"`
	NearTermGoal        string   `json:"nearTermGoal"`
	// FirstWeekTemplate is the change picked for the first seven days: sleep,
	// train, protein, journal or custom, as an open string. Optional; absent
	// or unknown, the week runs on nearTermGoal.
	FirstWeekTemplate string `json:"firstWeekTemplate,omitempty"`
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
	answers.FirstWeekTemplate = req.FirstWeekTemplate

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
