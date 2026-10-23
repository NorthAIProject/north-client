package firstweek

import (
	"net/http"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/web/shared/layout"
)

// SimpleNav folds the rail under More for a person whose first week is
// running, from day 0 to the review. Only people with a programme are
// affected, and only onboarding starts one, so accounts from before the
// first week never see the change.
//
// One indexed read per page. A failed read leaves the full rail: the
// page matters more than the fold.
func (s *Service) SimpleNav(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFrom(r.Context())
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		p, err := s.repo.Get(r.Context(), user.ID)
		if err == nil && p.Status == StatusActive && p.Day(s.now()) <= Days {
			r = r.WithContext(layout.WithSimpleNav(r.Context()))
		}
		next.ServeHTTP(w, r)
	})
}
