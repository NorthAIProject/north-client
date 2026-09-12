package layout

import (
	"context"
	"strings"

	"github.com/NorthAIProject/north-client/internal/shared/i18n"
)

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
	// Key names this destination in the message catalogue. Everything the user
	// reads about it — label, description, rail override — hangs off this stem.
	//
	// An identifier rather than the English text: see internal/shared/i18n for
	// why English-as-key breaks silently the first time the English is edited
	// for style.
	Key string

	// Label is what the palette shows, and the first thing a query matches.
	//
	// This is the English, and it stays here rather than living only in the
	// catalogue so that a caller with no request behind it — a test, the
	// worker — still reads a sentence. TestNavCatalogueMatchesTheEnglish keeps
	// the two copies honest.
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
//
// The constants are identifiers, not display text: they key the grouping and
// must not move when the language does. GroupLabel is what a person reads.
func GroupOrder() []string {
	return []string{GroupToday, GroupBody, GroupMind, GroupProgress, GroupSystem}
}

// GroupLabel is the heading a person reads for a group.
func GroupLabel(ctx context.Context, group string) string {
	return i18n.T(ctx, "nav.group."+strings.ToLower(group))
}

// LabelIn is the destination's label in the request's language.
func (d Destination) LabelIn(ctx context.Context) string {
	return d.translate(ctx, d.Key, d.Label)
}

// DescriptionIn is the destination's one-line description in the request's
// language.
func (d Destination) DescriptionIn(ctx context.Context) string {
	return d.translate(ctx, d.Key+".desc", d.Description)
}

// NavLabelIn is what the rail shows: the override when there is one, otherwise
// the label. The palette needs "Body insights" to be findable on its own; the
// rail, already under an Insights heading, only needs "Body".
func (d Destination) NavLabelIn(ctx context.Context) string {
	if d.Nav.Label == "" {
		return d.LabelIn(ctx)
	}
	return d.translate(ctx, d.Key+".nav", d.Nav.Label)
}

// SelfChildLabelIn is the child label for the one page that is also its own
// first child. Empty when the destination is not that page.
func (d Destination) SelfChildLabelIn(ctx context.Context) string {
	if d.Nav.SelfChildLabel == "" {
		return ""
	}
	return d.translate(ctx, d.Key+".self", d.Nav.SelfChildLabel)
}

// translate looks key up, falling back to this table's own English rather than
// to the key. A destination with no Key at all — one added without a catalogue
// entry — still renders its label instead of showing "nav.something" in the
// sidebar.
func (d Destination) translate(ctx context.Context, key, english string) string {
	if d.Key == "" {
		return english
	}
	if got := i18n.T(ctx, key); got != key {
		return got
	}
	return english
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
			Key: "nav.overview", Label: "Overview", Href: "/app", Icon: "layout-dashboard", Group: GroupToday,
			Description: "Where today stands.",
			Keywords:    []string{"dashboard", "home", "today", "start"},
			Nav:         NavPlacement{Show: true},
		},
		{
			Key: "nav.quick-capture", Label: "Quick capture", Href: "/app/capture", Icon: "zap", Group: GroupToday,
			Description: "Write your day in one line.",
			Keywords:    []string{"log", "add", "quick", "capture", "water", "sleep", "weight", "habit", "note"},
			Nav:         NavPlacement{Show: true},
		},
		{
			Key: "nav.check-ins", Label: "Check-ins", Href: "/app/check-ins", Icon: "smile", Group: GroupToday,
			Description: "How you are doing, in your words.",
			Keywords:    []string{"mood", "daily", "log", "feeling"},
			Nav:         NavPlacement{Show: true},
		},
		{
			Key: "nav.coach", Label: "Coach", Href: "/app/chat", Icon: "message-circle", Group: GroupToday,
			Description: "Talk it through.",
			Keywords:    []string{"chat", "ai", "ask", "assistant", "conversation"},
			Nav:         NavPlacement{Show: true},
		},

		// Body
		{
			Key: "nav.fitness", Label: "Fitness", Href: "/app/fitness", Icon: "dumbbell", Group: GroupBody,
			Description: "Training, activity, and nutrition in one place.",
			Keywords:    []string{"strava", "health", "hub", "exercise"},
			Nav:         NavPlacement{Show: true},
		},
		{
			Key: "nav.training", Label: "Training", Href: "/app/training", Icon: "clipboard-list", Group: GroupBody,
			Description: "AI-generated workout plans.",
			Keywords:    []string{"workout", "program", "gym", "routine", "lifting"},
		},
		{
			Key: "nav.new-training-plan", Label: "New training plan", Href: "/app/training/new", Icon: "clipboard-list", Group: GroupBody,
			Description: "Generate a plan from your goals.",
			Keywords:    []string{"workout", "create", "generate", "program"},
		},
		{
			Key: "nav.training-plans", Label: "Training plans", Href: "/app/training/plans", Icon: "clipboard-list", Group: GroupBody,
			Description: "Every plan you have saved.",
			Keywords:    []string{"workout", "saved", "history", "programs"},
		},
		{
			Key: "nav.exercises", Label: "Exercises", Href: "/app/exercises", Icon: "person-standing", Group: GroupBody,
			Description: "What each movement trains, on the model.",
			Keywords:    []string{"muscles", "movement", "catalog", "anatomy"},
		},
		{
			Key: "nav.form-check", Label: "Form check", Href: "/app/form", Icon: "video", Group: GroupBody,
			Description: "Upload a clip, get feedback.",
			Keywords:    []string{"video", "technique", "review", "upload"},
		},
		{
			Key: "nav.activity-timer", Label: "Activity timer", Href: "/app/activity", Icon: "timer", Group: GroupBody,
			Description: "Track a session, see calories burned.",
			Keywords:    []string{"stopwatch", "session", "start", "calories", "track"},
		},
		{
			Key: "nav.calculator", Label: "Calculator", Href: "/app/calculator", Icon: "calculator", Group: GroupBody,
			Description: "BMR, TDEE, and a macro target.",
			Keywords:    []string{"bmr", "tdee", "macros", "calories", "protein", "weight"},
		},
		{
			Key: "nav.ingredients", Label: "Ingredients", Href: "/app/nutrition/ingredients", Icon: "carrot", Group: GroupBody,
			Description: "Shared and your own foods.",
			Keywords:    []string{"food", "nutrition", "database", "meals"},
		},
		{
			Key: "nav.meal-plans", Label: "Meal plans", Href: "/app/nutrition/plans", Icon: "utensils", Group: GroupBody,
			Description: "Build plans, track totals.",
			Keywords:    []string{"food", "diet", "nutrition", "recipes", "eating"},
		},
		{
			Key: "nav.food-log", Label: "Food log", Href: "/app/nutrition/log", Icon: "notebook-pen", Group: GroupBody,
			Description: "Log today, see progress.",
			Keywords:    []string{"diary", "eat", "nutrition", "calories", "track"},
		},
		{
			Key: "nav.strava-activities", Label: "Strava activities", Href: "/app/fitness/activities", Icon: "map", Group: GroupBody,
			Description: "Your imported runs and rides, in 3D.",
			Keywords:    []string{"strava", "runs", "rides", "routes", "map", "gps"},
		},
		{
			Key: "nav.care", Label: "Care", Href: "/app/care", Icon: "heart-pulse", Group: GroupBody,
			Description: "Water, sleep, and habits.",
			Keywords:    []string{"hydration", "sleep", "habits", "reminders", "wellbeing"},
			Nav:         NavPlacement{Show: true},
		},

		// Mind
		{
			Key: "nav.mind", Label: "Mind", Href: "/app/mind", Icon: "brain", Group: GroupMind,
			Description: "Journal and reflect.",
			Keywords:    []string{"journal", "reflection", "writing", "thoughts"},
			Nav:         NavPlacement{Show: true},
		},
		{
			Key: "nav.memory", Label: "Memory", Href: "/app/memories", Icon: "sparkles", Group: GroupMind,
			Description: "What North remembers about you.",
			Keywords:    []string{"remember", "recall", "facts", "profile", "knows"},
			Nav:         NavPlacement{Show: true},
		},
		{
			Key: "nav.decisions", Label: "Decisions", Href: "/app/decisions", Icon: "scale", Group: GroupMind,
			Description: "The choices you made, and why.",
			Keywords:    []string{"journal", "choices", "reasoning", "log"},
			Nav:         NavPlacement{Show: true},
		},

		// Progress
		{
			Key: "nav.goals", Label: "Goals", Href: "/app/goals", Icon: "target", Group: GroupProgress,
			Description: "What you are working towards.",
			Keywords:    []string{"objectives", "targets", "milestones", "plans"},
			Nav:         NavPlacement{Show: true},
		},
		{
			Key: "nav.reports", Label: "Reports", Href: "/app/reports", Icon: "notebook", Group: GroupProgress,
			Description: "Weekly reviews and daily briefings.",
			Keywords:    []string{"review", "briefing", "summary", "weekly", "daily"},
			Nav:         NavPlacement{Show: true},
		},
		{
			Key: "nav.insights", Label: "Insights", Href: "/app/insights", Icon: "chart-line", Group: GroupProgress,
			Description: "How every part of it has been going.",
			Keywords:    []string{"charts", "trends", "analytics", "stats", "score", "summary"},
			Nav:         NavPlacement{Show: true, SelfChildLabel: "Summary"},
		},
		{
			Key: "nav.activity-insights", Label: "Activity feed", Href: "/app/insights/timeline", Icon: "chart-line", Group: GroupProgress,
			Description: "Everything logged, newest first.",
			Keywords:    []string{"charts", "trends", "timeline", "feed", "log", "analytics"},
			Nav:         NavPlacement{Show: true, Label: "Activity", Under: "/app/insights"},
		},
		{
			Key: "nav.body-insights", Label: "Body insights", Href: "/app/insights/body", Icon: "chart-line", Group: GroupProgress,
			Description: "Weight, measurements, and training load.",
			Keywords:    []string{"charts", "trends", "weight", "measurements", "analytics"},
			Nav:         NavPlacement{Show: true, Label: "Body", Under: "/app/insights"},
		},
		{
			Key: "nav.mind-insights", Label: "Mind insights", Href: "/app/insights/mind", Icon: "chart-line", Group: GroupProgress,
			Description: "Mood and reflection over time.",
			Keywords:    []string{"charts", "trends", "mood", "journal", "analytics"},
			Nav:         NavPlacement{Show: true, Label: "Mind", Under: "/app/insights"},
		},
		{
			Key: "nav.progress-insights", Label: "Progress insights", Href: "/app/insights/progress", Icon: "chart-line", Group: GroupProgress,
			Description: "How your goals are moving.",
			Keywords:    []string{"charts", "trends", "goals", "analytics"},
			Nav:         NavPlacement{Show: true, Label: "Progress", Under: "/app/insights"},
		},
		{
			Key: "nav.training-insights", Label: "Training insights", Href: "/app/insights/training", Icon: "chart-line", Group: GroupProgress,
			Description: "Volume, frequency, and adherence.",
			Keywords:    []string{"charts", "trends", "workouts", "volume", "analytics"},
			Nav:         NavPlacement{Show: true, Label: "Training", Under: "/app/insights"},
		},
		{
			Key: "nav.nutrition-insights", Label: "Nutrition insights", Href: "/app/insights/nutrition", Icon: "chart-line", Group: GroupProgress,
			Description: "Calories and macros against the plan.",
			Keywords:    []string{"charts", "trends", "calories", "macros", "food", "analytics"},
			Nav:         NavPlacement{Show: true, Label: "Nutrition", Under: "/app/insights"},
		},
		{
			Key: "nav.coach-insights", Label: "Coaching insights", Href: "/app/insights/coach", Icon: "chart-line", Group: GroupProgress,
			Description: "How much you and the coach talked.",
			Keywords:    []string{"charts", "trends", "chat", "messages", "coach", "analytics"},
			Nav:         NavPlacement{Show: true, Label: "Coaching", Under: "/app/insights"},
		},
		{
			Key: "nav.spend-insights", Label: "Model spend", Href: "/app/insights/spend", Icon: "chart-line", Group: GroupProgress,
			Description: "What your account spent on model calls.",
			Keywords:    []string{"charts", "cost", "tokens", "spend", "usage", "analytics"},
			Nav:         NavPlacement{Show: true, Label: "Spend", Under: "/app/insights"},
		},

		// System
		{
			Key: "nav.knowledge", Label: "Knowledge", Href: "/app/knowledge", Icon: "book-open", Group: GroupSystem,
			Description: "Documents and notes North can draw on.",
			Keywords:    []string{"documents", "files", "pdf", "notes", "upload", "library"},
			Nav:         NavPlacement{Show: true},
		},
		{
			Key: "nav.search-knowledge", Label: "Search knowledge", Href: "/app/knowledge/search", Icon: "search", Group: GroupSystem,
			Description: "Find a passage across everything you have added.",
			Keywords:    []string{"documents", "find", "passages", "lookup", "query"},
		},
		{
			Key: "nav.settings", Label: "Settings", Href: "/app/settings", Icon: "settings", Group: GroupSystem,
			Description: "Profile, preferences, and notifications.",
			Keywords:    []string{"account", "profile", "preferences", "timezone", "tone"},
			Nav:         NavPlacement{Show: true},
		},
		{
			Key: "nav.agent-connections", Label: "Agent connections", Href: "/app/settings/connections", Icon: "plug", Group: GroupSystem,
			Description: "Tokens for agents that connect over MCP.",
			Keywords:    []string{"mcp", "api", "token", "integration", "claude", "agents"},
		},
		{
			Key: "nav.activity-log", Label: "Activity log", Href: "/app/settings/activity", Icon: "history", Group: GroupSystem,
			Description: "What happened on your account.",
			Keywords:    []string{"audit", "sessions", "security", "history", "events"},
		},
	}
}
