package lifts

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/lifts/lift"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	pages "github.com/NorthAIProject/north-client/web/lifts"
)

// MaxImportBytes caps a Hevy export. Years of training are a few megabytes.
const MaxImportBytes = 10 << 20

// Handler is the web's set logger and Hevy import. Must be behind
// RequireAuth.
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Routes(r chi.Router) {
	r.Get("/lifts/today", h.today)
	r.Post("/lifts/sets", h.logSet)
	r.Post("/lifts/sets/{setID}/delete", h.undoSet)
	r.Get("/lifts/import", h.importPage)
	r.Post("/lifts/import", h.importUpload)
}

func (h *Handler) today(w http.ResponseWriter, r *http.Request) {
	h.renderToday(w, r, pages.SetForm{}, "")
}

// logSet records the form's set and answers with the card, keeping the
// exercise, weight, reps and kind filled in for the next set. A refusal
// answers 200 with the message in the card, because HTMX swaps only
// successful responses.
func (h *Handler) logSet(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.renderToday(w, r, pages.SetForm{}, "That form could not be read.")
		return
	}
	user := auth.MustUser(r.Context())
	form := pages.SetForm{
		Exercise: strings.TrimSpace(r.PostFormValue("exercise")),
		WeightKg: strings.TrimSpace(r.PostFormValue("weight_kg")),
		Reps:     strings.TrimSpace(r.PostFormValue("reps")),
		Kind:     r.PostFormValue("kind"),
		RIR:      strings.TrimSpace(r.PostFormValue("rir")),
	}
	in, msg := parseSetForm(form)
	if msg != "" {
		h.renderToday(w, r, form, msg)
		return
	}

	slug, err := h.svc.MatchExercise(r.Context(), in.ExerciseName)
	if err != nil {
		h.fail(w, r, "match exercise", err)
		return
	}
	in.ExerciseSlug = slug
	if in.SetNumber, err = h.svc.NextSetNumber(r.Context(), user, slug, in.ExerciseName); err != nil {
		h.fail(w, r, "next set number", err)
		return
	}
	if _, err = h.svc.Log(r.Context(), user, in); err != nil {
		var fields apperr.FieldErrors
		if apperr.As(err, &fields) && len(fields) > 0 {
			h.renderToday(w, r, form, fields[0].Message)
			return
		}
		h.fail(w, r, "log set", err)
		return
	}
	form.RIR = "" // effort is per set; the rest carries over
	h.renderToday(w, r, form, "")
}

// parseSetForm reads the form into a LogInput, or a message for the person.
func parseSetForm(f pages.SetForm) (LogInput, string) {
	in := LogInput{ExerciseName: f.Exercise, Kind: f.Kind}
	if in.ExerciseName == "" {
		return in, "Name the exercise."
	}
	if f.WeightKg != "" {
		kg, err := strconv.ParseFloat(strings.ReplaceAll(f.WeightKg, ",", "."), 64)
		if err != nil {
			return in, "Enter the weight in kilograms, or leave it empty for bodyweight."
		}
		in.WeightKg = kg
	}
	reps, err := strconv.Atoi(f.Reps)
	if err != nil {
		return in, "Enter how many reps."
	}
	in.Reps = reps
	if f.RIR != "" {
		rir, err := strconv.Atoi(f.RIR)
		if err != nil {
			return in, "Enter reps in reserve as a whole number, or leave it empty."
		}
		in.RIR = &rir
	}
	return in, ""
}

func (h *Handler) undoSet(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "setID"))
	if err != nil {
		h.renderToday(w, r, pages.SetForm{}, "That set was not found.")
		return
	}
	if err := h.svc.Undo(r.Context(), auth.MustUser(r.Context()), id); err != nil && !apperr.Is(err, apperr.ErrNotFound) {
		h.fail(w, r, "undo set", err)
		return
	}
	h.renderToday(w, r, pages.SetForm{}, "")
}

func (h *Handler) renderToday(w http.ResponseWriter, r *http.Request, form pages.SetForm, msg string) {
	user := auth.MustUser(r.Context())
	sets, recent, err := h.svc.Today(r.Context(), user)
	if err != nil {
		h.fail(w, r, "load today's sets", err)
		return
	}
	h.render(w, r, http.StatusOK, pages.TodayCard(pages.Today{Sets: setRows(sets), Recent: recent, Form: form, Error: msg}))
}

func setRows(sets []Set) []pages.SetRow {
	rows := make([]pages.SetRow, len(sets))
	for i, s := range sets {
		row := pages.SetRow{
			ID: s.ID.String(), Exercise: s.ExerciseName, Load: loadLabel(s),
			Warmup: s.Kind == lift.KindWarmup, Drop: s.Kind == lift.KindDrop,
		}
		if s.RIR != nil {
			row.RIR = fmt.Sprintf("%d in reserve", *s.RIR)
			if *s.RIR == 0 {
				row.RIR = "to failure"
			}
		}
		rows[i] = row
	}
	return rows
}

func loadLabel(s Set) string {
	if s.WeightKg <= 0 {
		return fmt.Sprintf("bodyweight × %d", s.Reps)
	}
	return fmt.Sprintf("%s kg × %d", strconv.FormatFloat(s.WeightKg, 'f', -1, 64), s.Reps)
}

func (h *Handler) importPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, http.StatusOK, pages.ImportPage(auth.MustUser(r.Context()), nil, ""))
}

func (h *Handler) importUpload(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	data, msg := formFile(r)
	if msg != "" {
		h.render(w, r, http.StatusUnprocessableEntity, pages.ImportPage(user, nil, msg))
		return
	}
	res, err := h.svc.ImportHevy(r.Context(), user, bytes.NewReader(data))
	if err != nil {
		var fields apperr.FieldErrors
		if apperr.As(err, &fields) && len(fields) > 0 {
			h.render(w, r, http.StatusUnprocessableEntity, pages.ImportPage(user, nil, fields[0].Message))
			return
		}
		middleware.FromContext(r.Context()).Error("import hevy", slog.Any("error", err))
		h.render(w, r, http.StatusInternalServerError, pages.ImportPage(user, imported(res),
			"The import stopped part way. The workouts counted above were saved; the rest were not."))
		return
	}
	h.render(w, r, http.StatusOK, pages.ImportPage(user, imported(res), ""))
}

func imported(res ImportResult) *pages.Imported {
	return &pages.Imported{Workouts: res.Workouts, Duplicates: res.Duplicates, Sets: res.Sets, Skipped: res.Skipped, Unmatched: res.Unmatched}
}

// formFile reads the upload the CSRF middleware has already parsed.
func formFile(r *http.Request) ([]byte, string) {
	file, _, err := r.FormFile("file")
	if err != nil {
		return nil, "Choose your Hevy CSV first."
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, MaxImportBytes+1))
	if err != nil {
		return nil, "That upload could not be read."
	}
	if len(data) > MaxImportBytes {
		return nil, "This file is larger than 10 MB."
	}
	return data, ""
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, what string, err error) {
	middleware.FromContext(r.Context()).Error(what, slog.Any("error", err))
	http.Error(w, "Something went wrong. Try again in a moment.", httpx.Status(err))
}

func (h *Handler) render(w http.ResponseWriter, r *http.Request, status int, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := c.Render(r.Context(), w); err != nil {
		middleware.FromContext(r.Context()).Error("render lifts", slog.Any("error", err))
	}
}
