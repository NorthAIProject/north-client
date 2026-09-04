package layout

// Destinations is every page a signed-in person can navigate to, in one list.
//
// It exists because the sidebar was never the whole application. Thirteen
// entries were reachable from the rail while roughly thirty pages existed, and
// the missing ones lived in a second hardcoded list inside the fitness hub, in
// back-links at the top of settings sub-pages, or nowhere at all. "I cannot
// find things" was the accurate description of a real gap, not a preference.
//
// The command palette and BuildNav both read this list, and BuildNav is
// derived from it rather than kept beside it. A supplementary "pages the
// sidebar does not show" list would recreate exactly the drift that centralising
// the nav was meant to end: the second time somebody adds a page, they update
// one list and not the other, and the palette goes quietly stale.
//
// Adding a page means adding a row here. cmd/web's route test fails until you
// do, which is the point.

// Groups, in the order they appear in both the rail and the palette.
const (
	GroupToday    = "Today"
	GroupBody     = "Body"
	GroupMind     = "Mind"
	GroupProgress = "Progress"
	GroupSystem   = "System"
)

// Destination is one navigable page.
type Destination struct {
	// Label is what the palette shows, and the first thing a query matches.
	Label string

	// Href is a literal GET route. No path parameters: a destination has to be
	// somewhere you can go without already knowing an id.
	Href string

	// Icon is a Lucide name and is required. An unknown name makes icon.Icon
	// return an error, templ flushes nothing, and every signed-in page answers
	// 200 with an empty body — so this is covered by a test rather than trust.
	Icon string

	Group string

	// Description is one short line, shown beside the label.
	Description string

	// Keywords are lowercase synonyms: matched, never rendered. This is where
	// recall comes from, and it is why the matcher can stay a substring test
	// instead of a fuzzy ranker nobody can predict.
	Keywords []string

	Nav NavPlacement
}

// NavPlacement is how a destination appears in the sidebar, if at all.
type NavPlacement struct {
	// Show puts the destination in the rail.
	Show bool

	// Label overrides Destination.Label in the rail. The palette needs
	// "Body insights" to be findable on its own; the rail, already under an
	// Insights heading, only needs "Body".
	Label string

	// Under is the Href of the parent item this nests below. Empty means top
	// level.
	Under string

	// SelfChildLabel is for the one page that is both a section landing page
	// and its own first child: /app/insights/timeline is the Insights item and
	// the Activity view under it. One documented field beats a duplicate row,
	// which keeps "no two destinations share an Href" a hard invariant.
	SelfChildLabel string
}

// GroupOrder is the order groups appear in.
func GroupOrder() []string {
	return []string{GroupToday, GroupBody, GroupMind, GroupProgress, GroupSystem}
}

// Destinations returns the registry.
//
// Three kinds of route are deliberately absent, and belong in the route test's
// deny-list rather than here:
//
//   - HTMX partials (/app/panels, /app/nudges/bell, /app/insights/*/body),
//     which are fragments of a page rather than a page.
//   - /app/settings/export.zip, which is a quota-consuming download. "Go to
//     page" must not start an account export.
//   - /app/settings/vault, which is mounted only outside production
//     (cmd/web/main.go passes !cfg.Env.IsProduction()). Listing it would ship a
//     guaranteed 404, and reading config here would put deployment knowledge
//     into the layout package.
func Destinations() []Destination {
	return []Destination{
		// Today
		{
			Label: "Overview", Href: "/app", Icon: "layout-dashboard", Group: GroupToday,
			Description: "Where today stands.",
			Keywords:    []string{"dashboard", "home", "today", "start"},
			Nav:         NavPlacement{Show: true},
		},
		{
			Label: "Quick capture", Href: "/app/capture", Icon: "zap", Group: GroupToday,
			Description: "Write your day in one line.",
			Keywords:    []string{"log", "add", "quick", "capture", "water", "sleep", "weight", "habit", "note"},
			Nav:         NavPlacement{Show: true},
		},
		{
			Label: "Check-ins", Href: "/app/check-ins", Icon: "smile", Group: GroupToday,
			Description: "How you are doing, in your words.",
			Keywords:    []string{"mood", "daily", "log", "feeling"},
			Nav:         NavPlacement{Show: true},
		},
		{
			Label: "Coach", Href: "/app/chat", Icon: "message-circle", Group: GroupToday,
			Description: "Talk it through.",
			Keywords:    []string{"chat", "ai", "ask", "assistant", "conversation"},
			Nav:         NavPlacement{Show: true},
		},

		// Body
		{
			Label: "Fitness", Href: "/app/fitness", Icon: "dumbbell", Group: GroupBody,
			Description: "Training, activity, and nutrition in one place.",
			Keywords:    []string{"strava", "health", "hub", "exercise"},
			Nav:         NavPlacement{Show: true},
		},
		{
			Label: "Training", Href: "/app/training", Icon: "clipboard-list", Group: GroupBody,
			Description: "AI-generated workout plans.",
			Keywords:    []string{"workout", "program", "gym", "routine", "lifting"},
		},
		{
			Label: "New training plan", Href: "/app/training/new", Icon: "clipboard-list", Group: GroupBody,
			Description: "Generate a plan from your goals.",
			Keywords:    []string{"workout", "create", "generate", "program"},
		},
		{
			Label: "Training plans", Href: "/app/training/plans", Icon: "clipboard-list", Group: GroupBody,
			Description: "Every plan you have saved.",
			Keywords:    []string{"workout", "saved", "history", "programs"},
		},
		{
			Label: "Exercises", Href: "/app/exercises", Icon: "person-standing", Group: GroupBody,
			Description: "What each movement trains, on the model.",
			Keywords:    []string{"muscles", "movement", "catalog", "anatomy"},
		},
		{
			Label: "Form check", Href: "/app/form", Icon: "video", Group: GroupBody,
			Description: "Upload a clip, get feedback.",
			Keywords:    []string{"video", "technique", "review", "upload"},
		},
		{
			Label: "Activity timer", Href: "/app/activity", Icon: "timer", Group: GroupBody,
			Description: "Track a session, see calories burned.",
			Keywords:    []string{"stopwatch", "session", "start", "calories", "track"},
		},
		{
			Label: "Calculator", Href: "/app/calculator", Icon: "calculator", Group: GroupBody,
			Description: "BMR, TDEE, and a macro target.",
			Keywords:    []string{"bmr", "tdee", "macros", "calories", "protein", "weight"},
		},
		{
			Label: "Ingredients", Href: "/app/nutrition/ingredients", Icon: "carrot", Group: GroupBody,
			Description: "Shared and your own foods.",
			Keywords:    []string{"food", "nutrition", "database", "meals"},
		},
		{
			Label: "Meal plans", Href: "/app/nutrition/plans", Icon: "utensils", Group: GroupBody,
			Description: "Build plans, track totals.",
			Keywords:    []string{"food", "diet", "nutrition", "recipes", "eating"},
		},
		{
			Label: "Food log", Href: "/app/nutrition/log", Icon: "notebook-pen", Group: GroupBody,
			Description: "Log today, see progress.",
			Keywords:    []string{"diary", "eat", "nutrition", "calories", "track"},
		},
		{
			Label: "Strava activities", Href: "/app/fitness/activities", Icon: "map", Group: GroupBody,
			Description: "Your imported runs and rides, in 3D.",
			Keywords:    []string{"strava", "runs", "rides", "routes", "map", "gps"},
		},
		{
			Label: "Care", Href: "/app/care", Icon: "heart-pulse", Group: GroupBody,
			Description: "Water, sleep, and habits.",
			Keywords:    []string{"hydration", "sleep", "habits", "reminders", "wellbeing"},
			Nav:         NavPlacement{Show: true},
		},

		// Mind
		{
			Label: "Mind", Href: "/app/mind", Icon: "brain", Group: GroupMind,
			Description: "Journal and reflect.",
			Keywords:    []string{"journal", "reflection", "writing", "thoughts"},
			Nav:         NavPlacement{Show: true},
		},
		{
			Label: "Memory", Href: "/app/memories", Icon: "sparkles", Group: GroupMind,
			Description: "What North remembers about you.",
			Keywords:    []string{"remember", "recall", "facts", "profile", "knows"},
			Nav:         NavPlacement{Show: true},
		},
		{
			Label: "Decisions", Href: "/app/decisions", Icon: "scale", Group: GroupMind,
			Description: "The choices you made, and why.",
			Keywords:    []string{"journal", "choices", "reasoning", "log"},
			Nav:         NavPlacement{Show: true},
		},

		// Progress
		{
			Label: "Goals", Href: "/app/goals", Icon: "target", Group: GroupProgress,
			Description: "What you are working towards.",
			Keywords:    []string{"objectives", "targets", "milestones", "plans"},
			Nav:         NavPlacement{Show: true},
		},
		{
			Label: "Reports", Href: "/app/reports", Icon: "notebook", Group: GroupProgress,
			Description: "Weekly reviews and daily briefings.",
			Keywords:    []string{"review", "briefing", "summary", "weekly", "daily"},
			Nav:         NavPlacement{Show: true},
		},
		{
			Label: "Insights", Href: "/app/insights/timeline", Icon: "chart-line", Group: GroupProgress,
			Description: "Your activity over time.",
			Keywords:    []string{"charts", "trends", "timeline", "analytics", "stats"},
			Nav:         NavPlacement{Show: true, SelfChildLabel: "Activity"},
		},
		{
			Label: "Body insights", Href: "/app/insights/body", Icon: "chart-line", Group: GroupProgress,
			Description: "Weight, measurements, and training load.",
			Keywords:    []string{"charts", "trends", "weight", "measurements", "analytics"},
			Nav:         NavPlacement{Show: true, Label: "Body", Under: "/app/insights/timeline"},
		},
		{
			Label: "Mind insights", Href: "/app/insights/mind", Icon: "chart-line", Group: GroupProgress,
			Description: "Mood and reflection over time.",
			Keywords:    []string{"charts", "trends", "mood", "journal", "analytics"},
			Nav:         NavPlacement{Show: true, Label: "Mind", Under: "/app/insights/timeline"},
		},
		{
			Label: "Progress insights", Href: "/app/insights/progress", Icon: "chart-line", Group: GroupProgress,
			Description: "How your goals are moving.",
			Keywords:    []string{"charts", "trends", "goals", "analytics"},
			Nav:         NavPlacement{Show: true, Label: "Progress", Under: "/app/insights/timeline"},
		},
		{
			Label: "Training insights", Href: "/app/insights/training", Icon: "chart-line", Group: GroupProgress,
			Description: "Volume, frequency, and adherence.",
			Keywords:    []string{"charts", "trends", "workouts", "volume", "analytics"},
			Nav:         NavPlacement{Show: true, Label: "Training", Under: "/app/insights/timeline"},
		},

		// System
		{
			Label: "Knowledge", Href: "/app/knowledge", Icon: "book-open", Group: GroupSystem,
			Description: "Documents and notes North can draw on.",
			Keywords:    []string{"documents", "files", "pdf", "notes", "upload", "library"},
			Nav:         NavPlacement{Show: true},
		},
		{
			Label: "Search knowledge", Href: "/app/knowledge/search", Icon: "search", Group: GroupSystem,
			Description: "Find a passage across everything you have added.",
			Keywords:    []string{"documents", "find", "passages", "lookup", "query"},
		},
		{
			Label: "Settings", Href: "/app/settings", Icon: "settings", Group: GroupSystem,
			Description: "Profile, preferences, and notifications.",
			Keywords:    []string{"account", "profile", "preferences", "timezone", "tone"},
			Nav:         NavPlacement{Show: true},
		},
		{
			Label: "Agent connections", Href: "/app/settings/connections", Icon: "plug", Group: GroupSystem,
			Description: "Tokens for agents that connect over MCP.",
			Keywords:    []string{"mcp", "api", "token", "integration", "claude", "agents"},
		},
		{
			Label: "Activity log", Href: "/app/settings/activity", Icon: "history", Group: GroupSystem,
			Description: "What happened on your account.",
			Keywords:    []string{"audit", "sessions", "security", "history", "events"},
		},
	}
}
