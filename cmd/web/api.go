package main

import (
	"github.com/labstack/echo/v5"
	echomw "github.com/labstack/echo/v5/middleware"

	"github.com/NorthAIProject/north-client/internal/achievements"
	"github.com/NorthAIProject/north-client/internal/activity"
	"github.com/NorthAIProject/north-client/internal/apns"
	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/awards"
	"github.com/NorthAIProject/north-client/internal/caffeine"
	"github.com/NorthAIProject/north-client/internal/calculator"
	"github.com/NorthAIProject/north-client/internal/capture"
	"github.com/NorthAIProject/north-client/internal/care"
	"github.com/NorthAIProject/north-client/internal/checkinreply"
	"github.com/NorthAIProject/north-client/internal/checkins"
	"github.com/NorthAIProject/north-client/internal/coach"
	"github.com/NorthAIProject/north-client/internal/crews"
	"github.com/NorthAIProject/north-client/internal/dailyposts"
	"github.com/NorthAIProject/north-client/internal/dashboard"
	"github.com/NorthAIProject/north-client/internal/day"
	"github.com/NorthAIProject/north-client/internal/decisions"
	"github.com/NorthAIProject/north-client/internal/documents"
	"github.com/NorthAIProject/north-client/internal/exercises"
	"github.com/NorthAIProject/north-client/internal/export"
	"github.com/NorthAIProject/north-client/internal/fasting"
	"github.com/NorthAIProject/north-client/internal/firstweek"
	"github.com/NorthAIProject/north-client/internal/fitness"
	"github.com/NorthAIProject/north-client/internal/goals"
	"github.com/NorthAIProject/north-client/internal/health"
	"github.com/NorthAIProject/north-client/internal/inbox"
	"github.com/NorthAIProject/north-client/internal/insights"
	"github.com/NorthAIProject/north-client/internal/lifts"
	"github.com/NorthAIProject/north-client/internal/lighterday"
	"github.com/NorthAIProject/north-client/internal/mcpauth"
	"github.com/NorthAIProject/north-client/internal/meals"
	"github.com/NorthAIProject/north-client/internal/media"
	"github.com/NorthAIProject/north-client/internal/medications"
	"github.com/NorthAIProject/north-client/internal/memories"
	"github.com/NorthAIProject/north-client/internal/milestones"
	"github.com/NorthAIProject/north-client/internal/mind"
	"github.com/NorthAIProject/north-client/internal/moderation"
	"github.com/NorthAIProject/north-client/internal/news"
	"github.com/NorthAIProject/north-client/internal/nudges"
	"github.com/NorthAIProject/north-client/internal/onboarding"
	"github.com/NorthAIProject/north-client/internal/outreach"
	"github.com/NorthAIProject/north-client/internal/planimport"
	"github.com/NorthAIProject/north-client/internal/profiles"
	"github.com/NorthAIProject/north-client/internal/progresschecks"
	"github.com/NorthAIProject/north-client/internal/push"
	"github.com/NorthAIProject/north-client/internal/recaps"
	"github.com/NorthAIProject/north-client/internal/reports"
	"github.com/NorthAIProject/north-client/internal/screentime"
	"github.com/NorthAIProject/north-client/internal/settings"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	"github.com/NorthAIProject/north-client/internal/social"
	"github.com/NorthAIProject/north-client/internal/soreness"
	"github.com/NorthAIProject/north-client/internal/stats"
	"github.com/NorthAIProject/north-client/internal/supplements"
	"github.com/NorthAIProject/north-client/internal/vault"
	"github.com/NorthAIProject/north-client/internal/voice"
	"github.com/NorthAIProject/north-client/internal/weekly"
	"github.com/NorthAIProject/north-client/internal/workouts"
	"github.com/NorthAIProject/north-client/internal/xp"
)

// mountAPI owns the public JSON API boundary, on Echo. Feature APIs register
// paths relative to /api/v1 so versioning stays centralized when more
// endpoints are added.
//
// Groups, by how a request proves who it is:
//   - public: no identity yet (sign-in, sign-up, Strava's OAuth return)
//   - bearer: a session token from sign-in, the native app's normal case
//   - capture: its own nk_ connection token, for agents and shortcuts
//   - uploads: bearer too, with a body cap that fits a file
//
// The body caps live here rather than around the mount, because one cap does
// not fit both: JSON is a few kilobytes and a filmed set is up to 90 MB.
func mountAPI(e *echo.Echo, sessions auth.SessionResolver, apis apiSet) {
	// A guess from Accept-Language for the signed-out routes, so an account
	// created from the app starts in the phone's language. auth.Bearer
	// replaces it with the account's own setting.
	api := e.Group("/api/v1", echo.WrapMiddleware(middleware.Locale))

	json := api.Group("", echomw.BodyLimit(maxJSONBody))
	apis.auth.RegisterPublic(json)
	// The MCP consent screen's server half, for a web front end that renders
	// the screen itself. Parking and account creation are public; the
	// decision, below, needs the bearer.
	apis.mcpAuth.RegisterPublic(json)
	apis.social.RegisterPublic(json)
	apis.crews.RegisterPublic(json)
	apis.capture.Register(json)
	apis.fitness.RegisterPublic(json)
	registerBearer(json.Group("", auth.Bearer(sessions)), apis)

	uploads := api.Group("", echomw.BodyLimit(maxUploadBody), auth.Bearer(sessions))
	apis.knowledge.RegisterUploads(uploads)
	apis.coach.RegisterUploads(uploads)
	apis.voice.RegisterUploads(uploads)
	apis.formChecks.RegisterUploads(uploads)
	apis.planImport.RegisterUploads(uploads)
	apis.lifts.RegisterUploads(uploads)
	apis.progress.RegisterUploads(uploads)

	// /api/v1 belongs to the API. A path nothing here routes is a JSON 404,
	// never a fall through to the browser app and its HTML.
	api.RouteNotFound("/*", func(*echo.Context) error { return echo.ErrNotFound })
}

const (
	// maxJSONBody bounds every JSON request. A health sync of daily
	// aggregates, the largest, is tens of kilobytes.
	maxJSONBody = 1 << 20
	// maxUploadBody fits the largest file the API takes, a form-check video,
	// with room for the multipart framing around it.
	maxUploadBody = media.MaxVideoBytes + 1<<20
)

func registerBearer(g *echo.Group, apis apiSet) {
	apis.auth.Register(g)
	apis.mcpAuth.Register(g)
	apis.onboarding.Register(g)
	apis.dashboard.Register(g)
	apis.day.Register(g)
	apis.caffeine.Register(g)
	apis.lifts.Register(g)
	apis.stats.Register(g)
	apis.fasting.Register(g)
	apis.supplements.Register(g)
	apis.medications.Register(g)
	apis.progress.Register(g)
	apis.screenTime.Register(g)
	apis.soreness.Register(g)
	apis.milestones.Register(g)
	apis.coach.Register(g)
	apis.voice.Register(g)
	apis.exercises.Register(g)
	apis.settings.Register(g)
	apis.training.Register(g)
	apis.activity.Register(g)
	apis.health.Register(g)
	apis.fitness.Register(g)
	apis.insights.Register(g)
	apis.goals.Register(g)
	apis.checkins.Register(g)
	apis.checkinReply.Register(g)
	apis.reports.Register(g)
	apis.memories.Register(g)
	apis.knowledge.Register(g)
	apis.formChecks.Register(g)
	apis.care.Register(g)
	apis.mind.Register(g)
	apis.nutrition.Register(g)
	apis.planImport.Register(g)
	apis.decisions.Register(g)
	apis.nudges.Register(g)
	apis.devices.Register(g)
	apis.export.RegisterAPI(g)
	apis.vault.RegisterAPI(g)
	apis.calculator.Register(g)
	apis.news.Register(g)
	apis.social.Register(g)
	apis.profiles.Register(g)
	apis.achievements.Register(g)
	apis.awards.Register(g)
	apis.crews.Register(g)
	apis.moderation.Register(g)
	apis.xp.Register(g)
	apis.weekly.Register(g)
	apis.recaps.Register(g)
	apis.dailyPosts.Register(g)
	apis.inbox.Register(g)
	apis.lighter.Register(g)
	apis.outreach.Register(g)
	apis.firstWeek.Register(g)
	apis.push.Register(g)
}

// apiSet is every feature API mounted under /api/v1. A struct rather than a
// growing parameter list: each phase of the native app adds one.
type apiSet struct {
	auth         *auth.API
	mcpAuth      *mcpauth.API
	capture      *capture.API
	onboarding   *onboarding.API
	dashboard    *dashboard.API
	day          *day.API
	caffeine     *caffeine.API
	lifts        *lifts.API
	stats        *stats.API
	fasting      *fasting.API
	supplements  *supplements.API
	medications  *medications.API
	progress     *progresschecks.API
	screenTime   *screentime.API
	soreness     *soreness.API
	milestones   *milestones.API
	coach        *coach.API
	voice        *voice.API
	exercises    *exercises.API
	settings     *settings.API
	training     *workouts.API
	activity     *activity.API
	health       *health.API
	fitness      *fitness.API
	insights     *insights.API
	goals        *goals.API
	checkins     *checkins.API
	checkinReply *checkinreply.API
	reports      *reports.API
	memories     *memories.API
	knowledge    *documents.API
	formChecks   *media.API
	care         *care.API
	mind         *mind.API
	nutrition    *meals.API
	planImport   *planimport.API
	decisions    *decisions.API
	nudges       *nudges.API
	devices      *apns.API
	export       *export.Handler
	vault        *vault.Handler
	calculator   *calculator.API
	news         *news.API
	social       *social.API
	profiles     *profiles.API
	achievements *achievements.API
	awards       *awards.API
	crews        *crews.API
	moderation   *moderation.API
	xp           *xp.API
	weekly       *weekly.API
	recaps       *recaps.API
	dailyPosts   *dailyposts.API
	inbox        *inbox.API
	lighter      *lighterday.API
	outreach     *outreach.API
	firstWeek    *firstweek.API
	push         *push.API
}
