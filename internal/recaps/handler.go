package recaps

import (
	"log/slog"
	"net/http"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/recaps/recap"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	recappages "github.com/NorthAIProject/north-client/web/recaps"
)

// Handler serves the recap page and the My Day strip.
type Handler struct {
	svc     *Service
	siteURL string
}

// NewHandler builds the handler. siteURL is what a shared recap links to;
// empty shares the text alone.
func NewHandler(svc *Service, siteURL string) *Handler {
	return &Handler{svc: svc, siteURL: siteURL}
}

// Routes mount under /app.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/recap", h.page)
	r.Get("/recap/panels", h.panels)
	r.Get("/recap/strip", h.strip)
	r.Post("/recap/share", h.share)
}

func (h *Handler) page(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	v, ok := h.view(w, r, recappages.Notice(r.URL.Query().Get("posted")))
	if !ok {
		return
	}
	h.render(w, r, recappages.Page(user, v))
}

func (h *Handler) panels(w http.ResponseWriter, r *http.Request) {
	if v, ok := h.view(w, r, recappages.NoticeNone); ok {
		h.render(w, r, recappages.Panels(v))
	}
}

func (h *Handler) strip(w http.ResponseWriter, r *http.Request) {
	rc, err := h.svc.Load(r.Context(), auth.MustUser(r.Context()), recap.KeyWeek)
	if err != nil {
		// The strip is a garnish on My Day: without it the page is whole,
		// so it disappears rather than showing an error in the middle.
		middleware.FromContext(r.Context()).Error("load recap strip", slog.Any("error", err))
		w.WriteHeader(http.StatusOK)
		return
	}
	h.render(w, r, recappages.StripView(recappages.BuildStrip(rc)))
}

func (h *Handler) share(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, i18n.T(r.Context(), "recap.error"), http.StatusBadRequest)
		return
	}
	key := r.PostFormValue("range")
	if !ValidKey(key) {
		key = recap.KeyWeek
	}
	shared, err := h.svc.Share(r.Context(), auth.MustUser(r.Context()), key, r.PostFormValue("share_training") == "1")
	if err != nil {
		h.fail(w, r, err)
		return
	}
	notice := recappages.NoticePosted
	if !shared.TrainingShared {
		notice = recappages.NoticeHidden
	}
	http.Redirect(w, r, "/app/recap?range="+key+"&posted="+string(notice), http.StatusSeeOther)
}

// view loads the recap the query names. An unknown range is the week rather
// than an error: a hand-edited address must not take the page down.
func (h *Handler) view(w http.ResponseWriter, r *http.Request, notice recappages.Notice) (recappages.View, bool) {
	key := r.URL.Query().Get("range")
	if !ValidKey(key) {
		key = recap.KeyWeek
	}
	if notice != recappages.NoticePosted && notice != recappages.NoticeHidden {
		notice = recappages.NoticeNone
	}
	rc, err := h.svc.Load(r.Context(), auth.MustUser(r.Context()), key)
	if err != nil {
		h.fail(w, r, err)
		return recappages.View{}, false
	}
	return recappages.BuildView(r.Context(), rc, h.siteURL, notice), true
}

func (h *Handler) render(w http.ResponseWriter, r *http.Request, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := c.Render(r.Context(), w); err != nil {
		middleware.FromContext(r.Context()).Error("render recap", slog.Any("error", err))
	}
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	middleware.FromContext(r.Context()).Error("recap failed", slog.Any("error", err))
	http.Error(w, i18n.T(r.Context(), "recap.error"), http.StatusInternalServerError)
}
