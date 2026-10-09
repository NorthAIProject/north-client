package medications

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	carepages "github.com/NorthAIProject/north-client/web/care"
)

// CarePage re-renders the care page after a write, the way the page's own
// writes do. care.Handler satisfies it. An interface rather than the type,
// because care composes this slice and importing it back would be a cycle.
type CarePage interface {
	Done(w http.ResponseWriter, r *http.Request)
	Rejected(w http.ResponseWriter, r *http.Request, form carepages.MedicationForm)
	Fail(w http.ResponseWriter, r *http.Request, err error)
}

// Handler takes the medications card's writes on the care page.
type Handler struct {
	svc  *Service
	page CarePage
}

func NewHandler(svc *Service, page CarePage) *Handler { return &Handler{svc: svc, page: page} }

func (h *Handler) Routes(r chi.Router) {
	r.Post("/care/medications", h.add)
	r.Post("/care/medications/{id}/stop", h.stop)
	r.Post("/care/medications/{id}/doses", h.logDose)
	r.Post("/care/medications/doses/{id}/undo", h.undoDose)
}

func (h *Handler) add(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.page.Fail(w, r, apperr.ErrValidation)
		return
	}
	form := carepages.MedicationForm{
		Name: r.PostFormValue("name"), Dose: r.PostFormValue("dose"), Times: r.PostFormValue("times"),
	}
	_, err := h.svc.Add(r.Context(), auth.MustUser(r.Context()), Input{
		Name: form.Name, Dose: form.Dose, Times: splitTimes(form.Times), Remind: true,
	})
	if err != nil {
		var fieldErrs apperr.FieldErrors
		if apperr.As(err, &fieldErrs) {
			form.Errors = fieldErrs.Messages()
			h.page.Rejected(w, r, form)
			return
		}
		h.page.Fail(w, r, err)
		return
	}
	h.page.Done(w, r)
}

func (h *Handler) stop(w http.ResponseWriter, r *http.Request) {
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}
	if _, err := h.svc.Stop(r.Context(), auth.MustUser(r.Context()), id); err != nil {
		h.page.Fail(w, r, err)
		return
	}
	h.page.Done(w, r)
}

func (h *Handler) logDose(w http.ResponseWriter, r *http.Request) {
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		h.page.Fail(w, r, apperr.ErrValidation)
		return
	}
	var slot *string
	if s := r.PostFormValue("slot"); s != "" {
		slot = &s
	}
	if _, err := h.svc.LogDose(r.Context(), auth.MustUser(r.Context()), id, r.PostFormValue("status"), slot); err != nil {
		h.page.Fail(w, r, err)
		return
	}
	h.page.Done(w, r)
}

func (h *Handler) undoDose(w http.ResponseWriter, r *http.Request) {
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}
	if err := h.svc.UndoDose(r.Context(), auth.MustUser(r.Context()), id); err != nil {
		h.page.Fail(w, r, err)
		return
	}
	h.page.Done(w, r)
}

func (h *Handler) pathID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.page.Fail(w, r, apperr.ErrNotFound)
		return uuid.Nil, false
	}
	return id, true
}

// splitTimes reads "08:00, 20:00" or "08:00 20:00" from the one times input.
// Validation of each entry is the service's.
func splitTimes(raw string) []string {
	return strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ' ' || r == ';' })
}
