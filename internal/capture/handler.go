package capture

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/quota"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/htmx"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	capturepages "github.com/NorthAIProject/north-client/web/capture"
)

// Handler serves the capture page: read a sentence, show what it means, write
// what the person agreed to.
type Handler struct {
	svc    *Service
	quotas *quota.Service
}

func NewHandler(svc *Service, quotas *quota.Service) *Handler {
	return &Handler{svc: svc, quotas: quotas}
}

// Routes registers the page.
//
// Only the parse is metered: it is the half that reaches a model. The commit
// costs a transaction, and refusing it after somebody has already paid for the
// parse is the worst possible place to stop them.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/capture", h.show)
	r.With(h.quotas.Guard(quota.QuickCapture)).Post("/capture/parse", h.parse)
	r.Post("/capture/commit", h.commit)
}

// show renders the empty box, or one prefilled from the PWA share target.
func (h *Handler) show(w http.ResponseWriter, r *http.Request) {
	text := strings.TrimSpace(r.URL.Query().Get("text"))

	// A share carries a title and a link as well as the text; joining them is
	// what makes "share this to Khepri" work from a browser rather than only
	// from a notes app.
	if title := strings.TrimSpace(r.URL.Query().Get("title")); title != "" && title != text {
		text = strings.TrimSpace(title + " " + text)
	}
	if link := strings.TrimSpace(r.URL.Query().Get("url")); link != "" {
		text = strings.TrimSpace(text + " " + link)
	}
	if len(text) > MaxText {
		text = text[:MaxText]
	}

	h.render(w, r, http.StatusOK, capturepages.Data{Text: text})
}

func (h *Handler) parse(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	if err := r.ParseForm(); err != nil {
		h.render(w, r, http.StatusUnprocessableEntity, capturepages.Data{Error: "That did not arrive properly. Try again."})
		return
	}

	text := strings.TrimSpace(r.PostFormValue("text"))
	draft, err := h.svc.Parse(r.Context(), user, text)
	if err != nil {
		// The sentence is kept: a failed parse must never cost somebody their
		// words.
		h.render(w, r, statusFor(err), capturepages.Data{Text: text, Error: message(err)})
		return
	}

	if len(draft.Items) == 0 && len(draft.Unparsed) == 0 {
		h.render(w, r, http.StatusOK, capturepages.Data{
			Text:  text,
			Error: "I could not find anything to log in that.",
		})
		return
	}

	h.render(w, r, http.StatusOK, capturepages.Data{Text: text, Draft: draft, HasDraft: true})
}

func (h *Handler) commit(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	if err := r.ParseForm(); err != nil {
		h.render(w, r, http.StatusUnprocessableEntity, capturepages.Data{Error: "That did not arrive properly. Try again."})
		return
	}

	text := strings.TrimSpace(r.PostFormValue("text"))
	items, err := itemsFromForm(r)
	if err != nil {
		h.render(w, r, http.StatusUnprocessableEntity, capturepages.Data{Text: text, Error: message(err)})
		return
	}

	receipt, err := h.svc.Commit(r.Context(), user, items)
	if err != nil {
		h.render(w, r, statusFor(err), capturepages.Data{Text: text, Error: message(err)})
		return
	}

	h.render(w, r, http.StatusOK, capturepages.Data{Receipt: receipt, HasSaved: true})
}

func (h *Handler) render(w http.ResponseWriter, r *http.Request, status int, data capturepages.Data) {
	user := auth.MustUser(r.Context())
	ctx := r.Context()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)

	var component templ.Component
	if htmx.IsRequest(r) {
		component = capturepages.Panel(data)
	} else {
		component = capturepages.Page(user, data)
	}
	if err := component.Render(ctx, w); err != nil {
		middleware.FromContext(ctx).Error("render capture", slog.Any("error", err))
	}
}

// itemsFromForm rebuilds the reviewed items from the posted preview.
//
// Nothing here is trusted to still be what was sent: every value goes through
// Validate, and the ids are re-checked against the account by the services that
// consume them. A person editing a hidden field can only write something they
// could already type into the care page, so this is about correctness rather
// than privilege.
func itemsFromForm(r *http.Request) ([]Item, error) {
	var items []Item

	for i := 0; i < MaxItems; i++ {
		kind := Kind(strings.TrimSpace(r.PostFormValue(field(i, "kind"))))
		if kind == "" {
			continue
		}
		// An unchecked row is a row the person declined. Silently writing it
		// would make the checkbox a lie.
		if r.PostFormValue(field(i, "include")) == "" {
			continue
		}

		item, err := itemFromForm(r, i, kind)
		if err != nil {
			return nil, err
		}

		clean, err := Validate(item)
		if err != nil {
			return nil, err
		}
		items = append(items, clean)
	}

	if len(items) == 0 {
		return nil, apperr.Wrap(apperr.ErrValidation, "nothing was ticked, so nothing was logged")
	}
	return items, nil
}

func itemFromForm(r *http.Request, i int, kind Kind) (Item, error) {
	item := Item{
		Kind:    kind,
		Source:  r.PostFormValue(field(i, "source")),
		Problem: r.PostFormValue(field(i, "problem")),
	}

	switch kind {
	case KindWater:
		item.Water = &Water{AmountML: atoi(r.PostFormValue(field(i, "amount_ml")))}

	case KindSleep:
		night := &Sleep{
			Minutes: atoi(r.PostFormValue(field(i, "minutes"))),
			Quality: atoi(r.PostFormValue(field(i, "quality"))),
		}
		if raw := strings.TrimSpace(r.PostFormValue(field(i, "date"))); raw != "" {
			when, err := time.Parse("2006-01-02", raw)
			if err != nil {
				return Item{}, apperr.Wrap(apperr.ErrValidation, "unreadable date %q", raw)
			}
			night.Date = when
		}
		item.Sleep = night

	case KindHabit:
		habit := &Habit{Name: r.PostFormValue(field(i, "habit_name"))}
		if raw := strings.TrimSpace(r.PostFormValue(field(i, "habit_id"))); raw != "" {
			id, err := uuid.Parse(raw)
			if err != nil {
				return Item{}, apperr.Wrap(apperr.ErrValidation, "unreadable habit")
			}
			habit.ID = id
		}
		item.Habit = habit

	case KindWeight:
		item.Weight = &Weight{KG: atof(r.PostFormValue(field(i, "kg")))}

	case KindCheckIn:
		item.CheckIn = &CheckIn{
			Mood:   atoi(r.PostFormValue(field(i, "mood"))),
			Energy: atoi(r.PostFormValue(field(i, "energy"))),
			Notes:  r.PostFormValue(field(i, "notes")),
		}

	case KindFood:
		food := &Food{
			Query:       r.PostFormValue(field(i, "food")),
			Grams:       atof(r.PostFormValue(field(i, "grams"))),
			MatchedName: r.PostFormValue(field(i, "matched_name")),
		}
		if raw := strings.TrimSpace(r.PostFormValue(field(i, "ingredient_id"))); raw != "" {
			id, err := uuid.Parse(raw)
			if err != nil {
				return Item{}, apperr.Wrap(apperr.ErrValidation, "unreadable ingredient")
			}
			food.IngredientID = id
		}
		item.Food = food

	default:
		return Item{}, apperr.Wrap(apperr.ErrValidation, "unknown capture kind %q", kind)
	}

	return item, nil
}

func field(i int, name string) string {
	return fmt.Sprintf("items[%d].%s", i, name)
}

// atoi and atof read a form number as zero rather than an error: the bounds in
// Validate are what decides whether the value is usable, and reporting "" as a
// parse failure would mean two different messages for the same empty box.
func atoi(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return n
}

func atof(s string) float64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	return f
}

func statusFor(err error) int {
	if apperr.Is(err, apperr.ErrValidation) {
		return http.StatusUnprocessableEntity
	}
	return http.StatusInternalServerError
}

// message keeps an internal failure off the screen while letting anything the
// person can act on through.
func message(err error) string {
	if apperr.Is(err, apperr.ErrValidation) || apperr.Is(err, apperr.ErrNotFound) {
		return capitalise(err.Error()) + "."
	}
	return "Something went wrong reading that. Try again."
}

// Service is the handler's service, so the JSON twin can be built from the
// same one rather than a second copy wired to the same tables.
func (h *Handler) Service() *Service { return h.svc }
