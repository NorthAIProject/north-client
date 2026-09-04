package capture

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/quota"
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

// Routes mounts the API. It carries its own authentication because it is not
// behind the browser session middleware: the caller is a script or an agent
// holding a personal access token, not a cookie.
func (a *API) Routes(r chi.Router) {
	r.Route("/v1/capture", func(r chi.Router) {
		r.Use(a.authenticate)
		r.Post("/parse", a.parse)
		r.Post("/commit", a.commit)
	})
}

func (a *API) parse(w http.ResponseWriter, r *http.Request) {
	user := userFrom(r.Context())

	var req ParseRequest
	if err := httpx.ReadJSON(w, r, &req, httpx.ReadOptions{MaxBytes: maxAPIBytes}); err != nil {
		httpx.Error(w, err, "The request body must be JSON with a text field.")
		return
	}

	// Consume rather than the Guard middleware: Guard renders an HTML refusal
	// page, which is the wrong answer on a JSON route.
	decision, err := a.quotas.Consume(r.Context(), user.ID, string(user.Tier), quota.QuickCapture)
	if err != nil {
		a.log.Error("capture quota", slog.Any("error", err))
		httpx.Error(w, err, "Something went wrong.")
		return
	}
	if !decision.Allowed {
		// The same answer the page gives, header and all: a caller should not
		// have to learn two refusals for one limit.
		if seconds := int(decision.RetryAfter.Seconds()); seconds > 0 {
			w.Header().Set("Retry-After", strconv.Itoa(seconds))
		}
		httpx.Error(w, httpx.ErrRateLimited, "You have made too many captures for now. Try again shortly.")
		return
	}

	draft, err := a.svc.Parse(r.Context(), user, req.Text)
	if err != nil {
		a.fail(w, err, "That could not be read.")
		return
	}

	httpx.WriteJSON(w, http.StatusOK, ParseResponse{
		Items:    nonNil(draft.Items),
		Unparsed: nonNilStrings(draft.Unparsed),
	})
}

func (a *API) commit(w http.ResponseWriter, r *http.Request) {
	user := userFrom(r.Context())

	var req CommitRequest
	if err := httpx.ReadJSON(w, r, &req, httpx.ReadOptions{MaxBytes: maxAPIBytes}); err != nil {
		httpx.Error(w, err, "The request body must be JSON with an items array.")
		return
	}

	receipt, err := a.svc.Commit(r.Context(), user, req.Items)
	if err != nil {
		a.fail(w, err, "Those entries could not be saved.")
		return
	}

	httpx.WriteJSON(w, http.StatusOK, CommitResponse{
		Written:  receipt.Written(),
		Failed:   receipt.Failed(),
		Outcomes: receipt.Outcomes,
		Skipped:  receipt.Skipped,
	})
}

// fail answers a service error, logging the ones that are North's fault.
func (a *API) fail(w http.ResponseWriter, err error, message string) {
	if httpx.Status(err) >= http.StatusInternalServerError {
		a.log.Error("capture api", slog.Any("error", err))
		httpx.Error(w, err, message)
		return
	}
	// A validation failure carries a sentence the caller can act on.
	httpx.Error(w, err, Sentence(err))
}

type apiUserKey struct{}

func (a *API) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearerToken(r)
		if !ok {
			a.unauthorized(w, r)
			return
		}

		user, err := a.auth.Authenticate(r.Context(), token)
		if err != nil {
			if apperr.Is(err, apperr.ErrUnauthenticated) || apperr.Is(err, apperr.ErrNotFound) {
				a.unauthorized(w, r)
				return
			}
			// A lookup that failed for another reason is North's problem, and
			// naming it would describe the deployment to someone who has not
			// authenticated.
			a.log.Error("capture api cannot resolve its caller", slog.Any("error", err))
			httpx.Error(w, apperr.ErrUnavailable, "Something went wrong.")
			return
		}

		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), apiUserKey{}, user)))
	})
}

func (a *API) unauthorized(w http.ResponseWriter, r *http.Request) {
	a.log.Warn("capture api rejected", slog.String("remote", r.RemoteAddr))
	w.Header().Set("WWW-Authenticate", `Bearer realm="north-capture"`)
	httpx.Error(w, apperr.ErrUnauthenticated, "That token is not valid.")
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
