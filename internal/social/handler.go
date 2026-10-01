package social

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/achievements/achievement"
	"github.com/NorthAIProject/north-client/internal/auth"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	socialpages "github.com/NorthAIProject/north-client/web/social"
)

// InviteCookie carries an invite code from /i/<code> through whichever way
// the visitor then signs up or signs in: password, Google, Apple, passkey.
// RedeemInvite reads it on their first page inside the app.
const InviteCookie = "north_invite"

// inviteCookieAge is long enough for somebody who taps a link today and signs
// up at the weekend.
const inviteCookieAge = 30 * 24 * time.Hour

type Handler struct {
	svc          *Service
	siteURL      string
	secure       bool
	achievements Achievements
}

// Achievements is the feed and kudos the Friends page shows.
// achievements.Service satisfies it; nil leaves both off the page.
type Achievements interface {
	Feed(ctx context.Context, viewerID uuid.UUID, before time.Time) ([]achievement.Item, error)
	Sharing(ctx context.Context, userID uuid.UUID) (achievement.Sharing, error)
	SetSharing(ctx context.Context, userID uuid.UUID, in achievement.Sharing) (achievement.Sharing, error)
	GiveKudos(ctx context.Context, giverID, achievementID uuid.UUID, giverName string) error
	TakeKudos(ctx context.Context, giverID, achievementID uuid.UUID) error
}

func (h *Handler) WithAchievements(a Achievements) *Handler {
	h.achievements = a
	return h
}

// NewHandler builds the web routes. secure marks the invite cookie Secure,
// which production needs and plain-http local development cannot have.
func NewHandler(svc *Service, siteURL string, secure bool) *Handler {
	return &Handler{svc: svc, siteURL: strings.TrimRight(siteURL, "/"), secure: secure}
}

// PublicRoutes mount outside /app: the invite landing page.
func (h *Handler) PublicRoutes(r chi.Router) {
	r.Get("/i/{code}", h.invite)
}

// Routes mount under /app.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/friends", h.index)
	r.Post("/friends/handle", h.setHandle)
	r.Post("/friends/follow", h.follow)
	r.Post("/friends/{userID}/accept", h.onPerson(h.svc.Accept))
	r.Post("/friends/{userID}/remove", h.onPerson(h.svc.RemoveFollower))
	r.Post("/friends/{userID}/unfollow", h.onPerson(h.svc.Unfollow))
	r.Post("/friends/{userID}/block", h.onPerson(h.svc.Block))
	r.Post("/friends/{userID}/unblock", h.onPerson(h.svc.Unblock))
	r.Post("/friends/sharing", h.setSharing)
	r.Post("/friends/kudos/{achievementID}", h.kudos(true))
	r.Post("/friends/kudos/{achievementID}/undo", h.kudos(false))
}

func (h *Handler) setSharing(w http.ResponseWriter, r *http.Request) {
	if h.achievements == nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}
	if err := r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}
	in := achievement.Sharing{
		Training: r.PostFormValue("share_training") != "",
		Streaks:  r.PostFormValue("share_streaks") != "",
		Goals:    r.PostFormValue("share_goals") != "",
	}
	if _, err := h.achievements.SetSharing(r.Context(), auth.MustUser(r.Context()).ID, in); err != nil {
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/app/friends", http.StatusSeeOther)
}

func (h *Handler) kudos(give bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.achievements == nil {
			h.fail(w, r, apperr.ErrNotFound)
			return
		}
		id, err := uuid.Parse(chi.URLParam(r, "achievementID"))
		if err != nil {
			h.fail(w, r, apperr.ErrNotFound)
			return
		}
		user := auth.MustUser(r.Context())
		if give {
			err = h.achievements.GiveKudos(r.Context(), user.ID, id, user.DisplayName)
		} else {
			err = h.achievements.TakeKudos(r.Context(), user.ID, id)
		}
		if err != nil {
			h.fail(w, r, err)
			return
		}
		http.Redirect(w, r, "/app/friends#feed", http.StatusSeeOther)
	}
}

// invite shows who sent the link. A signed-in visitor is connected at once; a
// signed-out one carries the code in a cookie to wherever they sign up.
func (h *Handler) invite(w http.ResponseWriter, r *http.Request) {
	preview, err := h.svc.PreviewInvite(r.Context(), chi.URLParam(r, "code"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if user, ok := auth.UserFrom(r.Context()); ok {
		if _, err := h.svc.Redeem(r.Context(), user.ID, preview.Code); err != nil {
			h.fail(w, r, err)
			return
		}
		http.Redirect(w, r, "/app/friends", http.StatusSeeOther)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: InviteCookie, Value: preview.Code, Path: "/",
		MaxAge: int(inviteCookieAge.Seconds()), HttpOnly: true, Secure: h.secure, SameSite: http.SameSiteLaxMode,
	})
	h.render(w, r, http.StatusOK, socialpages.InvitePage(preview))
}

// RedeemInvite is middleware for /app: it connects a signed-in account to
// whoever's link brought them, then forgets the code. Failures are logged and
// never stop the page; an invite is a nicety, the app is the point.
func (h *Handler) RedeemInvite(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(InviteCookie)
		user, ok := auth.UserFrom(r.Context())
		if err == nil && ok && cookie.Value != "" {
			if _, err := h.svc.Redeem(r.Context(), user.ID, cookie.Value); err != nil {
				middleware.FromContext(r.Context()).Warn("could not redeem invite", slog.Any("error", err))
			}
			http.SetCookie(w, &http.Cookie{Name: InviteCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: h.secure, SameSite: http.SameSiteLaxMode})
		}
		next.ServeHTTP(w, r)
	})
}

func (h *Handler) index(w http.ResponseWriter, r *http.Request) {
	h.page(w, r, http.StatusOK, socialpages.FriendsForm{})
}

func (h *Handler) page(w http.ResponseWriter, r *http.Request, status int, form socialpages.FriendsForm) {
	user := auth.MustUser(r.Context())
	overview, err := h.svc.Overview(r.Context(), user.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if form.Handle == "" && form.Errors["handle"] == "" {
		form.Handle = overview.Handle
	}
	var feed socialpages.FeedSection
	if h.achievements != nil {
		feed.Enabled = true
		if feed.Items, err = h.achievements.Feed(r.Context(), user.ID, time.Time{}); err != nil {
			h.fail(w, r, err)
			return
		}
		if feed.Sharing, err = h.achievements.Sharing(r.Context(), user.ID); err != nil {
			h.fail(w, r, err)
			return
		}
	}
	share, err := h.shareLinks(r, user.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.render(w, r, status, socialpages.FriendsPage(user, overview, InviteURL(h.siteURL, overview.Invite.Code), form, feed, share))
}

func (h *Handler) setHandle(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}
	raw := r.PostFormValue("handle")
	if _, err := h.svc.SetHandle(r.Context(), auth.MustUser(r.Context()).ID, raw); err != nil {
		h.formError(w, r, err, socialpages.FriendsForm{Handle: raw})
		return
	}
	http.Redirect(w, r, "/app/friends", http.StatusSeeOther)
}

func (h *Handler) follow(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}
	raw := r.PostFormValue("follow")
	if _, err := h.svc.Follow(r.Context(), auth.MustUser(r.Context()).ID, raw); err != nil {
		if apperr.Is(err, apperr.ErrNotFound) {
			err = apperr.FieldErrors{}.Add("follow", "Nobody has that handle.")
		}
		h.formError(w, r, err, socialpages.FriendsForm{Follow: raw})
		return
	}
	http.Redirect(w, r, "/app/friends", http.StatusSeeOther)
}

// formError re-renders the page with the fields' messages, or fails.
func (h *Handler) formError(w http.ResponseWriter, r *http.Request, err error, form socialpages.FriendsForm) {
	var fieldErrs apperr.FieldErrors
	if !apperr.As(err, &fieldErrs) {
		h.fail(w, r, err)
		return
	}
	form.Errors = fieldErrs.Messages()
	h.page(w, r, http.StatusUnprocessableEntity, form)
}

func (h *Handler) onPerson(act func(ctx context.Context, me, them uuid.UUID) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		them, err := uuid.Parse(chi.URLParam(r, "userID"))
		if err != nil {
			h.fail(w, r, apperr.ErrNotFound)
			return
		}
		if err := act(r.Context(), auth.MustUser(r.Context()).ID, them); err != nil {
			h.fail(w, r, err)
			return
		}
		http.Redirect(w, r, "/app/friends", http.StatusSeeOther)
	}
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
		http.Error(w, "That request could not be read.", http.StatusUnprocessableEntity)
	default:
		middleware.FromContext(r.Context()).Error("friends request failed", slog.Any("error", err))
		http.Error(w, "Something went wrong.", http.StatusInternalServerError)
	}
}

// inviteText is what a shared invite says before its link.
const inviteText = "I'm using Khepri to keep my goals, training and check-ins going. Join me:"

// shareLinks are the X and Facebook share pages, each with its own invite
// code so the strangers-count can tell which posting brought somebody in.
// Plain share intents: the person posts it themselves, nothing is sent for
// them, and no platform API or key is involved.
func (h *Handler) shareLinks(r *http.Request, userID uuid.UUID) (socialpages.ShareLinks, error) {
	x, err := h.svc.InviteFor(r.Context(), userID, ChannelX)
	if err != nil {
		return socialpages.ShareLinks{}, err
	}
	fb, err := h.svc.InviteFor(r.Context(), userID, ChannelFacebook)
	if err != nil {
		return socialpages.ShareLinks{}, err
	}
	return socialpages.ShareLinks{
		X: "https://x.com/intent/post?" + url.Values{
			"text": {inviteText}, "url": {InviteURL(h.siteURL, x.Code)},
		}.Encode(),
		Facebook: "https://www.facebook.com/sharer/sharer.php?" + url.Values{
			"u": {InviteURL(h.siteURL, fb.Code)},
		}.Encode(),
	}, nil
}
