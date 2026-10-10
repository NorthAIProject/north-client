package planimport

import (
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/meals"
	"github.com/NorthAIProject/north-client/internal/quota"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// API is plan import for native clients: upload a file, get a draft back,
// preview edits, commit. The same service, rules and quota as the web pages.
type API struct {
	svc    *Service
	quotas *quota.Service
}

// NewAPI builds the routes. Routes go in the JSON group; UploadRoutes in the
// upload group, whose body cap fits a file.
func NewAPI(svc *Service, quotas *quota.Service) *API { return &API{svc: svc, quotas: quotas} }

func (a *API) Routes(r chi.Router) {
	r.Post("/training/import", a.commitWorkout)
	r.Post("/nutrition/import/preview", a.previewMeal)
	r.Post("/nutrition/import", a.commitMeal)
}

func (a *API) UploadRoutes(r chi.Router) {
	r.With(a.quotas.GuardJSON(quota.PlanImport)).Post("/training/import/parse", a.parseWorkout)
	r.With(a.quotas.GuardJSON(quota.PlanImport)).Post("/nutrition/import/parse", a.parseMeal)
}

// ImportedPlan answers a commit with the new plan's id; the client loads it
// through the ordinary plan endpoints, the same as a plan made by hand.
type ImportedPlan struct {
	PlanID uuid.UUID `json:"planId"`
}

// MealCommitRequest is a reviewed meal draft and the person's answer to any
// overage it has.
type MealCommitRequest struct {
	Draft          MealDraft `json:"draft"`
	ConfirmOverage bool      `json:"confirmOverage"`
}

// MealCommitRefusal is the 422 for a meal commit: the usual error shape, plus
// the draft as the server previewed it, so the client can show each reason
// beside the line or day it belongs to. A day over its target is a 409
// MacroOverage instead, as everywhere else in meal plans.
type MealCommitRefusal struct {
	Error httpx.ErrorDetail `json:"error"`
	Draft MealDraft         `json:"draft"`
}

func (a *API) parseWorkout(w http.ResponseWriter, r *http.Request) {
	name, data, ok := readUpload(w, r)
	if !ok {
		return
	}
	draft, err := a.svc.ParseWorkout(r.Context(), auth.MustUser(r.Context()), name, data, "")
	if err != nil {
		writeError(w, err, "The file could not be imported.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, draft)
}

func (a *API) parseMeal(w http.ResponseWriter, r *http.Request) {
	name, data, ok := readUpload(w, r)
	if !ok {
		return
	}
	draft, err := a.svc.ParseMeal(r.Context(), auth.MustUser(r.Context()), name, data, "")
	if err != nil {
		writeError(w, err, "The file could not be imported.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, draft)
}

func (a *API) commitWorkout(w http.ResponseWriter, r *http.Request) {
	var draft WorkoutDraft
	if err := httpx.ReadJSON(w, r, &draft, httpx.ReadOptions{}); err != nil {
		httpx.Error(w, err, "That plan could not be read.")
		return
	}
	stored, err := a.svc.CommitWorkout(r.Context(), auth.MustUser(r.Context()), draft)
	if err != nil {
		writeError(w, err, "The plan could not be saved.")
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, ImportedPlan{PlanID: stored.ID})
}

func (a *API) previewMeal(w http.ResponseWriter, r *http.Request) {
	var draft MealDraft
	if err := httpx.ReadJSON(w, r, &draft, httpx.ReadOptions{}); err != nil {
		httpx.Error(w, err, "That plan could not be read.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, a.svc.PreviewMeal(r.Context(), auth.MustUser(r.Context()).ID, draft))
}

func (a *API) commitMeal(w http.ResponseWriter, r *http.Request) {
	var req MealCommitRequest
	if err := httpx.ReadJSON(w, r, &req, httpx.ReadOptions{}); err != nil {
		httpx.Error(w, err, "That plan could not be read.")
		return
	}

	saved, previewed, err := a.svc.CommitMeal(r.Context(), auth.MustUser(r.Context()), req.Draft, req.ConfirmOverage)
	if err == nil {
		httpx.WriteJSON(w, http.StatusCreated, ImportedPlan{PlanID: saved.ID})
		return
	}

	var over *meals.OverageError
	var fields apperr.FieldErrors
	switch {
	case apperr.As(err, &over):
		// The same 409 every other change to a meal plan gets; canConfirm says
		// whether asking again with confirmOverage will save it.
		httpx.WriteJSON(w, http.StatusConflict, meals.ProjectOverage(over))
	case apperr.As(err, &fields):
		httpx.WriteJSON(w, http.StatusUnprocessableEntity, MealCommitRefusal{
			Error: httpx.ErrorDetail{Message: "Some foods need attention before saving.", Fields: fields.Messages()},
			Draft: previewed,
		})
	default:
		httpx.Error(w, err, "The plan could not be saved.")
	}
}

// readUpload reads the one file an import takes. The body is capped here as
// well as by the route group, whose cap is sized for video.
func readUpload(w http.ResponseWriter, r *http.Request) (string, []byte, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, MaxBytes+(1<<20))
	if err := r.ParseMultipartForm(4 << 20); err != nil {
		msg := "That upload could not be read."
		var tooBig *http.MaxBytesError
		if apperr.As(err, &tooBig) {
			msg = "This file is larger than 10 MB."
		}
		httpx.Error(w, apperr.FieldErrors{}.Add("file", msg), msg)
		return "", nil, false
	}
	defer func() { _ = r.MultipartForm.RemoveAll() }()

	file, header, err := r.FormFile("file")
	if err != nil {
		httpx.Error(w, apperr.FieldErrors{}.Add("file", "Choose a file first."), "Choose a file first.")
		return "", nil, false
	}
	defer func() { _ = file.Close() }()

	data, err := io.ReadAll(io.LimitReader(file, MaxBytes+1))
	if err != nil {
		httpx.Error(w, apperr.FieldErrors{}.Add("file", "That upload could not be read."), "That upload could not be read.")
		return "", nil, false
	}
	return header.Filename, data, true
}

// writeError answers with the refusal's own sentence when there is one: the
// person needs "this PDF is password-protected", not "could not be imported".
func writeError(w http.ResponseWriter, err error, fallback string) {
	var fe *FileError
	if apperr.As(err, &fe) {
		httpx.Error(w, err, fe.Message)
		return
	}
	var fields apperr.FieldErrors
	if apperr.As(err, &fields) && len(fields) > 0 {
		httpx.Error(w, err, fields[0].Message)
		return
	}
	httpx.Error(w, err, fallback)
}
