package mcpserver

import (
	"context"
	"crypto/subtle"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/NorthAIProject/north-client/internal/users"
)

// Config is what the MCP server needs to serve.
type Config struct {
	Services Services

	// Token is the bearer every request must present.
	Token string

	// UserID is the account every tool call acts as.
	UserID uuid.UUID

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

	mux.Handle("/mcp", authenticate(cfg, log, streamable))

	return mux
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
			log.Error("mcp cannot load its user", slog.Any("error", err))
			http.Error(w, "the configured MCP_USER_ID does not resolve to an account", http.StatusInternalServerError)
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
