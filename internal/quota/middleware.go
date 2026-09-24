package quota

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/NorthAIProject/north-client/internal/shared/httpx"
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
// work.
func (s *Service) refuse(w http.ResponseWriter, r *http.Request, action Action, decision Decision) {
	w.Header().Set("Retry-After", strconv.Itoa(retrySeconds(decision)))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusTooManyRequests)

	if err := quotapages.Refused(describe(action), humanDelay(decision.RetryAfter)).Render(r.Context(), w); err != nil {
		s.log.Error("rendering the quota refusal failed", slog.Any("error", err))
	}
}

// refuseJSON is refuse's 429 for the API, with the sentence as its message.
func (s *Service) refuseJSON(w http.ResponseWriter, _ *http.Request, action Action, decision Decision) {
	w.Header().Set("Retry-After", strconv.Itoa(retrySeconds(decision)))
	httpx.WriteJSON(w, http.StatusTooManyRequests, httpx.ErrorBody{Error: httpx.ErrorDetail{
		Message: "You have used all your " + describe(action) + " for now. Try again " + humanDelay(decision.RetryAfter) + ".",
	}})
}

func retrySeconds(decision Decision) int {
	seconds := int(decision.RetryAfter.Round(time.Second) / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	return seconds
}

// describe names an action the way a person would, since the wire name appears
// in the sentence they read.
func describe(action Action) string {
	switch action {
	case CoachMessage:
		return "coach messages"
	case DocumentUpload:
		return "uploads"
	case DocumentReindex:
		return "reindexing"
	case ReportGenerate:
		return "report generation"
	case MediaAnalysis:
		return "video analysis"
	case QuickCapture:
		return "quick capture"
	case VoiceCapture:
		return "voice notes"
	default:
		return string(action)
	}
}

// humanDelay renders a wait as something worth reading. "in 47 minutes" is
// useful; "in 2823 seconds" is a number a person has to do arithmetic on.
func humanDelay(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "in less than a minute"
	case d < 2*time.Minute:
		return "in about a minute"
	case d < time.Hour:
		return "in " + strconv.Itoa(int(d.Minutes())) + " minutes"
	case d < 2*time.Hour:
		return "in about an hour"
	default:
		return "in " + strconv.Itoa(int(d.Hours())) + " hours"
	}
}
