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
func mountAPI(r chi.Router, captureAPI *capture.API, authAPI *auth.API, onboardingAPI *onboarding.API, dashboardAPI *dashboard.API) {
	r.Route("/api/v1", func(r chi.Router) {
		authAPI.Routes(r)
		onboardingAPI.Routes(r)
		dashboardAPI.Routes(r)
		captureAPI.Routes(r)
	})
}
