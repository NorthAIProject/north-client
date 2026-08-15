package health

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/ratelimit"
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

	// RequestsPerMinute bounds one account's ingest rate. Zero uses the
	// package default.
	RequestsPerMinute int

	// AnonymousPerMinute bounds unauthenticated attempts from one address.
	// Zero uses the package default.
	AnonymousPerMinute int

	Log *slog.Logger
}

// defaultRequestsPerMinute bounds one account.
//
// Higher than the MCP surface allows itself, because a phone catching up after
// a week offline legitimately sends many payloads in a row, and none of them
// reach a paid model. The cost of a request here is a transaction, not a bill.
const defaultRequestsPerMinute = 240

// defaultAnonymousPerMinute bounds unauthenticated attempts from one address.
//
// Far above anything a real client does, because this is not the spend limit —
// that is the per-account bound's job, after the caller has a name. This exists
// only so a loop guessing tokens costs a map lookup instead of a database
// query.
const defaultAnonymousPerMinute = 600

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

	// Order matters, and it is not the obvious one.
	//
	// The real bound needs an identity, so it has to run after authentication —
	// which means a token-guessing loop would otherwise reach the database on
	// every attempt. throttleAnonymous sits in front to absorb that: generous
	// enough that no real client notices, cheap enough that a flood costs a map
	// lookup rather than a query.
	return throttleAnonymous(cfg, log, authenticate(cfg.Auth, log,
		throttle(cfg, log, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

			workouts, err := payload.workouts()
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			// Both halves are optional, so a bridge that only has one kind of
			// data does not have to send an empty array of the other.
			var written, workoutsWritten int
			if len(readings) > 0 {
				result, ingestErr := cfg.Service.Ingest(r.Context(), user.ID, source, readings)
				if ingestErr != nil {
					fail(w, log, source, user.ID.String(), ingestErr)
					return
				}
				written = result.Written
			}
			if len(workouts) > 0 {
				result, ingestErr := cfg.Service.IngestWorkouts(r.Context(), user.ID, source, workouts)
				if ingestErr != nil {
					fail(w, log, source, user.ID.String(), ingestErr)
					return
				}
				workoutsWritten = result.Written
			}
			if written == 0 && workoutsWritten == 0 {
				http.Error(w, "payload carried neither readings nor workouts", http.StatusBadRequest)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]int{
				"written":  written,
				"workouts": workoutsWritten,
			})
		}))))
}

// fail reports an ingest error, distinguishing the caller's fault from North's.
func fail(w http.ResponseWriter, log *slog.Logger, source, userID string, err error) {
	if apperr.Is(err, apperr.ErrValidation) {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	log.Error("health ingest failed",
		slog.String("source", source),
		slog.String("user_id", userID),
		slog.Any("error", err))
	http.Error(w, "could not store payload", http.StatusInternalServerError)
}

// workouts converts the wire shape into the domain one, parsing timestamps here
// so a bad one is reported with the workout that carried it.
func (p ingestPayload) workouts() ([]Workout, error) {
	out := make([]Workout, 0, len(p.Workouts))
	for i, in := range p.Workouts {
		started, err := time.Parse(time.RFC3339, in.StartedAt)
		if err != nil {
			return nil, apperr.Wrap(apperr.ErrValidation,
				"workout %d (%s): started_at must be RFC 3339", i, in.ActivityCode)
		}
		ended, err := time.Parse(time.RFC3339, in.EndedAt)
		if err != nil {
			return nil, apperr.Wrap(apperr.ErrValidation,
				"workout %d (%s): ended_at must be RFC 3339", i, in.ActivityCode)
		}
		out = append(out, Workout{
			ActivityCode: in.ActivityCode,
			ExternalID:   in.ExternalID,
			StartedAt:    started,
			EndedAt:      ended,
			Calories:     in.Calories,
		})
	}
	return out, nil
}

// throttleAnonymous bounds requests by remote address before they are
// authenticated.
//
// Keyed by address, which throttle deliberately is not: an address is the only
// identity available before the token has been checked, and its weakness — a
// caller can change ports, or arrive from many hosts — is why this is a floor
// rather than the real limit.
func throttleAnonymous(cfg HandlerConfig, log *slog.Logger, next http.Handler) http.Handler {
	perMinute := cfg.AnonymousPerMinute
	if perMinute <= 0 {
		perMinute = defaultAnonymousPerMinute
	}
	buckets := ratelimit.New(perMinute)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		addr := r.RemoteAddr
		if host, _, err := net.SplitHostPort(addr); err == nil {
			addr = host
		}

		if !buckets.Allow(addr) {
			log.Warn("health ingest throttled before authentication", slog.String("remote", r.RemoteAddr))
			tooManyRequests(w)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// throttle bounds how often one account may push.
//
// It runs after authenticate so the budget belongs to a known account: an
// unauthenticated caller can never consume an authenticated one's allowance,
// and a caller that changes address does not get a fresh one.
func throttle(cfg HandlerConfig, log *slog.Logger, next http.Handler) http.Handler {
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
			log.Warn("health ingest throttled",
				slog.String("remote", r.RemoteAddr),
				slog.String("user_id", user.ID.String()))
			tooManyRequests(w)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func tooManyRequests(w http.ResponseWriter) {
	w.Header().Set("Retry-After", "1")
	http.Error(w, "too many requests", http.StatusTooManyRequests)
}

type ingestPayload struct {
	Workouts []struct {
		ActivityCode string  `json:"activity_code"`
		ExternalID   string  `json:"external_id"`
		StartedAt    string  `json:"started_at"`
		EndedAt      string  `json:"ended_at"`
		Calories     float64 `json:"calories"`
	} `json:"workouts"`

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
