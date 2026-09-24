package onboarding

import (
	"context"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
	"github.com/NorthAIProject/north-client/internal/users"
)

// SessionResolver is the narrow bearer-session boundary needed by native
// onboarding requests.
type SessionResolver interface {
	Resolve(context.Context, string) (auth.Session, error)
}

// API exposes the questionnaire without coupling it to browser forms.
type API struct {
	svc      *Service
	sessions SessionResolver
}

func NewAPI(svc *Service, sessions SessionResolver) *API {
	return &API{svc: svc, sessions: sessions}
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
	user, ok := a.user(w, r)
	if !ok {
		return
	}

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

func (a *API) user(w http.ResponseWriter, r *http.Request) (users.User, bool) {
	token, found := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	token = strings.TrimSpace(token)
	if !found || token == "" {
		httpx.Error(w, apperr.ErrUnauthenticated, "A bearer token is required.")
		return users.User{}, false
	}

	session, err := a.sessions.Resolve(r.Context(), token)
	if err != nil {
		if apperr.Is(err, apperr.ErrUnauthenticated) || apperr.Is(err, apperr.ErrNotFound) {
			httpx.Error(w, apperr.ErrUnauthenticated, "That token is not valid.")
		} else {
			httpx.Error(w, apperr.ErrUnavailable, "Something went wrong.")
		}
		return users.User{}, false
	}
	return session.User, true
}
