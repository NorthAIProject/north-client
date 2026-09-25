package main

import (
	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/activity"
	"github.com/NorthAIProject/north-client/internal/apns"
	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/calculator"
	"github.com/NorthAIProject/north-client/internal/capture"
	"github.com/NorthAIProject/north-client/internal/care"
	"github.com/NorthAIProject/north-client/internal/checkins"
	"github.com/NorthAIProject/north-client/internal/coach"
	"github.com/NorthAIProject/north-client/internal/dashboard"
	"github.com/NorthAIProject/north-client/internal/decisions"
	"github.com/NorthAIProject/north-client/internal/documents"
	"github.com/NorthAIProject/north-client/internal/exercises"
	"github.com/NorthAIProject/north-client/internal/export"
	"github.com/NorthAIProject/north-client/internal/fitness"
	"github.com/NorthAIProject/north-client/internal/goals"
	"github.com/NorthAIProject/north-client/internal/health"
	"github.com/NorthAIProject/north-client/internal/insights"
	"github.com/NorthAIProject/north-client/internal/meals"
	"github.com/NorthAIProject/north-client/internal/media"
	"github.com/NorthAIProject/north-client/internal/memories"
	"github.com/NorthAIProject/north-client/internal/mind"
	"github.com/NorthAIProject/north-client/internal/news"
	"github.com/NorthAIProject/north-client/internal/nudges"
	"github.com/NorthAIProject/north-client/internal/onboarding"
	"github.com/NorthAIProject/north-client/internal/reports"
	"github.com/NorthAIProject/north-client/internal/settings"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	"github.com/NorthAIProject/north-client/internal/workouts"
)

// mountAPI owns the public JSON API boundary. Feature APIs register paths
// relative to /api/v1 so versioning stays centralized when more endpoints are
// added.
//
// Groups, by how a request proves who it is:
//   - public: no identity yet (sign-in, sign-up, Strava's OAuth return)
//   - bearer: a session token from sign-in, the native app's normal case
//   - capture: its own nk_ connection token, for agents and shortcuts
//   - uploads: bearer too, with a body cap that fits a file
//
// The body caps live here rather than around the mount, because one cap does
// not fit both: JSON is a few kilobytes and a filmed set is up to 200 MB.
func mountAPI(r chi.Router, sessions auth.SessionResolver, apis apiSet) {
	r.Route("/api/v1", func(r chi.Router) {
		// A guess from Accept-Language for the signed-out routes, so an account
		// created from the app starts in the phone's language. RequireBearer
		// replaces it with the account's own setting.
		r.Use(middleware.Locale)

		r.Group(func(r chi.Router) {
			r.Use(middleware.MaxBody(maxJSONBody))
			mountJSON(r, sessions, apis)
		})

		r.Group(func(r chi.Router) {
			r.Use(middleware.MaxBody(maxUploadBody))
			r.Use(auth.RequireBearer(sessions))
			apis.knowledge.UploadRoutes(r)
			apis.formChecks.UploadRoutes(r)
		})
	})
}

const (
	// maxJSONBody bounds every JSON request. A health sync of daily
	// aggregates, the largest, is tens of kilobytes.
	maxJSONBody = 1 << 20
	// maxUploadBody fits the largest file the API takes, a form-check video,
	// with room for the multipart framing around it.
	maxUploadBody = media.MaxVideoBytes + 1<<20
)

func mountJSON(r chi.Router, sessions auth.SessionResolver, apis apiSet) {
	apis.auth.PublicRoutes(r)
	apis.capture.Routes(r)
	apis.fitness.PublicRoutes(r)

	r.Group(func(r chi.Router) {
		r.Use(auth.RequireBearer(sessions))
		apis.auth.Routes(r)
		apis.onboarding.Routes(r)
		apis.dashboard.Routes(r)
		apis.coach.Routes(r)
		apis.exercises.Routes(r)
		apis.settings.Routes(r)
		apis.training.Routes(r)
		apis.activity.Routes(r)
		apis.health.Routes(r)
		apis.fitness.Routes(r)
		apis.insights.Routes(r)
		apis.goals.Routes(r)
		apis.checkins.Routes(r)
		apis.reports.Routes(r)
		apis.memories.Routes(r)
		apis.knowledge.Routes(r)
		apis.formChecks.Routes(r)
		apis.care.Routes(r)
		apis.mind.Routes(r)
		apis.nutrition.Routes(r)
		apis.decisions.Routes(r)
		apis.nudges.Routes(r)
		apis.devices.Routes(r)
		apis.export.APIRoutes(r)
		apis.calculator.Routes(r)
		apis.news.Routes(r)
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
	settings   *settings.API
	training   *workouts.API
	activity   *activity.API
	health     *health.API
	fitness    *fitness.API
	insights   *insights.API
	goals      *goals.API
	checkins   *checkins.API
	reports    *reports.API
	memories   *memories.API
	knowledge  *documents.API
	formChecks *media.API
	care       *care.API
	mind       *mind.API
	nutrition  *meals.API
	decisions  *decisions.API
	nudges     *nudges.API
	devices    *apns.API
	export     *export.Handler
	calculator *calculator.API
	news       *news.API
}
