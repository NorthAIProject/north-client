package crews

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	crewpages "github.com/NorthAIProject/north-client/web/crews"
)

// JoinCookie carries a crew code from /i/c/<code> through signing up or in;
// JoinFromCookie joins on the first page inside /app.
const JoinCookie = "north_crew"

type Handler struct {
	svc     *Service
	siteURL string
	secure  bool
}

func NewHandler(svc *Service, siteURL string, secure bool) *Handler {
	return &Handler{svc: svc, siteURL: strings.TrimRight(siteURL, "/"), secure: secure}
}

// PublicRoutes mount outside /app: the join link.
func (h *Handler) PublicRoutes(r chi.Router) {
	r.Get("/i/c/{code}", h.joinLink)
}

// Routes mount under /app.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/crews", h.index)
	r.Post("/crews", h.create)
	r.Get("/crews/{crewID}", h.board)
	r.Post("/crews/{crewID}/leave", h.leave)
	r.Post("/crews/{crewID}/members/{userID}/remove", h.remove)
	r.Post("/crews/{crewID}/challenge", h.challenge)
}

func (h *Handler) joinLink(w http.ResponseWriter, r *http.Request) {
	c, err := h.svc.Preview(r.Context(), chi.URLParam(r, "code"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if user, ok := auth.UserFrom(r.Context()); ok {
		joined, err := h.svc.Join(r.Context(), user.ID, c.Code)
		if err != nil {
			h.fail(w, r, err)
			return
		}
		http.Redirect(w, r, "/app/crews/"+joined.ID.String(), http.StatusSeeOther)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: JoinCookie, Value: c.Code, Path: "/", MaxAge: int((30 * 24 * time.Hour).Seconds()),
		HttpOnly: true, Secure: h.secure, SameSite: http.SameSiteLaxMode,
	})
	h.render(w, r, http.StatusOK, crewpages.JoinPage(c))
}

// JoinFromCookie is middleware for /app: it joins the crew a signed-out
// visitor opened a link to, once they are in, then forgets the code.
func (h *Handler) JoinFromCookie(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(JoinCookie)
		user, ok := auth.UserFrom(r.Context())
		if err == nil && ok && cookie.Value != "" {
			if _, err := h.svc.Join(r.Context(), user.ID, cookie.Value); err != nil {
				middleware.FromContext(r.Context()).Warn("could not join crew from link", slog.Any("error", err))
			}
			http.SetCookie(w, &http.Cookie{Name: JoinCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: h.secure, SameSite: http.SameSiteLaxMode})
		}
		next.ServeHTTP(w, r)
	})
}

func (h *Handler) index(w http.ResponseWriter, r *http.Request) {
	h.list(w, r, http.StatusOK, crewpages.CreateForm{})
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request, status int, form crewpages.CreateForm) {
	user := auth.MustUser(r.Context())
	mine, err := h.svc.Mine(r.Context(), user.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.render(w, r, status, crewpages.IndexPage(user, mine, form))
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}
	name := r.PostFormValue("name")
	c, err := h.svc.Create(r.Context(), auth.MustUser(r.Context()).ID, name)
	if err != nil {
		var fieldErrs apperr.FieldErrors
		if apperr.As(err, &fieldErrs) {
			h.list(w, r, http.StatusUnprocessableEntity, crewpages.CreateForm{Name: name, Errors: fieldErrs.Messages()})
			return
		}
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/app/crews/"+c.ID.String(), http.StatusSeeOther)
}

func (h *Handler) board(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "crewID"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}
	user := auth.MustUser(r.Context())
	b, err := h.svc.Board(r.Context(), id, user.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.render(w, r, http.StatusOK, crewpages.BoardPage(user, b, JoinURL(h.siteURL, b.Code)))
}

func (h *Handler) leave(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "crewID"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}
	if err := h.svc.Leave(r.Context(), id, auth.MustUser(r.Context()).ID); err != nil {
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/app/crews", http.StatusSeeOther)
}

func (h *Handler) remove(w http.ResponseWriter, r *http.Request) {
	crewID, err1 := uuid.Parse(chi.URLParam(r, "crewID"))
	memberID, err2 := uuid.Parse(chi.URLParam(r, "userID"))
	if err1 != nil || err2 != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}
	if err := h.svc.Remove(r.Context(), crewID, auth.MustUser(r.Context()).ID, memberID); err != nil {
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/app/crews/"+crewID.String(), http.StatusSeeOther)
}

// challenge sets the weekly challenge, or clears it when kind is "none".
func (h *Handler) challenge(w http.ResponseWriter, r *http.Request) {
	crewID, err := uuid.Parse(chi.URLParam(r, "crewID"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}
	if err = r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}
	var ch *Challenge
	if kind := r.PostFormValue("kind"); kind != "none" {
		target, _ := strconv.Atoi(r.PostFormValue("target"))
		ch = &Challenge{Kind: kind, Target: target}
	}
	if err = h.svc.SetChallenge(r.Context(), crewID, auth.MustUser(r.Context()).ID, ch); err != nil {
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/app/crews/"+crewID.String(), http.StatusSeeOther)
}

func (h *Handler) render(w http.ResponseWriter, r *http.Request, status int, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := c.Render(r.Context(), w); err != nil {
		middleware.FromContext(r.Context()).Error("render failed", slog.Any("error", err))
	}
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case apperr.Is(err, apperr.ErrNotFound):
		http.Error(w, "Not found.", http.StatusNotFound)
	case apperr.Is(err, apperr.ErrValidation):
		var fieldErrs apperr.FieldErrors
		if apperr.As(err, &fieldErrs) {
			for _, msg := range fieldErrs.Messages() {
				http.Error(w, msg, http.StatusUnprocessableEntity)
				return
			}
		}
		http.Error(w, "That request could not be read.", http.StatusUnprocessableEntity)
	default:
		middleware.FromContext(r.Context()).Error("crews request failed", slog.Any("error", err))
		http.Error(w, "Something went wrong.", http.StatusInternalServerError)
	}
}
