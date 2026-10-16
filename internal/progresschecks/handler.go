package progresschecks

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/progresschecks/progresscheck"
	"github.com/NorthAIProject/north-client/internal/quota"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	"github.com/NorthAIProject/north-client/internal/users"
	progresspages "github.com/NorthAIProject/north-client/web/progress"
)

// Handler is the progress-check page. Must be behind RequireAuth.
type Handler struct {
	svc    *Service
	quotas *quota.Service
}

func NewHandler(svc *Service, quotas *quota.Service) *Handler {
	return &Handler{svc: svc, quotas: quotas}
}

func (h *Handler) Routes(r chi.Router) {
	r.Get("/progress", h.show)
	r.Post("/progress", h.create)
	r.Post("/progress/schedule", h.setSchedule)
	r.Post("/progress/{id}/delete", h.delete)
	r.With(h.quotas.Guard(quota.ProgressEstimate)).Post("/progress/estimate", h.estimate)
	r.With(h.quotas.Guard(quota.ProgressEstimate)).Post("/progress/compare", h.compare)
}

func (h *Handler) show(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, http.StatusOK, progresspages.Form{})
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	r.Body = http.MaxBytesReader(w, r.Body, maxCheckUpload)
	if err := r.ParseMultipartForm(4 << 20); err != nil {
		h.render(w, r, http.StatusUnprocessableEntity, progresspages.Form{
			Errors: map[string]string{"front": i18n.T(r.Context(), "progress.error.upload")},
		})
		return
	}
	defer func() { _ = r.MultipartForm.RemoveAll() }()

	form := readForm(r)
	in, err := formInput(form)
	if err == nil {
		uploads, closeAll, photoErr := formPhotos(r)
		defer closeAll()
		if err = photoErr; err == nil {
			_, err = h.svc.Save(r.Context(), user, in, uploads)
		}
	}
	if err != nil {
		var fe apperr.FieldErrors
		if apperr.As(err, &fe) {
			form.Errors = fe.Messages()
			h.render(w, r, http.StatusUnprocessableEntity, form)
			return
		}
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/app/progress", http.StatusSeeOther)
}

// estimate reads measurements off the chosen photos and answers the form's
// fields, filled where they were empty. Nothing is stored; the person saves.
// Every handled outcome renders the fields, at 422 when refused, so htmx
// swaps the message into place.
func (h *Handler) estimate(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	r.Body = http.MaxBytesReader(w, r.Body, maxCheckUpload)
	if err := r.ParseMultipartForm(4 << 20); err != nil {
		h.renderFields(w, r, http.StatusUnprocessableEntity, progresspages.Form{
			Errors: map[string]string{"front": i18n.T(r.Context(), "progress.error.upload")},
		})
		return
	}
	defer func() { _ = r.MultipartForm.RemoveAll() }()

	form := readForm(r)
	uploads, closeAll, err := formPhotos(r)
	defer closeAll()
	var est Estimate
	if err == nil {
		est, _, err = h.svc.Estimate(r.Context(), user, uploads)
	}
	if err != nil {
		var fe apperr.FieldErrors
		switch {
		case apperr.As(err, &fe):
			form.Errors = fe.Messages()
		case errors.Is(err, ErrAssistUnavailable):
			form.EstimateError = i18n.T(r.Context(), "progress.estimate.unavailable")
		default:
			middleware.FromContext(r.Context()).Warn("progress estimate failed", slog.Any("error", err))
			form.EstimateError = i18n.T(r.Context(), "progress.estimate.failed")
		}
		h.renderFields(w, r, http.StatusUnprocessableEntity, form)
		return
	}
	h.renderFields(w, r, http.StatusOK, applyEstimate(form, est))
}

// applyEstimate fills the empty fields. A number the person typed is a tape
// reading and outranks a model's estimate, so it stays.
func applyEstimate(form progresspages.Form, est Estimate) progresspages.Form {
	note := &progresspages.EstimateNote{Confidence: est.Confidence, Notes: est.Notes}
	for _, f := range []struct {
		dst *string
		v   *float64
	}{
		{&form.Waist, est.WaistCm}, {&form.Hips, est.HipsCm}, {&form.Chest, est.ChestCm},
	} {
		switch {
		case f.v == nil:
		case strings.TrimSpace(*f.dst) == "":
			*f.dst = number(*f.v)
			note.Filled++
		default:
			note.Kept++
		}
	}
	if note.Filled > 0 {
		form.Source = progresscheck.SourceAssisted
		form.AssistConfidence, form.AssistNotes = est.Confidence, est.Notes
	}
	form.Estimate = note
	return form
}

// readForm is the new-check form's text fields as posted.
func readForm(r *http.Request) progresspages.Form {
	return progresspages.Form{
		MeasuredOn: r.FormValue("measured_on"),
		Weight:     r.FormValue("weight"), Waist: r.FormValue("waist"), Hips: r.FormValue("hips"),
		Chest: r.FormValue("chest"), Thigh: r.FormValue("thigh"), Arm: r.FormValue("arm"),
		Source: r.FormValue("source"), AssistConfidence: r.FormValue("assist_confidence"),
		AssistNotes: r.FormValue("assist_notes"),
	}
}

func (h *Handler) renderFields(w http.ResponseWriter, r *http.Request, status int, form progresspages.Form) {
	user := auth.MustUser(r.Context())
	assist, err := h.assist(r.Context(), user)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	today := localDate(user, time.Now()).Format(time.DateOnly)
	if err := progresspages.Fields(today, assist, form).Render(r.Context(), w); err != nil {
		middleware.FromContext(r.Context()).Error("render progress fields", slog.Any("error", err))
	}
}

// assist is where this person's photos would go for an estimate.
func (h *Handler) assist(ctx context.Context, user users.User) (progresspages.Assist, error) {
	dest, err := h.svc.AssistDestination(ctx, user)
	if errors.Is(err, ErrAssistUnavailable) {
		return progresspages.Assist{}, nil
	}
	if err != nil {
		return progresspages.Assist{}, err
	}
	return progresspages.Assist{Available: true, Label: dest.Label, Own: dest.Own}, nil
}

func (h *Handler) setSchedule(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	if err := r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}
	var err error
	if raw := r.PostFormValue("cadence"); raw == "" {
		err = h.svc.StopSchedule(r.Context(), user)
	} else if cadence, ok := progresscheck.ParseCadence(raw); ok {
		_, err = h.svc.SetCadence(r.Context(), user, cadence)
	} else {
		err = apperr.ErrValidation
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/app/progress", http.StatusSeeOther)
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}
	if err := h.svc.Delete(r.Context(), auth.MustUser(r.Context()), id); err != nil {
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/app/progress", http.StatusSeeOther)
}

// compare describes what changed between the first and latest checks with
// photos, saves it, and shows the page with it. A refusal renders the page
// with the reason beside the button.
func (h *Handler) compare(w http.ResponseWriter, r *http.Request) {
	_, err := h.svc.Compare(r.Context(), auth.MustUser(r.Context()))
	if err == nil {
		http.Redirect(w, r, "/app/progress#progress-compare", http.StatusSeeOther)
		return
	}
	var fe apperr.FieldErrors
	var msg string
	switch {
	case apperr.As(err, &fe):
		msg = i18n.T(r.Context(), "progress.compare.need")
	case errors.Is(err, ErrAssistUnavailable):
		msg = i18n.T(r.Context(), "progress.compare.unavailable")
	default:
		middleware.FromContext(r.Context()).Warn("progress compare failed", slog.Any("error", err))
		msg = i18n.T(r.Context(), "progress.compare.failed")
	}
	h.renderPage(w, r, http.StatusUnprocessableEntity, progresspages.Form{}, msg)
}

func (h *Handler) render(w http.ResponseWriter, r *http.Request, status int, form progresspages.Form) {
	h.renderPage(w, r, status, form, "")
}

func (h *Handler) renderPage(w http.ResponseWriter, r *http.Request, status int, form progresspages.Form, compareErr string) {
	user := auth.MustUser(r.Context())
	data, err := h.page(r, user, form)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	data.CompareError = compareErr
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := progresspages.Page(user, data).Render(r.Context(), w); err != nil {
		middleware.FromContext(r.Context()).Error("render progress", slog.Any("error", err))
	}
}

func (h *Handler) page(r *http.Request, user users.User, form progresspages.Form) (progresspages.Data, error) {
	ctx := r.Context()
	now := time.Now()
	checks, err := h.svc.List(ctx, user, DefaultLimit)
	if err != nil {
		return progresspages.Data{}, err
	}
	sched, due, err := h.svc.Due(ctx, user, now)
	if err != nil {
		return progresspages.Data{}, err
	}

	data := progresspages.Data{
		Today:   localDate(user, now).Format(time.DateOnly),
		Cadence: string(sched.Cadence), Due: due,
		Checks: make([]progresspages.Check, len(checks)),
		Form:   form,
	}
	if sched.Cadence != "" {
		data.NextDueOn = i18n.Date(ctx, sched.NextDueOn, "Mon 2 Jan")
	}
	for i, c := range checks {
		var older *Check
		if i+1 < len(checks) {
			older = &checks[i+1]
		}
		data.Checks[i] = pageCheck(r, c, older)
	}
	data.Last = lastPhotos(data.Checks)
	if data.Assist, err = h.assist(ctx, user); err != nil {
		return progresspages.Data{}, err
	}
	if data.CanCompare, err = h.svc.CanCompare(ctx, user); err != nil {
		return progresspages.Data{}, err
	}
	cmp, stale, ok, err := h.svc.SavedComparison(ctx, user)
	if err != nil {
		return progresspages.Data{}, err
	}
	if ok {
		data.Comparison = &progresspages.Comparison{
			From: i18n.Date(ctx, cmp.FromOn, "2 Jan 2006"), To: i18n.Date(ctx, cmp.ToOn, "2 Jan 2006"),
			Summary: cmp.Summary, Confidence: cmp.Confidence, Limits: cmp.Limits,
			Provider: cmp.Provider, Stale: stale,
		}
	}
	return data, nil
}

// pageCheck is a check as the history shows it, each number against the
// check before it.
func pageCheck(r *http.Request, c Check, older *Check) progresspages.Check {
	out := progresspages.Check{
		ID: c.ID.String(), Date: i18n.Date(r.Context(), c.MeasuredOn, "2 Jan 2006"),
		Assisted: c.Source == progresscheck.SourceAssisted, Complete: c.Complete(),
	}
	for _, f := range []struct {
		key, unit string
		get       func(Measurements) *float64
	}{
		{"progress.m.weight", "kg", func(m Measurements) *float64 { return m.WeightKg }},
		{"progress.m.waist", "cm", func(m Measurements) *float64 { return m.WaistCm }},
		{"progress.m.hips", "cm", func(m Measurements) *float64 { return m.HipsCm }},
		{"progress.m.chest", "cm", func(m Measurements) *float64 { return m.ChestCm }},
		{"progress.m.thigh", "cm", func(m Measurements) *float64 { return m.ThighCm }},
		{"progress.m.arm", "cm", func(m Measurements) *float64 { return m.ArmCm }},
	} {
		v := f.get(c.Measurements)
		if v == nil {
			continue
		}
		chip := progresspages.Chip{Key: f.key, Value: readNumber(r.Context(), *v) + " " + f.unit}
		if f.unit == "kg" && c.WeightCarried {
			chip.Carried = true
		} else if older != nil {
			if was := f.get(older.Measurements); was != nil && (f.unit != "kg" || !older.WeightCarried) {
				chip.Delta = delta(r.Context(), *v-*was)
			}
		}
		out.Chips = append(out.Chips, chip)
	}
	for _, p := range c.Photos {
		out.Photos = append(out.Photos, progresspages.Photo{Pose: string(p.Pose), URL: "/app/media/" + p.MediaID.String()})
	}
	return out
}

// formInput reads the number fields. A blank field is not measured; anything
// else must be a number, with a comma accepted as the decimal mark.
func formInput(f progresspages.Form) (Input, error) {
	var in Input
	var errs apperr.FieldErrors
	if f.MeasuredOn != "" {
		on, err := time.Parse(time.DateOnly, f.MeasuredOn)
		if err != nil {
			errs = errs.Add("measured_on", "The date is not valid.")
		}
		in.MeasuredOn = on
	}
	for _, field := range []struct {
		raw, key string
		dst      **float64
	}{
		{f.Weight, "weight_kg", &in.WeightKg},
		{f.Waist, "waist_cm", &in.WaistCm},
		{f.Hips, "hips_cm", &in.HipsCm},
		{f.Chest, "chest_cm", &in.ChestCm},
		{f.Thigh, "thigh_cm", &in.ThighCm},
		{f.Arm, "arm_cm", &in.ArmCm},
	} {
		raw := strings.ReplaceAll(strings.TrimSpace(field.raw), ",", ".")
		if raw == "" {
			continue
		}
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			errs = errs.Add(field.key, "Enter a number.")
			continue
		}
		*field.dst = &v
	}
	in.Source = f.Source
	if in.Source == progresscheck.SourceAssisted {
		in.AssistConfidence, in.AssistNotes = f.AssistConfidence, f.AssistNotes
	}
	return in, errs.OrNil()
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case apperr.Is(err, apperr.ErrNotFound):
		http.Error(w, i18n.T(r.Context(), "progress.error.notfound"), http.StatusNotFound)
	case apperr.Is(err, apperr.ErrValidation):
		http.Error(w, i18n.T(r.Context(), "progress.error.invalid"), http.StatusUnprocessableEntity)
	default:
		middleware.FromContext(r.Context()).Error("progress check request failed", slog.Any("error", err))
		http.Error(w, i18n.T(r.Context(), "progress.error"), http.StatusInternalServerError)
	}
}

// number is a measurement as a form field holds it: always a decimal point,
// because the browser parses it back.
func number(v float64) string {
	return strings.TrimSuffix(fmt.Sprintf("%.1f", v), ".0")
}

// readNumber is a measurement as a person reads it, in their decimal mark.
func readNumber(ctx context.Context, v float64) string {
	s := i18n.Decimal(ctx, v, 1)
	return strings.TrimSuffix(strings.TrimSuffix(s, ".0"), ",0")
}

// delta is a change with its sign; no change is not shown.
func delta(ctx context.Context, d float64) string {
	d = math.Round(d*10) / 10
	switch {
	case d > 0:
		return "+" + readNumber(ctx, d)
	case d < 0:
		return "−" + readNumber(ctx, -d)
	default:
		return ""
	}
}

// lastPhotos is, for each pose, the newest photo any check has: a check saved
// with numbers only still leaves the last photos to compare against.
func lastPhotos(checks []progresspages.Check) *progresspages.Check {
	var last progresspages.Check
	for _, pose := range progresscheck.Poses {
		for _, c := range checks {
			if p, ok := c.Photo(string(pose)); ok {
				last.Photos = append(last.Photos, p)
				break
			}
		}
	}
	if len(last.Photos) == 0 {
		return nil
	}
	return &last
}
