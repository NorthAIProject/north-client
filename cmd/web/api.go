package main

import (
	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/capture"
	"github.com/NorthAIProject/north-client/internal/coach"
	"github.com/NorthAIProject/north-client/internal/dashboard"
	"github.com/NorthAIProject/north-client/internal/exercises"
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
func mountAPI(r chi.Router, sessions auth.SessionResolver, apis apiSet) {
	r.Route("/api/v1", func(r chi.Router) {
		apis.auth.PublicRoutes(r)
		apis.capture.Routes(r)

		r.Group(func(r chi.Router) {
			r.Use(auth.RequireBearer(sessions))
			apis.auth.Routes(r)
			apis.onboarding.Routes(r)
			apis.dashboard.Routes(r)
			apis.coach.Routes(r)
			apis.exercises.Routes(r)
		})
	})
}

// apiSet is every feature API mounted under /api/v1. A struct rather than a
// growing parameter list: each phase of the native app adds one.
type apiSet struct {
	auth       *auth.API
	capture    *capture.API
	onboarding *onboarding.API
	dashboard  *dashboard.API
	coach      *coach.API
	exercises  *exercises.API
}
