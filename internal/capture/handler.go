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
// The parse is metered: it is the half that reaches a model. The commit costs a
// transaction, and refusing it after somebody has already paid for the parse is
// the worst possible place to stop them. A voice note's transcription is metered
// on its own, by the shared dictation endpoint in internal/voice.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/capture", h.show)
	r.Get("/capture/panel", h.panel)
	r.With(h.quotas.Guard(quota.QuickCapture)).Post("/capture/parse", h.parse)
	r.Post("/capture/commit", h.commit)
	r.With(h.quotas.Guard(quota.QuickCapture)).Post("/capture/foods", h.foods)
}

// show renders the empty box, or one prefilled from the PWA share target.
func (h *Handler) show(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, http.StatusOK, capturepages.Data{Text: sharedText(r)})
}

// panel renders the same box as show, as a fragment, for another page to
// embed: My Day's "+" dialog and the food log load it lazily. return_to names
// the page it sits on, so a saved capture can send the browser back there and
// the page shows what was just logged.
func (h *Handler) panel(w http.ResponseWriter, r *http.Request) {
	data := capturepages.Data{Text: sharedText(r), ReturnTo: embedReturnTo(r.URL.Query().Get("return_to"))}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := capturepages.Panel(data).Render(r.Context(), w); err != nil {
		middleware.FromContext(r.Context()).Error("render capture panel", slog.Any("error", err))
	}
}

// sharedText reads a prefilled sentence from the query string.
func sharedText(r *http.Request) string {
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
	return text
}

// embeddedPages are the pages that embed the panel. A list rather than any
// local path: return_to ends up in a redirect, and an arbitrary value from a
// form field is an open redirect waiting for someone to put a URL in it.
var embeddedPages = map[string]bool{
	"/app":               true,
	"/app/nutrition/log": true,
}

// embedReturnTo returns raw when it names a page that embeds the panel, and ""
// — the standalone capture page — otherwise.
func embedReturnTo(raw string) string {
	if embeddedPages[raw] {
		return raw
	}
	return ""
}

func (h *Handler) parse(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	if err := r.ParseForm(); err != nil {
		h.render(w, r, http.StatusUnprocessableEntity, capturepages.Data{Error: "That did not arrive properly. Try again."})
		return
	}

	text := strings.TrimSpace(r.PostFormValue("text"))
	returnTo := embedReturnTo(r.PostFormValue("return_to"))
	draft, err := h.svc.Parse(r.Context(), user, text)
	if err != nil {
		// The sentence is kept: a failed parse must never cost somebody their
		// words.
		h.render(w, r, statusFor(err), capturepages.Data{Text: text, ReturnTo: returnTo, Error: message(err)})
		return
	}

	if len(draft.Items) == 0 && len(draft.Unparsed) == 0 {
		h.render(w, r, http.StatusOK, capturepages.Data{
			Text:     text,
			ReturnTo: returnTo,
			Error:    "I could not find anything to log in that.",
		})
		return
	}

	h.render(w, r, http.StatusOK, capturepages.Data{Text: text, ReturnTo: returnTo, Draft: draft, HasDraft: true})
}

func (h *Handler) commit(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	if err := r.ParseForm(); err != nil {
		h.render(w, r, http.StatusUnprocessableEntity, capturepages.Data{Error: "That did not arrive properly. Try again."})
		return
	}

	text := strings.TrimSpace(r.PostFormValue("text"))
	returnTo := embedReturnTo(r.PostFormValue("return_to"))
	items, err := itemsFromForm(r)
	if err != nil {
		h.render(w, r, http.StatusUnprocessableEntity, capturepages.Data{Text: text, ReturnTo: returnTo, Error: message(err)})
		return
	}

	receipt, err := h.svc.Commit(r.Context(), user, items)
	if err != nil {
		h.render(w, r, statusFor(err), capturepages.Data{Text: text, ReturnTo: returnTo, Error: message(err)})
		return
	}

	// Embedded, a clean save goes back to the page it was made on, which
	// re-renders with the new entries — the same landing every other form in
	// My Day's "+" dialog has. A receipt with a failure in it stays on screen
	// instead: reloading would hide the one line the person needs to read.
	if returnTo != "" && receipt.Failed() == 0 {
		goBack(w, r, returnTo)
		return
	}

	h.render(w, r, http.StatusOK, capturepages.Data{Receipt: receipt, HasSaved: true, ReturnTo: returnTo})
}

// goBack sends the browser to a page: by HX-Redirect for an HTMX request,
// which would otherwise swap the whole page into the panel, and by an ordinary
// See Other without JavaScript.
func goBack(w http.ResponseWriter, r *http.Request, to string) {
	if htmx.IsRequest(r) {
		w.Header().Set("HX-Redirect", to)
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, to, http.StatusSeeOther)
}

// foods reads a spoken or typed meal for one meal on a plan and answers the
// box with a review under it. It writes nothing; the review posts to the meal.
//
// The meal id is only carried through to that review's form action. Whose
// meal it is gets checked where the rows are written, not here, where a
// forged id could at most cost its sender a parse against their own quota.
func (h *Handler) foods(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	if err := r.ParseForm(); err != nil {
		http.Error(w, "That request could not be read.", http.StatusUnprocessableEntity)
		return
	}
	mealID, err := uuid.Parse(r.PostFormValue("meal_id"))
	if err != nil {
		http.Error(w, "Not found.", http.StatusNotFound)
		return
	}

	data := capturepages.MealVoiceData{
		MealID: mealID.String(),
		Text:   strings.TrimSpace(r.PostFormValue("text")),
	}
	draft, err := h.svc.ParseFoods(r.Context(), user, data.Text)
	if err != nil {
		data.Error = message(err)
		h.renderMealVoice(w, r, statusFor(err), data)
		return
	}

	data.Draft, data.Parsed = draft, true
	h.renderMealVoice(w, r, http.StatusOK, data)
}

func (h *Handler) renderMealVoice(w http.ResponseWriter, r *http.Request, status int, data capturepages.MealVoiceData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := capturepages.MealVoice(data).Render(r.Context(), w); err != nil {
		middleware.FromContext(r.Context()).Error("render meal voice", slog.Any("error", err))
	}
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
		return Sentence(err)
	}
	return "Something went wrong reading that. Try again."
}

// Service is the handler's service, so the JSON twin can be built from the
// same one rather than a second copy wired to the same tables.
func (h *Handler) Service() *Service { return h.svc }
