package social

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
	"github.com/NorthAIProject/north-client/internal/social/phone"
)

// The routes a web front end needs to serve the Friends page without this
// binary rendering it: the phone card's country select, and Facebook's web
// redirect flow split into its two halves. Each reuses what the Go page
// handler calls, so both front ends apply the same rules. None of them sets a
// cookie; the front end keeps the OAuth state in its own.

// FacebookWebCallbackPath is where Facebook returns a connection begun on
// the Friends page. It must be registered in the Meta app as a Valid OAuth
// Redirect URI, next to FacebookNativeCallbackPath.
const FacebookWebCallbackPath = "/app/friends/facebook/callback"

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

func (a *API) registerWeb(g *echo.Group) {
	g.GET("/social/phone/countries", a.phoneCountries)
	g.POST("/social/facebook/web", a.facebookWebStart)
	g.POST("/social/facebook/web/callback", a.facebookWebFinish)
}

func (a *API) phoneCountries(c *echo.Context) error {
	user := auth.MustUser(c.Request().Context())
	return c.JSON(http.StatusOK, projectCountries(phone.DefaultDial(user.Timezone, string(user.Locale))))
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
func (a *API) facebookWebStart(c *echo.Context) error {
	state, err := NewOAuthState()
	if err != nil {
		return httpx.Fail(err, "Facebook is not available right now.")
	}
	consent, err := a.svc.FacebookAuthURL(state, a.siteURL+FacebookWebCallbackPath)
	if err != nil {
		return httpx.Fail(err, "Facebook is not available right now.")
	}
	return c.JSON(http.StatusOK, FacebookWebStart{URL: consent, State: state, ExpiresIn: int(facebookStateTTL.Seconds())})
}

// facebookWebFinish connects the account behind the code and imports who is
// here. 409 when that Facebook account is connected to another account.
func (a *API) facebookWebFinish(c *echo.Context) error {
	ctx := c.Request().Context()
	var req FacebookWebCode
	if err := readJSON(c, &req); err != nil {
		return err
	}
	f, err := a.svc.ConnectFacebook(ctx, auth.MustUser(ctx).ID, req.Code, a.siteURL+FacebookWebCallbackPath)
	if err != nil {
		if errors.Is(err, ErrFacebookTaken) {
			return httpx.Fail(err, "That Facebook account is connected to another Khepri account.")
		}
		return httpx.Fail(err, "Facebook could not be connected.")
	}
	return c.JSON(http.StatusOK, projectFacebook(f, a.svc.FacebookEnabled()))
}
