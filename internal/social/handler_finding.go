package social

import (
	"crypto/subtle"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/NorthAIProject/north-client/internal/auth"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	"github.com/NorthAIProject/north-client/internal/social/phone"
	"github.com/NorthAIProject/north-client/internal/users"
	socialpages "github.com/NorthAIProject/north-client/web/social"
)

// FacebookWebCallbackPath is where Facebook returns a connection begun on
// the Friends page. It must be registered in the Meta app as a Valid OAuth
// Redirect URI, next to FacebookNativeCallbackPath.
const FacebookWebCallbackPath = "/app/friends/facebook/callback"

const (
	facebookStateCookie = "north_facebook_oauth"
	facebookCookiePath  = "/app/friends/facebook"
)

// finding is the phone and Facebook cards. A card whose integration is off
// still shows when there is something on it to remove.
func (h *Handler) finding(r *http.Request, user users.User) (socialpages.Finding, error) {
	out := socialpages.Finding{
		Phone: socialpages.PhoneSection{
			Enabled: h.svc.PhoneEnabled(),
			Dial:    phone.DefaultDial(user.Timezone, string(user.Locale)),
		},
		Facebook: socialpages.FacebookSection{
			Enabled: h.svc.FacebookEnabled(),
			Result:  r.URL.Query().Get("facebook"),
		},
	}
	var err error
	if out.Phone.Phone, err = h.svc.Phone(r.Context(), user.ID); err != nil {
		return socialpages.Finding{}, err
	}
	if out.Facebook.Friends, err = h.svc.FacebookFriends(r.Context(), user.ID); err != nil {
		return socialpages.Finding{}, err
	}
	return out, nil
}

func (h *Handler) startPhone(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}
	raw, dial := r.PostFormValue("phone"), r.PostFormValue("country")
	if _, err := h.svc.StartPhoneVerification(r.Context(), auth.MustUser(r.Context()).ID, raw, dial); err != nil {
		h.formError(w, r, phoneFieldError(err, "phone"), socialpages.FriendsForm{Phone: raw, Country: dial})
		return
	}
	http.Redirect(w, r, "/app/friends#phone-card", http.StatusSeeOther)
}

func (h *Handler) checkPhone(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}
	if _, err := h.svc.CheckPhoneCode(r.Context(), auth.MustUser(r.Context()).ID, r.PostFormValue("code")); err != nil {
		h.formError(w, r, phoneFieldError(err, "code"), socialpages.FriendsForm{})
		return
	}
	http.Redirect(w, r, "/app/friends#phone-card", http.StatusSeeOther)
}

func (h *Handler) cancelPhone(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.CancelPhoneVerification(r.Context(), auth.MustUser(r.Context()).ID); err != nil {
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/app/friends#phone-card", http.StatusSeeOther)
}

func (h *Handler) removePhone(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.RemovePhone(r.Context(), auth.MustUser(r.Context()).ID); err != nil {
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/app/friends#phone-card", http.StatusSeeOther)
}

// phoneFieldError shows a rate limit next to the field that hit it, like any
// other reason the form was refused.
func phoneFieldError(err error, field string) error {
	if errors.Is(err, ErrPhoneRateLimited) {
		return apperr.FieldErrors{}.Add(field, "Too many codes for now. Try again in an hour.")
	}
	return err
}

// facebookConnect starts the OAuth flow. The state lives in an HttpOnly
// cookie, so the callback can prove the response belongs to a flow this
// browser started, the same arrangement as Strava and Google.
func (h *Handler) facebookConnect(w http.ResponseWriter, r *http.Request) {
	state, err := NewOAuthState()
	if err != nil {
		h.fail(w, r, err)
		return
	}
	consent, err := h.svc.FacebookAuthURL(state, h.siteURL+FacebookWebCallbackPath)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: facebookStateCookie, Value: state, Path: facebookCookiePath,
		MaxAge: int(facebookStateTTL / time.Second), HttpOnly: true, Secure: h.secure, SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, consent, http.StatusFound)
}

// facebookCallback connects the account and imports, then sends the browser
// back to the Friends page with how it went.
func (h *Handler) facebookCallback(w http.ResponseWriter, r *http.Request) {
	if !h.svc.FacebookEnabled() {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: facebookStateCookie, Value: "", Path: facebookCookiePath,
		MaxAge: -1, HttpOnly: true, Secure: h.secure, SameSite: http.SameSiteLaxMode,
	})
	q := r.URL.Query()
	result := "connected"
	cookie, cookieErr := r.Cookie(facebookStateCookie)
	switch {
	case q.Get("error") != "":
		result = "cancelled"
	case cookieErr != nil || cookie.Value == "":
		result = "expired"
	case subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(q.Get("state"))) != 1:
		result = "expired"
	default:
		_, err := h.svc.ConnectFacebook(r.Context(), auth.MustUser(r.Context()).ID, q.Get("code"), h.siteURL+FacebookWebCallbackPath)
		switch {
		case errors.Is(err, ErrFacebookTaken):
			result = "taken"
		case err != nil:
			middleware.FromContext(r.Context()).Error("facebook connect failed", slog.Any("error", err))
			result = "failed"
		}
	}
	http.Redirect(w, r, "/app/friends?"+url.Values{"facebook": {result}}.Encode()+"#facebook", http.StatusSeeOther)
}

func (h *Handler) facebookDisconnect(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.DisconnectFacebook(r.Context(), auth.MustUser(r.Context()).ID); err != nil {
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/app/friends#facebook", http.StatusSeeOther)
}
