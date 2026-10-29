package capture

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/quota"
	"github.com/NorthAIProject/north-client/internal/shared/echox"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
	"github.com/NorthAIProject/north-client/internal/users"
)

// maxAPIBytes bounds a capture body. A capture is a paragraph; anything larger
// is a document, and internal/documents is where those go.
const maxAPIBytes = 64 << 10

// Authenticator resolves a presented bearer token to the account it acts as.
//
// Declared here rather than imported for the reason internal/health declares
// its own: this package needs one method, and depending on the whole
// connections service to get it would tie the JSON edge to how tokens happen to
// be stored today. *connections.Service satisfies it.
type Authenticator interface {
	Authenticate(ctx context.Context, token string) (users.User, error)
}

// API is the JSON twin of the capture page.
//
// It lives beside the HTML handler and shares the same service, which is the
// arrangement docs/IOS.md asks for: a handler that lives away from its service
// drifts from it.
//
// The two calls mirror the page exactly, and the split is the point. There is
// deliberately no one-shot "text in, rows out" endpoint: a caller that skips
// the preview is a caller writing whatever the model guessed, which is the same
// objection that keeps capture out of the agent registry, only over HTTP.
type API struct {
	svc    *Service
	auth   Authenticator
	quotas *quota.Service
	log    *slog.Logger

	// sessions resolves the native app's sign-in session, so the app (and its
	// App Intents) can capture without minting an nk_ connection token.
	sessions auth.SessionResolver
}

// WithSessions lets capture also accept a sign-in session token. Connection
// tokens are recognised by their nk_ prefix, so each request costs one lookup.
func (a *API) WithSessions(sessions auth.SessionResolver) *API {
	a.sessions = sessions
	return a
}

func NewAPI(svc *Service, auth Authenticator, quotas *quota.Service, log *slog.Logger) *API {
	if log == nil {
		log = slog.Default()
	}
	return &API{svc: svc, auth: auth, quotas: quotas, log: log}
}

// ParseRequest is what a caller sends to have a sentence read.
type ParseRequest struct {
	Text string `json:"text"`
}

// ParseResponse is a draft nobody has agreed to yet.
type ParseResponse struct {
	Items    []Item   `json:"items"`
	Unparsed []string `json:"unparsed"`
}

// CommitRequest is the items a caller decided to keep.
type CommitRequest struct {
	Items []Item `json:"items"`
}

// CommitResponse reports what each item did.
type CommitResponse struct {
	Written  int       `json:"written"`
	Failed   int       `json:"failed"`
	Outcomes []Outcome `json:"outcomes"`
	Skipped  []Item    `json:"skipped,omitempty"`
}

// Register registers capture endpoints relative to the API version boundary.
// It carries its own authentication because it is not behind the browser
// session middleware: the caller is a script or an agent holding a personal
// access token, not a cookie.
func (a *API) Register(g *echo.Group) {
	cg := g.Group("/capture", a.authenticate)
	cg.POST("/parse", a.parse)
	cg.POST("/commit", a.commit)
	cg.POST("/foods", a.foods)
}

func (a *API) parse(c *echo.Context) error {
	ctx := c.Request().Context()
	user := userFrom(ctx)

	var req ParseRequest
	if err := echox.BindJSON(c, &req, httpx.ReadOptions{MaxBytes: maxAPIBytes}, "The request body must be JSON with a text field."); err != nil {
		return err
	}

	if err := a.allow(c, user); err != nil {
		return err
	}

	draft, err := a.svc.Parse(ctx, user, req.Text)
	if err != nil {
		return a.fail(err, "That could not be read.")
	}

	return c.JSON(http.StatusOK, ParseResponse{
		Items:    nonNil(draft.Items),
		Unparsed: nonNilStrings(draft.Unparsed),
	})
}

// foods reads a meal into portions, each with the ingredients it could mean.
// It writes nothing: the app adds the portions a person kept through the
// nutrition API, one call per portion.
func (a *API) foods(c *echo.Context) error {
	ctx := c.Request().Context()
	user := userFrom(ctx)

	var req ParseRequest
	if err := echox.BindJSON(c, &req, httpx.ReadOptions{MaxBytes: maxAPIBytes}, "The request body must be JSON with a text field."); err != nil {
		return err
	}
	if err := a.allow(c, user); err != nil {
		return err
	}

	draft, err := a.svc.ParseFoods(ctx, user, req.Text)
	if err != nil {
		return a.fail(err, "That could not be read.")
	}

	draft.Unparsed = nonNilStrings(draft.Unparsed)
	return c.JSON(http.StatusOK, draft)
}

// allow spends one parse from the account's capture quota, returning the
// refusal when there is none left.
//
// Consume rather than the Guard middleware: Guard renders an HTML refusal
// page, which is the wrong answer on a JSON route.
func (a *API) allow(c *echo.Context, user users.User) error {
	decision, err := a.quotas.Consume(c.Request().Context(), user.ID, string(user.Tier), quota.QuickCapture)
	if err != nil {
		a.log.Error("capture quota", slog.Any("error", err))
		return httpx.Fail(err, "Something went wrong.")
	}
	if !decision.Allowed {
		// The same answer the page gives, header and all: a caller should not
		// have to learn two refusals for one limit.
		if seconds := int(decision.RetryAfter.Seconds()); seconds > 0 {
			c.Response().Header().Set("Retry-After", strconv.Itoa(seconds))
		}
		return httpx.Fail(httpx.ErrRateLimited, "You have made too many captures for now. Try again shortly.")
	}
	return nil
}

func (a *API) commit(c *echo.Context) error {
	ctx := c.Request().Context()
	user := userFrom(ctx)

	var req CommitRequest
	if err := echox.BindJSON(c, &req, httpx.ReadOptions{MaxBytes: maxAPIBytes}, "The request body must be JSON with an items array."); err != nil {
		return err
	}

	receipt, err := a.svc.Commit(ctx, user, req.Items)
	if err != nil {
		return a.fail(err, "Those entries could not be saved.")
	}

	return c.JSON(http.StatusOK, CommitResponse{
		Written:  receipt.Written(),
		Failed:   receipt.Failed(),
		Outcomes: receipt.Outcomes,
		Skipped:  receipt.Skipped,
	})
}

// fail turns a service error into the reply's error, logging the ones that
// are North's fault.
func (a *API) fail(err error, message string) error {
	if httpx.Status(err) >= http.StatusInternalServerError {
		a.log.Error("capture api", slog.Any("error", err))
		return httpx.Fail(err, message)
	}
	// A validation failure carries a sentence the caller can act on. Nothing
	// else does: a provider's 402 or 403 surfaces here below 500, and its text
	// names the model and quotes the provider's own response body.
	if apperr.Is(err, apperr.ErrValidation) || apperr.Is(err, apperr.ErrNotFound) {
		return httpx.Fail(err, Sentence(err))
	}
	return httpx.Fail(err, message)
}

type apiUserKey struct{}

func (a *API) authenticate(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		r := c.Request()
		token, ok := bearerToken(r)
		if !ok {
			return a.unauthorized(c)
		}

		user, err := a.caller(r.Context(), token)
		if err != nil {
			if apperr.Is(err, apperr.ErrUnauthenticated) || apperr.Is(err, apperr.ErrNotFound) {
				return a.unauthorized(c)
			}
			// A lookup that failed for another reason is North's problem, and
			// naming it would describe the deployment to someone who has not
			// authenticated.
			a.log.Error("capture api cannot resolve its caller", slog.Any("error", err))
			return httpx.Fail(apperr.ErrUnavailable, "Something went wrong.")
		}

		c.SetRequest(r.WithContext(context.WithValue(r.Context(), apiUserKey{}, user)))
		return next(c)
	}
}

// caller resolves a token to its person: an nk_ connection token for agents
// and shortcuts, otherwise the app's sign-in session when sessions are wired.
func (a *API) caller(ctx context.Context, token string) (users.User, error) {
	if strings.HasPrefix(token, connectionTokenPrefix) || a.sessions == nil {
		return a.auth.Authenticate(ctx, token)
	}
	session, err := a.sessions.Resolve(ctx, token)
	if err != nil {
		return users.User{}, err
	}
	return session.User, nil
}

// connectionTokenPrefix marks the tokens Settings → Connections issues.
const connectionTokenPrefix = "nk_"

func (a *API) unauthorized(c *echo.Context) error {
	a.log.Warn("capture api rejected", slog.String("remote", c.Request().RemoteAddr))
	c.Response().Header().Set("WWW-Authenticate", `Bearer realm="north-capture"`)
	return httpx.Fail(apperr.ErrUnauthenticated, "That token is not valid.")
}

func userFrom(ctx context.Context) users.User {
	user, _ := ctx.Value(apiUserKey{}).(users.User)
	return user
}

func bearerToken(r *http.Request) (string, bool) {
	header := r.Header.Get("Authorization")
	if header == "" {
		return "", false
	}
	token, found := strings.CutPrefix(header, "Bearer ")
	if !found {
		return "", false
	}
	token = strings.TrimSpace(token)
	return token, token != ""
}

// nonNil keeps an empty list rendering as [] rather than null, so a client can
// iterate without a nil check.
func nonNil(items []Item) []Item {
	if items == nil {
		return []Item{}
	}
	return items
}

func nonNilStrings(list []string) []string {
	if list == nil {
		return []string{}
	}
	return list
}
