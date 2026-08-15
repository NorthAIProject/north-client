package health

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

// Authenticator resolves a presented bearer token to the account it acts as.
//
// Declared here rather than imported from mcpserver so this package does not
// depend on the MCP surface to accept an HTTP POST. connections.Service
// satisfies both, which is the point: one credential a person can see and
// revoke in one place, whichever endpoint they point at.
type Authenticator interface {
	Authenticate(ctx context.Context, token string) (users.User, error)
}

type HandlerConfig struct {
	Service *Service

	// Auth decides who a request acts as. Required.
	Auth Authenticator

	Log *slog.Logger
}

// sourcePattern bounds what may name a provider.
//
// Deliberately a shape check and not an allowlist. An allowlist would put a
// code change between a new provider and its first reading, which is the same
// friction the CHECK constraint on activity_sessions.source creates — only the
// cost is a deploy instead of a migration. This exists so a source stays
// something a person can read in a disconnect button and an operator can grep
// in a log, and so nothing exotic reaches the column.
//
// Callers discover which sources actually exist by asking the database, not by
// consulting a list in the code.
var sourcePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_]{1,31}$`)

// NewHandler serves health ingest.
//
// The route is POST {source}, because a person may push from more than one
// place with the same token — a phone bridge and a laptop script are different
// providers of the same account — and the row has to record which.
//
// # Why this is not behind the session
//
// The caller is a background process on somebody's phone. It has no cookie, no
// browser, and no way to complete a CSRF exchange, so this endpoint is mounted
// outside that chain and the bearer token carries the whole identity. That
// makes the token the only thing in front of a write, which is why an
// unauthenticated request never reaches the decoder.
func NewHandler(cfg HandlerConfig) http.Handler {
	log := cfg.Log
	if log == nil {
		log = slog.Default()
	}

	return authenticate(cfg.Auth, log, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		source := strings.Trim(r.URL.Path, "/")
		if !sourcePattern.MatchString(source) {
			http.Error(w, "source must be 2-32 characters of a-z, 0-9 and underscore", http.StatusBadRequest)
			return
		}

		user, ok := userFrom(r.Context())
		if !ok {
			// Unreachable: authenticate runs first and sets the user.
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		var payload ingestPayload
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&payload); err != nil {
			// The decoder's message names the offending field, which is what a
			// person debugging their own bridge needs and reveals nothing they
			// did not already send.
			http.Error(w, "malformed payload: "+err.Error(), http.StatusBadRequest)
			return
		}

		readings, err := payload.readings()
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		result, err := cfg.Service.Ingest(r.Context(), user.ID, source, readings)
		if err != nil {
			if apperr.Is(err, apperr.ErrValidation) {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			log.Error("health ingest failed",
				slog.String("source", source),
				slog.String("user_id", user.ID.String()),
				slog.Any("error", err))
			http.Error(w, "could not store readings", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]int{"written": result.Written})
	}))
}

type ingestPayload struct {
	Readings []struct {
		Metric    string  `json:"metric"`
		Value     float64 `json:"value"`
		Unit      string  `json:"unit"`
		StartedAt string  `json:"started_at"`
		EndedAt   *string `json:"ended_at"`
	} `json:"readings"`
}

// readings converts the wire shape into the domain one.
//
// Timestamps are parsed here rather than by the JSON decoder so a bad one is
// reported with the reading that carried it. A bridge sending ten thousand
// samples cannot act on "invalid time" without being told which.
func (p ingestPayload) readings() ([]Reading, error) {
	out := make([]Reading, 0, len(p.Readings))
	for i, in := range p.Readings {
		started, err := time.Parse(time.RFC3339, in.StartedAt)
		if err != nil {
			return nil, apperr.Wrap(apperr.ErrValidation,
				"reading %d (%s): started_at must be RFC 3339", i, in.Metric)
		}

		var ended *time.Time
		if in.EndedAt != nil {
			parsed, err := time.Parse(time.RFC3339, *in.EndedAt)
			if err != nil {
				return nil, apperr.Wrap(apperr.ErrValidation,
					"reading %d (%s): ended_at must be RFC 3339", i, in.Metric)
			}
			ended = &parsed
		}

		out = append(out, Reading{
			Metric:    in.Metric,
			Value:     in.Value,
			Unit:      in.Unit,
			StartedAt: started,
			EndedAt:   ended,
		})
	}
	return out, nil
}

type userKey struct{}

func userFrom(ctx context.Context) (users.User, bool) {
	u, ok := ctx.Value(userKey{}).(users.User)
	return u, ok
}

// authenticate resolves the bearer token to an account before anything else
// reads the request.
func authenticate(auth Authenticator, log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearer(r)
		if !ok {
			unauthorized(w, log, r)
			return
		}

		user, err := auth.Authenticate(r.Context(), token)
		if err != nil {
			if apperr.Is(err, apperr.ErrUnauthenticated) || apperr.Is(err, apperr.ErrNotFound) {
				unauthorized(w, log, r)
				return
			}
			// A lookup that failed for another reason is North's problem, not
			// the caller's, and naming it would describe the deployment to
			// someone who has not authenticated.
			log.Error("health ingest cannot resolve its caller", slog.Any("error", err))
			http.Error(w, "server misconfigured", http.StatusInternalServerError)
			return
		}

		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey{}, user)))
	})
}

func unauthorized(w http.ResponseWriter, log *slog.Logger, r *http.Request) {
	log.Warn("health ingest rejected", slog.String("remote", r.RemoteAddr))
	w.Header().Set("WWW-Authenticate", `Bearer realm="north-health-ingest"`)
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
