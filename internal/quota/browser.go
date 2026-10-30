package quota

import (
	"log/slog"
	"net/http"
	"strconv"

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
//
// This file is the browser's half of the guard and the only one that imports
// the templates; the API's GuardJSON and GuardAPI are in middleware.go.
func (s *Service) Guard(action Action) func(http.Handler) http.Handler {
	return s.guard(action, s.refuse)
}

// refuse writes the 429 and the panel that replaces whatever asked for the
// work, in the reader's language.
func (s *Service) refuse(w http.ResponseWriter, r *http.Request, action Action, decision Decision) {
	w.Header().Set("Retry-After", strconv.Itoa(retrySeconds(decision)))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusTooManyRequests)

	ctx := r.Context()
	panel := quotapages.Refused(actionLabel(ctx, string(action)), delayLabel(ctx, decision.RetryAfter))
	if err := panel.Render(ctx, w); err != nil {
		s.log.Error("rendering the quota refusal failed", slog.Any("error", err))
	}
}
