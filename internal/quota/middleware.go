package quota

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/NorthAIProject/north-client/internal/shared/httpx"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	quotapages "github.com/NorthAIProject/north-client/web/quota"
)

// Guard bounds a route by one action's budget.
//
// It is applied per route rather than per handler — `r.With(guard).Post(...)` —
// so that reading a page never spends the budget for writing to it. A person
// refreshing their report list should not be closer to being unable to generate
// one.
//
// The refusal is rendered here rather than handed to each handler's own error
// path because there is nothing route-specific about it: the answer is the same
// sentence everywhere, and threading a new sentinel through six handlers to say
// it would be more code for the same words.
func (s *Service) Guard(action Action) func(http.Handler) http.Handler {
	return s.guard(action, s.refuse)
}

// GuardJSON is Guard for the native app's API: the same budget and the same
// sentence, answered as the API's JSON error body instead of a page.
func (s *Service) GuardJSON(action Action) func(http.Handler) http.Handler {
	return s.guard(action, s.refuseJSON)
}

type refusal func(w http.ResponseWriter, r *http.Request, action Action, decision Decision)

func (s *Service) guard(action Action, refuse refusal) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			who, ok := s.identify(r.Context())
			if !ok {
				// Unreachable behind RequireAuth. Refusing rather than allowing,
				// because an unidentified caller on a guarded route means the
				// route is mounted wrong and an unbounded one is the worse of
				// the two failures.
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			decision, err := s.Consume(r.Context(), who.UserID, who.Tier, action)
			if err != nil {
				// Consume already fails open, so this is defensive.
				s.log.Error("quota check failed",
					slog.String("action", string(action)),
					slog.Any("error", err))
				next.ServeHTTP(w, r)
				return
			}

			if decision.Allowed {
				next.ServeHTTP(w, r)
				return
			}

			refuse(w, r, action, decision)
		})
	}
}

// refuse writes the 429 and the panel that replaces whatever asked for the
// work, in the reader's language.
func (s *Service) refuse(w http.ResponseWriter, r *http.Request, action Action, decision Decision) {
	w.Header().Set("Retry-After", strconv.Itoa(retrySeconds(decision)))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusTooManyRequests)

	if err := quotapages.RefusedFor(string(action), decision.RetryAfter).Render(r.Context(), w); err != nil {
		s.log.Error("rendering the quota refusal failed", slog.Any("error", err))
	}
}

// refuseJSON is refuse's 429 for the API, with the sentence as its message.
// The native app shows the message verbatim, so it is translated here.
//
// Code and fields carry the raw action and wait as well, so a web client can
// draw the same panel the cookie-session pages swap in (web/quota.Refused)
// rather than only the one sentence. Fields here are not form errors: a
// client keys on the code first.
func (s *Service) refuseJSON(w http.ResponseWriter, r *http.Request, action Action, decision Decision) {
	ctx := r.Context()
	seconds := strconv.Itoa(retrySeconds(decision))
	w.Header().Set("Retry-After", seconds)
	httpx.WriteJSON(w, http.StatusTooManyRequests, httpx.ErrorBody{Error: httpx.ErrorDetail{
		Message: i18n.Tf(ctx, "quota.api", quotapages.ActionLabel(ctx, string(action)), quotapages.DelayLabel(ctx, decision.RetryAfter)),
		Code:    CodeExceeded,
		Fields:  map[string]string{"action": string(action), "retryAfter": seconds},
	}})
}

// CodeExceeded is ErrorDetail.code on every quota refusal over the API.
const CodeExceeded = "quota_exceeded"

func retrySeconds(decision Decision) int {
	seconds := int(decision.RetryAfter.Round(time.Second) / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	return seconds
}
