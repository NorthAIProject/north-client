package social

import (
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/FACorreiaa/go-utils/pkg/util"

	"github.com/NorthAIProject/north-client/internal/auth"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
)

// FacebookNativeCallbackPath is where Facebook returns a connection begun in
// the iOS app. Public: the app's sign-in sheet carries no session, so the
// state alone says whose connection it is. It must be registered in the Meta
// app as a Valid OAuth Redirect URI.
const FacebookNativeCallbackPath = "/api/v1/social/facebook/callback"

// FacebookReturnURL is where that callback sends the sign-in sheet. The sheet
// closes on the khepri:// scheme and the app reads the result from the query.
const FacebookReturnURL = "khepri://friends/facebook"

type PhoneView struct {
	// Configured is false when this deployment cannot text codes; the app
	// then offers no way to add a number.
	Configured bool `json:"configured"`
	// Number is the verified number, E.164, or empty.
	Number     string     `json:"number"`
	VerifiedAt *time.Time `json:"verifiedAt,omitempty"`
	// Pending is the number a code was just texted to, empty when no code is
	// waiting.
	Pending string `json:"pending"`
}

type PhoneStartRequest struct {
	// Phone is as typed: "+351 912 345 678", "00351…", or a national number.
	Phone string `json:"phone"`
	// CountryCode is the calling code, digits only ("351"), that a national
	// number is read in. Not needed when Phone starts with + or 00.
	CountryCode string `json:"countryCode"`
}

type PhoneCheckRequest struct {
	Code string `json:"code"`
}

type FacebookView struct {
	// Configured is false when this deployment has no Meta app; the app then
	// offers no way to connect.
	Configured bool `json:"configured"`
	Connected  bool `json:"connected"`
	// People are who the last import found, for an hour after it. Empty
	// before any import and once it expires: connecting again refreshes it.
	People     []MatchedPerson `json:"people"`
	ImportedAt *time.Time      `json:"importedAt,omitempty"`
}

type FacebookConnect struct {
	// AuthorizeURL opens in the app's sign-in sheet; Facebook returns through
	// the server to FacebookReturnURL with result=connected, cancelled,
	// expired, taken or failed.
	AuthorizeURL string `json:"authorizeUrl"`
}

func (a *API) phone(w http.ResponseWriter, r *http.Request) {
	p, err := a.svc.Phone(r.Context(), auth.MustUser(r.Context()).ID)
	if err != nil {
		httpx.Error(w, err, "Your phone number could not be loaded.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, projectPhone(p, a.svc.PhoneEnabled()))
}

func (a *API) startPhone(w http.ResponseWriter, r *http.Request) {
	var req PhoneStartRequest
	if !readJSON(w, r, &req) {
		return
	}
	p, err := a.svc.StartPhoneVerification(r.Context(), auth.MustUser(r.Context()).ID, req.Phone, req.CountryCode)
	if err != nil {
		phoneError(w, err, "A code could not be sent.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, projectPhone(p, a.svc.PhoneEnabled()))
}

func (a *API) checkPhone(w http.ResponseWriter, r *http.Request) {
	var req PhoneCheckRequest
	if !readJSON(w, r, &req) {
		return
	}
	p, err := a.svc.CheckPhoneCode(r.Context(), auth.MustUser(r.Context()).ID, req.Code)
	if err != nil {
		phoneError(w, err, "That code could not be checked.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, projectPhone(p, a.svc.PhoneEnabled()))
}

func (a *API) cancelPhone(w http.ResponseWriter, r *http.Request) {
	if err := a.svc.CancelPhoneVerification(r.Context(), auth.MustUser(r.Context()).ID); err != nil {
		httpx.Error(w, err, "That code could not be cancelled.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) removePhone(w http.ResponseWriter, r *http.Request) {
	if err := a.svc.RemovePhone(r.Context(), auth.MustUser(r.Context()).ID); err != nil {
		httpx.Error(w, err, "Your phone number could not be removed.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// phoneError answers 429 for a rate limit and otherwise as httpx.Error does.
func phoneError(w http.ResponseWriter, err error, message string) {
	if errors.Is(err, ErrPhoneRateLimited) {
		httpx.Error(w, httpx.ErrRateLimited, "Too many codes for now. Try again later.")
		return
	}
	httpx.Error(w, err, message)
}

func projectPhone(p Phone, configured bool) PhoneView {
	out := PhoneView{Configured: configured, Number: p.Number, Pending: p.Pending}
	if !p.VerifiedAt.IsZero() {
		out.VerifiedAt = util.Ptr(p.VerifiedAt)
	}
	return out
}

func (a *API) facebookFriends(w http.ResponseWriter, r *http.Request) {
	f, err := a.svc.FacebookFriends(r.Context(), auth.MustUser(r.Context()).ID)
	if err != nil {
		httpx.Error(w, err, "Facebook friends could not be loaded.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, projectFacebook(f, a.svc.FacebookEnabled()))
}

func (a *API) connectFacebook(w http.ResponseWriter, r *http.Request) {
	consent, err := a.svc.BeginNativeFacebook(r.Context(), auth.MustUser(r.Context()).ID, a.siteURL+FacebookNativeCallbackPath)
	if err != nil {
		httpx.Error(w, err, "Facebook is not available right now.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, FacebookConnect{AuthorizeURL: consent})
}

func (a *API) disconnectFacebook(w http.ResponseWriter, r *http.Request) {
	if err := a.svc.DisconnectFacebook(r.Context(), auth.MustUser(r.Context()).ID); err != nil {
		httpx.Error(w, err, "Facebook could not be disconnected.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// facebookCallback finishes a connection begun by connectFacebook and hands
// the result back to the app. It always redirects to the app, never renders:
// the sign-in sheet shows nothing of its own.
func (a *API) facebookCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	result := "connected"
	if q.Get("error") != "" {
		result = "cancelled"
	} else {
		_, err := a.svc.FinishNativeFacebook(r.Context(), q.Get("state"), q.Get("code"), a.siteURL+FacebookNativeCallbackPath)
		switch {
		case err == nil:
		case errors.Is(err, ErrFacebookTaken):
			result = "taken"
		case apperr.Is(err, apperr.ErrNotFound):
			result = "expired"
		default:
			middleware.FromContext(r.Context()).Error("native facebook connect failed", slog.Any("error", err))
			result = "failed"
		}
	}
	http.Redirect(w, r, FacebookReturnURL+"?"+url.Values{"result": {result}}.Encode(), http.StatusFound)
}

func projectFacebook(f FacebookFriends, configured bool) FacebookView {
	out := FacebookView{Configured: configured, Connected: f.Connected, People: projectMatches(f.People).People}
	if !f.ImportedAt.IsZero() {
		out.ImportedAt = util.Ptr(f.ImportedAt)
	}
	return out
}
