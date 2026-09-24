package main

import (
	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/capture"
	"github.com/NorthAIProject/north-client/internal/dashboard"
	"github.com/NorthAIProject/north-client/internal/onboarding"
)

// mountAPI owns the public JSON API boundary. Feature APIs register paths
// relative to /api/v1 so versioning stays centralized when more endpoints are
// added.
//
// Three groups, by how a request proves who it is:
//   - public: no identity yet (sign-in, sign-up)
//   - bearer: a session token from sign-in, the native app's normal case
//   - capture: its own nk_ connection token, for agents and shortcuts
func mountAPI(r chi.Router, sessions auth.SessionResolver, captureAPI *capture.API, authAPI *auth.API, onboardingAPI *onboarding.API, dashboardAPI *dashboard.API) {
	r.Route("/api/v1", func(r chi.Router) {
		authAPI.PublicRoutes(r)
		captureAPI.Routes(r)

		r.Group(func(r chi.Router) {
			r.Use(auth.RequireBearer(sessions))
			authAPI.Routes(r)
			onboardingAPI.Routes(r)
			dashboardAPI.Routes(r)
		})
	})
}
