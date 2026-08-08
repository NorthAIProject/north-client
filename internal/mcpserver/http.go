package mcpserver

import (
	"context"
	"crypto/subtle"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/time/rate"

	"github.com/NorthAIProject/north-client/internal/users"
)

// defaultRequestsPerMinute bounds an unmetered LLM-spend path. Generous for a
// human-driven agent, and low enough that a retry loop is noticed rather than
// billed.
const defaultRequestsPerMinute = 120

// Config is what the MCP server needs to serve.
type Config struct {
	Services Services

	// Token is the bearer every request must present.
	Token string

	// UserID is the account every tool call acts as.
	UserID uuid.UUID

	// AllowedOrigins are the browser origins permitted to reach /mcp.
	//
	// Empty means no browser origin is allowed, which is the right default: a
	// real MCP client sends no Origin at all. See guardOrigin.
	AllowedOrigins []string

	// RequestsPerMinute bounds one token's call rate. Zero uses the default.
	RequestsPerMinute int

	Version string
	Log     *slog.Logger
}

// NewHandler builds the HTTP surface: health checks and the MCP endpoint.
//
// # Single user, by design
//
// One static token maps to one account. There is no per-caller identity, no
// scopes, and no way to act as anyone else — which is exactly why this must not
// be exposed publicly. Bind it to the tailnet interface and leave it there.
// Anyone holding the token has full read and write access to that user's
// coaching data.
//
// A multi-tenant version needs personal access tokens and an introspection
// endpoint, the way apps/norviq/norviq-mcp does it. That is a separate piece of
// work, and pretending otherwise here would be the dangerous version of this
// comment.
func NewHandler(cfg Config) http.Handler {
	log := cfg.Log
	if log == nil {
		log = slog.Default()
	}

	version := cfg.Version
	if version == "" {
		version = "0.1.0"
	}

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

	// Order matters: reject the browser first, then the wrong token, then the
	// caller who is asking too often. Each stage is cheaper than the next, and
	// an unauthenticated caller must never be able to consume the rate budget
	// of the authenticated one.
	mux.Handle("/mcp", guardOrigin(cfg, log, authenticate(cfg, log, throttle(cfg, log, streamable))))

	return mux
}

// guardOrigin rejects requests carrying a browser origin North does not know.
//
// The SDK's StreamableHTTPHandler is constructed with no options, so nothing
// else here validates Origin or Host, and DNS-rebinding protection rested
// entirely on the loopback bind. That is one misconfigured MCP_LISTEN_ADDR away
// from a page on the open web driving somebody's coach.
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

// throttle bounds how often the token may call.
//
// ask_coach reaches a paid model on every invocation, behind one static bearer
// with no per-caller identity. An agent in a retry loop is not hypothetical,
// and the bill for one arrives before anyone notices.
//
// One limiter for the whole server rather than per remote address: there is
// exactly one credential, so per-address buckets would only let a caller reset
// its own budget by changing port.
func throttle(cfg Config, log *slog.Logger, next http.Handler) http.Handler {
	perMinute := cfg.RequestsPerMinute
	if perMinute <= 0 {
		perMinute = defaultRequestsPerMinute
	}

	// Burst is a full minute's worth: a client listing tools and then calling
	// several in quick succession is normal, and shaping that into a trickle
	// would make the server feel broken.
	limiter := rate.NewLimiter(rate.Limit(float64(perMinute)/60.0), perMinute)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !limiter.Allow() {
			log.Warn("mcp request throttled", slog.String("remote", r.RemoteAddr))
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

// authenticate checks the bearer token and loads the single account the server
// acts as.
func authenticate(cfg Config, log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearer(r)
		// Compared in constant time so the response latency does not leak how
		// much of the token was correct.
		if !ok || subtle.ConstantTimeCompare([]byte(token), []byte(cfg.Token)) != 1 {
			log.Warn("mcp request rejected", slog.String("remote", r.RemoteAddr))
			w.Header().Set("WWW-Authenticate", `Bearer realm="north-mcp"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		user, err := cfg.Services.Users.ByID(r.Context(), cfg.UserID)
		if err != nil {
			// The detail goes to the operator, not to the caller. Naming the
			// environment variable in the response told an unauthenticated-
			// enough client how the server is configured, in exchange for
			// nothing: whoever can fix it is reading these logs.
			log.Error("mcp cannot load its user",
				slog.Any("error", err),
				slog.String("configured_user_id", cfg.UserID.String()))
			http.Error(w, "server misconfigured", http.StatusInternalServerError)
			return
		}

		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey{}, user)))
	})
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
