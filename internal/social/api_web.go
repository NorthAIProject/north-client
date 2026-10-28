package social

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
	"github.com/NorthAIProject/north-client/internal/social/phone"
)

// The routes a web front end needs to serve the Friends page without this
// binary rendering it: the phone card's country select, and Facebook's web
// redirect flow split into its two halves. Each reuses what the Go page
// handler calls, so both front ends apply the same rules. None of them sets a
// cookie; the front end keeps the OAuth state in its own.

// CountryView is one calling code the phone card offers beside a national number.
type CountryView struct {
	// Dial is the calling code without the plus: "351".
	Dial string `json:"dial"`
	// Name is the English name; a front end shows social.country.<dial>
	// from its catalogue when it has one.
	Name string `json:"name"`
}

// PhoneCountries is the select and the code preselected for this account.
type PhoneCountries struct {
	// DefaultDial is read from the account's timezone and language; empty
	// when neither says.
	DefaultDial string        `json:"defaultDial"`
	Countries   []CountryView `json:"countries"`
}

// FacebookWebStart is where to send the browser, and the state the front end
// keeps (in its own short-lived cookie) and compares on the callback.
type FacebookWebStart struct {
	URL   string `json:"url"`
	State string `json:"state"`
	// ExpiresIn is how long the state is good for, in seconds.
	ExpiresIn int `json:"expiresIn"`
}

// FacebookWebCode is the code Facebook returned to the web callback, after
// the front end has checked the state.
type FacebookWebCode struct {
	Code string `json:"code"`
}

func (a *API) webRoutes(r chi.Router) {
	r.Get("/social/phone/countries", a.phoneCountries)
	r.Post("/social/facebook/web", a.facebookWebStart)
	r.Post("/social/facebook/web/callback", a.facebookWebFinish)
}

func (a *API) phoneCountries(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	httpx.WriteJSON(w, http.StatusOK, projectCountries(phone.DefaultDial(user.Timezone, string(user.Locale))))
}

func projectCountries(dial string) PhoneCountries {
	out := PhoneCountries{DefaultDial: dial, Countries: make([]CountryView, 0, len(phone.Countries))}
	for _, c := range phone.Countries {
		out.Countries = append(out.Countries, CountryView{Dial: c.Dial, Name: c.Name})
	}
	return out
}

// facebookWebStart begins the flow the Friends page's Connect button starts.
// The callback is this site's /app/friends/facebook/callback, the URI
// registered in the Meta app, whichever front end serves it.
func (a *API) facebookWebStart(w http.ResponseWriter, r *http.Request) {
	state, err := NewOAuthState()
	if err != nil {
		httpx.Error(w, err, "Facebook is not available right now.")
		return
	}
	consent, err := a.svc.FacebookAuthURL(state, a.siteURL+FacebookWebCallbackPath)
	if err != nil {
		httpx.Error(w, err, "Facebook is not available right now.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, FacebookWebStart{URL: consent, State: state, ExpiresIn: int(facebookStateTTL.Seconds())})
}

// facebookWebFinish connects the account behind the code and imports who is
// here. 409 when that Facebook account is connected to another account.
func (a *API) facebookWebFinish(w http.ResponseWriter, r *http.Request) {
	var req FacebookWebCode
	if !readJSON(w, r, &req) {
		return
	}
	f, err := a.svc.ConnectFacebook(r.Context(), auth.MustUser(r.Context()).ID, req.Code, a.siteURL+FacebookWebCallbackPath)
	if err != nil {
		if errors.Is(err, ErrFacebookTaken) {
			httpx.Error(w, err, "That Facebook account is connected to another Khepri account.")
			return
		}
		httpx.Error(w, err, "Facebook could not be connected.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, projectFacebook(f, a.svc.FacebookEnabled()))
}
