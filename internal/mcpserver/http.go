package mcpserver

import (
	"context"
	"crypto/subtle"
	"log/slog"
	"net"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/ratelimit"
	"github.com/NorthAIProject/north-client/internal/users"
)

// defaultRequestsPerMinute bounds an unmetered LLM-spend path. Generous for a
// human-driven agent, and low enough that a retry loop is noticed rather than
// billed.
const defaultRequestsPerMinute = 120

// Version is what the MCP handshake advertises.
//
// It lives here rather than in a binary because the tool surface is what it
// names, and that surface is now served from two processes. A version that
// differed between cmd/web and cmd/mcp-server would describe the same contract
// two ways. The contract this names is testdata/tools.golden.json.
//
// 0.2.0: every tool declares whether it writes, and results carry structured
// content as well as text.
const Version = "0.2.0"

// Authenticator resolves a presented bearer token to the account it acts as.
//
// This is the whole multi-tenancy seam. A deployment that serves one person
// from an environment variable and one that serves everybody from a table
// differ only in which implementation is passed here, and a future OAuth
// verifier is a third one rather than a rewrite of this file.
type Authenticator interface {
	// Authenticate returns the user the token belongs to, or an error. Every
	// failure must be indistinguishable to the caller: naming which part was
	// wrong tells an unauthenticated client something it has not earned.
	Authenticate(ctx context.Context, token string) (users.User, error)
}

// UserLoader is the slice of users.Service StaticAuthenticator needs.
type UserLoader interface {
	ByID(ctx context.Context, id uuid.UUID) (users.User, error)
}

// StaticAuthenticator is the original single-user behaviour: one token in the
// environment, one account, no per-caller identity.
//
// It is why cmd/mcp-server still works unchanged, and it must keep working —
// the tailnet deployment documented in skills/north-connect/SKILL.md depends on
// it. Anyone holding the token has full read and write access to that account,
// so this belongs behind a private interface and nowhere else.
type StaticAuthenticator struct {
	Token  string
	UserID uuid.UUID
	Users  UserLoader
}

func (a StaticAuthenticator) Authenticate(ctx context.Context, token string) (users.User, error) {
	// Compared in constant time so the response latency does not leak how much
	// of the token was correct.
	if subtle.ConstantTimeCompare([]byte(token), []byte(a.Token)) != 1 {
		return users.User{}, apperr.ErrUnauthenticated
	}
	user, err := a.Users.ByID(ctx, a.UserID)
	if err != nil {
		return users.User{}, apperr.Wrap(err, "load configured mcp user")
	}
	return user, nil
}

// Config is what the MCP server needs to serve.
type Config struct {
	Services Services

	// Auth decides who a request acts as. Required.
	Auth Authenticator

	// AllowedOrigins are the browser origins permitted to reach /mcp.
	//
	// Empty means no browser origin is allowed, which is the right default: a
	// real MCP client sends no Origin at all. See guardOrigin.
	AllowedOrigins []string

	// RequestsPerMinute bounds one account's call rate. Zero uses the default.
	RequestsPerMinute int

	Version string
	Log     *slog.Logger
}

// NewHandler builds the HTTP surface: health checks and the MCP endpoint.
//
// # Who a request acts as
//
// Identity comes from the bearer token via Config.Auth, and the tools are
// registered for that user alone. There is no user parameter anywhere in the
// tool surface, so a token is an authentication credential and never an
// authorisation bypass.
//
// A StaticAuthenticator maps every token to one configured account and must not
// be exposed publicly. The connections service maps each token to its owner and
// is what the public mount uses. Scopes do not exist yet: read and write are
// the same credential, which is a real limitation and the next thing OAuth
// should fix.
func NewHandler(cfg Config) http.Handler {
	mux := http.NewServeMux()

	// Unauthenticated on purpose: a health check that needs a credential is
	// useless to a load balancer, and it reveals nothing.
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready"))
	})

	mux.Handle("/mcp", Endpoint(cfg))

	return mux
}

// Endpoint is the /mcp handler alone, without the health checks.
//
// cmd/web mounts this into its own router, which already answers /healthz;
// a second health check under a different name would leave two answers to the
// same question.
func Endpoint(cfg Config) http.Handler {
	log := cfg.Log
	if log == nil {
		log = slog.Default()
	}

	version := cfg.Version
	if version == "" {
		version = "0.1.0"
	}

	streamable := mcp.NewStreamableHTTPHandler(func(req *http.Request) *mcp.Server {
		s := mcp.NewServer(&mcp.Implementation{Name: "north", Version: version}, nil)

		user, ok := userFrom(req.Context())
		if !ok {
			// Unreachable: authenticate runs first. An empty server exposes no
			// tools, which is the safe way to be wrong.
			return s
		}

		Register(s, cfg.Services, user)
		return s
	}, nil)

	// Order matters, and it is not the obvious one.
	//
	// The real throttle needs an identity, so it has to run after authentication
	// — which means a token-guessing loop would otherwise reach the database on
	// every attempt. throttleAnonymous sits in front to absorb that: generous
	// enough that no real client notices, cheap enough that a flood costs a map
	// lookup rather than a query. The browser check stays first because it is
	// cheaper still.
	return guardOrigin(cfg, log,
		throttleAnonymous(cfg, log,
			authenticate(cfg, log,
				throttle(cfg, log, streamable))))
}

// guardOrigin rejects requests carrying a browser origin North does not know.
//
// The SDK's StreamableHTTPHandler is constructed with no options, so nothing
// else here validates Origin or Host, and DNS-rebinding protection rested
// entirely on the loopback bind. Once this endpoint is mounted on the public
// web app that bind is gone, and this check is the whole defence — a page on
// the open web must not be able to drive somebody's coach with their browser's
// credentials.
//
// A request with no Origin passes: that is every real MCP client. Only a
// browser sets the header, and a browser is not the intended caller.
func guardOrigin(cfg Config, log *slog.Logger, next http.Handler) http.Handler {
	allowed := make(map[string]bool, len(cfg.AllowedOrigins))
	for _, origin := range cfg.AllowedOrigins {
		if origin = strings.TrimSpace(origin); origin != "" {
			allowed[strings.ToLower(origin)] = true
		}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" && !allowed[strings.ToLower(origin)] {
			log.Warn("mcp request rejected: unrecognised origin",
				slog.String("origin", origin),
				slog.String("remote", r.RemoteAddr))
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// anonymousPerMinute bounds unauthenticated attempts from one address.
//
// Far above anything a real client does, because this is not the spend limit —
// that is throttle's job, after the caller has a name. This exists only so a
// loop guessing tokens costs a map lookup instead of a database query.
const anonymousPerMinute = 600

// throttleAnonymous bounds requests by remote address before they are
// authenticated.
//
// Keyed by address, which throttle deliberately is not: an address is the only
// identity available before the token has been checked, and its weakness — a
// caller can change ports, or arrive from many hosts — is why this is a floor
// rather than the real limit.
func throttleAnonymous(_ Config, log *slog.Logger, next http.Handler) http.Handler {
	buckets := ratelimit.New(anonymousPerMinute)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		addr := r.RemoteAddr
		if host, _, err := net.SplitHostPort(addr); err == nil {
			addr = host
		}

		if !buckets.Allow(addr) {
			log.Warn("mcp request throttled before authentication", slog.String("remote", r.RemoteAddr))
			w.Header().Set("Retry-After", "1")
			http.Error(w, "too many requests", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// throttle bounds how often one account may call.
//
// ask_coach reaches a paid model on every invocation, and an agent in a retry
// loop is not hypothetical — the bill for one arrives before anyone notices.
// This runs after authenticate so the budget belongs to a known account: an
// unauthenticated caller can never consume an authenticated one's allowance.
func throttle(cfg Config, log *slog.Logger, next http.Handler) http.Handler {
	perMinute := cfg.RequestsPerMinute
	if perMinute <= 0 {
		perMinute = defaultRequestsPerMinute
	}
	buckets := ratelimit.New(perMinute)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := userFrom(r.Context())
		if !ok {
			// Unreachable: authenticate runs first and sets the user.
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		if !buckets.Allow(user.ID.String()) {
			log.Warn("mcp request throttled",
				slog.String("remote", r.RemoteAddr),
				slog.String("user_id", user.ID.String()))
			w.Header().Set("Retry-After", "1")
			http.Error(w, "too many requests", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type userKey struct{}

func userFrom(ctx context.Context) (users.User, bool) {
	u, ok := ctx.Value(userKey{}).(users.User)
	return u, ok
}

// authenticate resolves the bearer token to an account and puts it on the
// request context for everything downstream.
func authenticate(cfg Config, log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearer(r)
		if !ok {
			unauthorized(w, log, r)
			return
		}

		user, err := cfg.Auth.Authenticate(r.Context(), token)
		if err != nil {
			if apperr.Is(err, apperr.ErrUnauthenticated) || apperr.Is(err, apperr.ErrNotFound) {
				unauthorized(w, log, r)
				return
			}
			// A lookup that failed for any other reason is North's problem, not
			// the caller's. The detail goes to the operator: naming the cause in
			// the response told an unauthenticated-enough client how the server
			// is configured, in exchange for nothing.
			log.Error("mcp cannot resolve its caller", slog.Any("error", err))
			http.Error(w, "server misconfigured", http.StatusInternalServerError)
			return
		}

		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey{}, user)))
	})
}

func unauthorized(w http.ResponseWriter, log *slog.Logger, r *http.Request) {
	log.Warn("mcp request rejected", slog.String("remote", r.RemoteAddr))
	w.Header().Set("WWW-Authenticate", `Bearer realm="north-mcp"`)
	http.Error(w, "unauthorized", http.StatusUnauthorized)
}

func bearer(r *http.Request) (string, bool) {
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
