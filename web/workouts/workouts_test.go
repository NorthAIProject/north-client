package workouts

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/a-h/templ"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/exercises/exercise"
	"github.com/NorthAIProject/north-client/internal/users"
	"github.com/NorthAIProject/north-client/internal/workouts/plan"
)

// The plan page is the screen someone reads mid-session, so it is the one that
// most needs the artwork — and the one where its absence went unnoticed while
// the catalog pages had it.

func renderPlan(t *testing.T, p plan.Plan) string {
	t.Helper()
	var b strings.Builder
	page := PlanPage(users.User{DisplayName: "Ada"}, PlanView{
		ID:        uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		Plan:      p,
		CreatedAt: time.Date(2026, 8, 27, 0, 0, 0, 0, time.UTC),
	})
	if err := page.Render(context.Background(), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	return b.String()
}

func planWith(exercises ...plan.Exercise) plan.Plan {
	return plan.Plan{
		Name:       "Four weeks of dumbbells",
		Rationale:  "Because you said you have dumbbells.",
		WeeksTotal: 4,
		Days: []plan.PlanDay{{
			Weekday:   "Monday",
			Focus:     "lower body",
			Exercises: exercises,
		}},
	}
}

func gobletSquat() plan.Exercise {
	return plan.Exercise{
		Name:             "Dumbbell Goblet Squat",
		Sets:             3,
		Reps:             "8-12",
		RestSeconds:      90,
		Equipment:        "dumbbell",
		FormCues:         "Knees out.",
		CatalogSlug:      "dumbbell-goblet-squat",
		IllustrationSlug: "goblet-squat",
		Primary:          []string{"quads"},
		Secondary:        []string{"glutes"},
	}
}

func improvised() plan.Exercise {
	return plan.Exercise{
		Name:        "Something the model made up",
		Sets:        3,
		Reps:        "10",
		RestSeconds: 60,
		Equipment:   "dumbbell",
		FormCues:    "Careful.",
		Primary:     []string{"chest"},
	}
}

func TestPlanPageShowsTheIllustrationForACataloguedExercise(t *testing.T) {
	t.Parallel()

	html := renderPlan(t, planWith(gobletSquat()))

	for _, want := range []string{
		"/assets/exercises/goblet-squat/frame-1.svg",
		"/assets/exercises/goblet-squat/frame-3.svg",
		"mask-image",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("plan page is missing %q", want)
		}
	}
	if strings.Contains(html, ".svg.gz") {
		t.Error("markup asks for .svg.gz; the storage format must not reach the template")
	}
}

// An exercise the model improvised has no catalog row, so no artwork. The row
// still has to render everything else.
func TestPlanPageRendersAnImprovisedExerciseWithoutArtwork(t *testing.T) {
	t.Parallel()

	html := renderPlan(t, planWith(improvised()))

	if strings.Contains(html, "/assets/exercises/") {
		t.Error("an exercise with no illustration slug still rendered artwork")
	}
	if !strings.Contains(html, "Something the model made up") {
		t.Error("the exercise row itself did not render")
	}
}

// CC BY-SA 4.0 requires visible credit wherever the artwork appears.
func TestPlanPageCreditsTheArtworkOnlyWhenItIsShown(t *testing.T) {
	t.Parallel()

	const licence = "creativecommons.org/licenses/by-sa/4.0"

	if html := renderPlan(t, planWith(gobletSquat())); !strings.Contains(html, licence) {
		t.Error("plan page shows artwork with no licence credit")
	}
	if html := renderPlan(t, planWith(improvised())); strings.Contains(html, licence) {
		t.Error("plan page credited artwork that is not on it")
	}
}

// A plan mixes catalogued and improvised exercises freely; one illustrated
// exercise is enough to owe the credit, and must not suppress the other rows.
func TestAMixedPlanRendersBothKindsOfRow(t *testing.T) {
	t.Parallel()

	html := renderPlan(t, planWith(gobletSquat(), improvised()))

	if !strings.Contains(html, "/assets/exercises/goblet-squat/frame-1.svg") {
		t.Error("the catalogued exercise lost its artwork")
	}
	if !strings.Contains(html, "Something the model made up") {
		t.Error("the improvised exercise stopped rendering")
	}
	if !strings.Contains(html, "creativecommons.org/licenses/by-sa/4.0") {
		t.Error("one illustrated exercise is enough to owe the credit")
	}
}

// The muscle dialog is keyed by an id derived from the exercise's position, and
// the illustration reuses it. Two exercises sharing an Alpine id would cycle as
// one illustration.
func TestEveryIllustratedRowGetsItsOwnComponentID(t *testing.T) {
	t.Parallel()

	second := gobletSquat()
	second.Name = "Dumbbell Split Squat"
	second.IllustrationSlug = "dumbbell-split-squat"

	html := renderPlan(t, planWith(gobletSquat(), second))

	// Both frames sets must be present, and under distinct element ids.
	if !strings.Contains(html, "/assets/exercises/goblet-squat/frame-1.svg") ||
		!strings.Contains(html, "/assets/exercises/dumbbell-split-squat/frame-1.svg") {
		t.Fatal("one of the two illustrations did not render")
	}
	if count := strings.Count(html, `-art"`); count != 2 {
		t.Errorf("found %d illustration element ids, want 2 distinct ones", count)
	}
}

func planView(p plan.Plan) PlanView {
	return PlanView{
		ID:        uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		Plan:      p,
		CreatedAt: time.Date(2026, 8, 27, 0, 0, 0, 0, time.UTC),
	}
}

func render(t *testing.T, c templ.Component) string {
	t.Helper()
	var b strings.Builder
	if err := c.Render(context.Background(), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	return b.String()
}

// Every edit URL is built from the plan's id, and the day card is the swap
// target. Getting either wrong makes the buttons silently do nothing.
func TestExerciseRowLinksItsSwapButtonAtTheDayCard(t *testing.T) {
	t.Parallel()

	html := renderPlan(t, planWith(gobletSquat()))

	if !strings.Contains(html, `id="plan-day-0"`) {
		t.Error("the day card has no id for HTMX to target")
	}
	want := "/app/training/11111111-1111-1111-1111-111111111111/days/0/exercises/0/swap"
	if !strings.Contains(html, want) {
		t.Errorf("swap button does not point at %q", want)
	}
	if !strings.Contains(html, `hx-target="#plan-day-0"`) {
		t.Error("swap button does not target the day card")
	}
}

func TestSwapPickerOffersSuggestionsAndCanBeCancelled(t *testing.T) {
	t.Parallel()

	v := planView(planWith(gobletSquat()))
	picker := SwapPicker(v, 0, 0, "Dumbbell Goblet Squat")
	picker.Suggestions = []exercise.Exercise{{
		Slug: "dumbbell-front-squat", Name: "Dumbbell Front Squat",
		Equipment: "dumbbell", Primary: []string{"quads"},
	}}

	html := render(t, DayCardSwapping(v, 0, v.Plan.Days[0], 0, picker))

	if !strings.Contains(html, "Dumbbell Front Squat") {
		t.Error("the suggestion did not render")
	}
	// The slug travels as a form field rather than hx-vals, so it can carry the
	// CSRF token with it — see TestThePickerOptionPostsAsAFormWithTheSlugAndToken.
	if !strings.Contains(html, `name="catalog_slug" value="dumbbell-front-squat"`) {
		t.Error("the suggestion does not post a catalog slug")
	}
	if !strings.Contains(html, `hx-post="/app/training/11111111-1111-1111-1111-111111111111/days/0/exercises/0/swap"`) {
		t.Error("the suggestion does not post to the swap endpoint")
	}
	// Cancel has to put the original row back without a page load.
	if !strings.Contains(html, "/app/training/11111111-1111-1111-1111-111111111111/days/0\"") {
		t.Error("Cancel does not fetch the plain day card")
	}
	// The prescription is what a swap preserves, and the panel should say so.
	if !strings.Contains(html, "Sets, reps and rest stay as they are") {
		t.Error("the panel does not explain what a swap keeps")
	}
}

func TestSwapPickerSaysWhenASearchMatchedNothing(t *testing.T) {
	t.Parallel()

	v := planView(planWith(gobletSquat()))
	picker := SwapPicker(v, 0, 0, "Dumbbell Goblet Squat")
	picker.Query = "zzz"
	html := render(t, DayCardSwapping(v, 0, v.Plan.Days[0], 0, picker))

	if !strings.Contains(html, "Nothing matched") {
		t.Error("an empty search renders no explanation")
	}
}

// The picker replaces only the exercise being swapped; the rest of the day has
// to stay on screen, or the page looks like it lost the session.
func TestSwapPickerLeavesTheOtherExercisesVisible(t *testing.T) {
	t.Parallel()

	v := planView(planWith(gobletSquat(), improvised()))
	html := render(t, DayCardSwapping(v, 0, v.Plan.Days[0], 0, SwapPicker(v, 0, 0, "Dumbbell Goblet Squat")))

	if !strings.Contains(html, "Something the model made up") {
		t.Error("the other exercise disappeared while swapping")
	}
	// The swapped slot became the picker, which names the lift it is replacing.
	// Its prescription is what disappears — that is the row, not the panel.
	if !strings.Contains(html, "Replacing") {
		t.Error("the exercise being swapped did not become the picker")
	}
	if strings.Contains(html, "3×8-12") {
		t.Error("the replaced exercise still renders its prescription row")
	}
}

// A swap can bring the page its first illustrated exercise, so the licence
// credit has to arrive with the edit rather than at the next full page load.
func TestTheEditResponseCarriesTheCredit(t *testing.T) {
	t.Parallel()

	v := planView(planWith(gobletSquat()))
	if html := render(t, PlanBody(v)); !strings.Contains(html, "creativecommons.org/licenses/by-sa/4.0") {
		t.Error("artwork was swapped in without its credit")
	}

	// And still only when there is artwork to credit.
	plain := planView(planWith(improvised()))
	if html := render(t, PlanBody(plain)); strings.Contains(html, "creativecommons.org/licenses/by-sa/4.0") {
		t.Error("credited artwork that is not on the page")
	}
}

// The body is the swap target, so it has to carry the id HTMX aims at.
func TestPlanBodyIsAddressableAsTheEditTarget(t *testing.T) {
	t.Parallel()

	html := render(t, PlanBody(planView(planWith(gobletSquat()))))
	if !strings.Contains(html, `id="plan-body"`) {
		t.Error("the edit target has no id")
	}
}

// Validation problems are shown, not enforced. The page must say what no longer
// matches without implying the edit was rejected.
func TestValidationProblemsRenderAsANoticeRatherThanBlockingTheEdit(t *testing.T) {
	t.Parallel()

	v := planView(planWith(gobletSquat()))
	v.Problems = []string{`"Barbell Back Squat" needs barbell, which they do not have`}

	html := render(t, PlanPage(users.User{DisplayName: "Ada"}, v))

	if !strings.Contains(html, "no longer matches your intake") {
		t.Error("the problem was not surfaced")
	}
	if !strings.Contains(html, "needs barbell") {
		t.Error("the problem text was not rendered")
	}
	// The plan itself must still be on the page.
	if !strings.Contains(html, "Dumbbell Goblet Squat") {
		t.Error("the plan stopped rendering when it had problems")
	}
}

// Regression: an edit produces a new plan, so every day card on the page — not
// just the edited one — has to be re-rendered with the new id.
//
// Swapping one day card left the others carrying the superseded id, so the
// second swap on a different day was refused, and the refusal re-rendered the
// same stale id, which meant it was refused forever.
func TestTheEditResponseRefreshesEveryDaySoNoneKeepAStalePlanID(t *testing.T) {
	t.Parallel()

	p := plan.Plan{
		Name: "Two days", WeeksTotal: 4,
		Days: []plan.PlanDay{
			{Weekday: "Monday", Focus: "lower", Exercises: []plan.Exercise{gobletSquat()}},
			{Weekday: "Thursday", Focus: "upper", Exercises: []plan.Exercise{improvised()}},
		},
	}
	v := planView(p)

	html := render(t, PlanBody(v))

	// Both days must carry the id the edit just produced.
	for _, day := range []string{"days/0", "days/1"} {
		want := "/app/training/11111111-1111-1111-1111-111111111111/" + day
		if !strings.Contains(html, want) {
			t.Errorf("the response does not re-render %s with the current plan id", day)
		}
	}
	if !strings.Contains(html, `id="plan-day-1"`) {
		t.Error("the untouched day was not re-rendered, so it keeps the superseded id")
	}
}

// Regression: an edit can introduce a validation problem, and the notice lives
// outside the day cards. Swapping only a day card could never show it.
func TestTheEditResponseCarriesTheValidationNotice(t *testing.T) {
	t.Parallel()

	v := planView(planWith(gobletSquat()))
	v.Problems = []string{`"Barbell Back Squat" needs barbell, which they do not have`}

	html := render(t, PlanBody(v))

	if !strings.Contains(html, "no longer matches your intake") {
		t.Error("an edit that broke the intake would show no notice")
	}
	if !strings.Contains(html, "needs barbell") {
		t.Error("the problem text is missing from the edit response")
	}
}

// Adding and swapping share one panel; only the heading and where it posts
// differ. These pin that the configuration actually reaches the markup.
func TestAddPickerPostsToTheDaysExerciseCollection(t *testing.T) {
	t.Parallel()

	v := planView(planWith(gobletSquat()))
	picker := AddPicker(v, 0, "Monday")
	picker.Suggestions = []exercise.Exercise{{
		Slug: "dumbbell-curl", Name: "Dumbbell Curl", Equipment: "dumbbell",
	}}

	html := render(t, DayCardAdding(v, 0, v.Plan.Days[0], picker))

	if !strings.Contains(html, "Add to Monday") {
		t.Error("the panel does not say which day it adds to")
	}
	if !strings.Contains(html, `hx-post="/app/training/11111111-1111-1111-1111-111111111111/days/0/exercises"`) {
		t.Error("the option does not post to the day's exercise collection")
	}
	// The prescription a new exercise starts at is a choice, so it is stated.
	if !strings.Contains(html, "3×8-12") {
		t.Error("the panel does not say what the new exercise starts at")
	}
	// The existing exercises stay on screen while adding.
	if !strings.Contains(html, "Dumbbell Goblet Squat") {
		t.Error("the day's existing exercises disappeared while adding")
	}
}

func TestEveryExerciseOffersRemoveWithAConfirmation(t *testing.T) {
	t.Parallel()

	html := renderPlan(t, planWith(gobletSquat()))

	if !strings.Contains(html, `hx-post="/app/training/11111111-1111-1111-1111-111111111111/days/0/exercises/0/remove"`) {
		t.Error("no remove control on the exercise")
	}
	// There is no undo in the interface, so removal is confirmed.
	if !strings.Contains(html, "hx-confirm") {
		t.Error("remove is not confirmed despite there being no undo")
	}
}

func TestEveryDayOffersAddingAnExercise(t *testing.T) {
	t.Parallel()

	html := renderPlan(t, planWith(gobletSquat()))

	if !strings.Contains(html, `/app/training/11111111-1111-1111-1111-111111111111/days/0/add`) {
		t.Error("no way to add an exercise to the day")
	}
}

// Removing the last exercise leaves an empty day, which is allowed. The card
// has to say so rather than rendering as though it had failed to load.
func TestAnEmptyDayExplainsItselfAndStillOffersAdding(t *testing.T) {
	t.Parallel()

	empty := plan.Plan{
		Name: "Two days", WeeksTotal: 4,
		Days: []plan.PlanDay{{Weekday: "Monday", Focus: "lower", Exercises: nil}},
	}

	html := render(t, PlanBody(planView(empty)))

	if !strings.Contains(html, "Nothing in this session yet") {
		t.Error("an emptied day renders as though it were broken")
	}
	if !strings.Contains(html, "/days/0/add") {
		t.Error("an emptied day offers no way to put anything back")
	}
}

// Order is training information, so the controls have to exist — and the ends
// must not offer a move that would go nowhere.
func TestReorderControlsAppearOnlyWhereAMoveIsPossible(t *testing.T) {
	t.Parallel()

	first := gobletSquat()
	second := improvised()
	html := renderPlan(t, planWith(first, second))

	if !strings.Contains(html, `/days/0/exercises/0/move`) {
		t.Error("the first exercise cannot be moved at all")
	}
	if !strings.Contains(html, `/days/0/exercises/1/move`) {
		t.Error("the second exercise cannot be moved at all")
	}
	// Two exercises: one "up" (on index 1) and one "down" (on index 0). The
	// direction is a hidden form field rather than hx-vals, so that the control
	// can carry the CSRF token.
	if got := strings.Count(html, `name="direction" value="up"`); got != 1 {
		t.Errorf("found %d up controls, want 1 — the first exercise has nothing above it", got)
	}
	if got := strings.Count(html, `name="direction" value="down"`); got != 1 {
		t.Errorf("found %d down controls, want 1 — the last exercise has nothing below it", got)
	}
}

func TestASingleExerciseDayOffersNoReordering(t *testing.T) {
	t.Parallel()

	html := renderPlan(t, planWith(gobletSquat()))
	if strings.Contains(html, "/move") {
		t.Error("a day with one exercise offers a move that could go nowhere")
	}
}

// The prescription is editable in place. Expanded-or-collapsed is local UI
// state; the edit itself posts to the server.
func TestThePrescriptionIsEditableInPlace(t *testing.T) {
	t.Parallel()

	html := renderPlan(t, planWith(gobletSquat()))

	if !strings.Contains(html, `hx-post="/app/training/11111111-1111-1111-1111-111111111111/days/0/exercises/0/sets"`) {
		t.Error("no form posts a new prescription")
	}
	for _, field := range []string{`name="sets"`, `name="reps"`, `name="rest_seconds"`} {
		if !strings.Contains(html, field) {
			t.Errorf("the prescription form has no %s", field)
		}
	}
	// Collapsed until asked for, and cloaked so it does not flash open.
	if !strings.Contains(html, "x-cloak") {
		t.Error("the form is not cloaked, so it flashes open before Alpine runs")
	}
	// The current values are what the form starts from.
	if !strings.Contains(html, `value="90"`) {
		t.Error("the form does not start from the current rest")
	}
}

func summaries() []PlanSummary {
	return []PlanSummary{
		{
			ID:    uuid.MustParse("22222222-2222-2222-2222-222222222222"),
			Name:  "Four weeks of dumbbells",
			Weeks: 4, Days: 3,
			CreatedAt: time.Date(2026, 8, 27, 0, 0, 0, 0, time.UTC),
		},
		{
			ID:    uuid.MustParse("33333333-3333-3333-3333-333333333333"),
			Name:  "Six weeks of barbell",
			Weeks: 6, Days: 4,
			CreatedAt: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		},
	}
}

// The list exists because generating a second plan used to make the first
// unreachable. Every row being a working link is the whole point.
func TestPlansPageLinksEveryPlan(t *testing.T) {
	t.Parallel()

	html := render(t, PlansPage(users.User{DisplayName: "Ada"}, summaries()))

	for _, want := range []string{
		`href="/app/training/22222222-2222-2222-2222-222222222222"`,
		`href="/app/training/33333333-3333-3333-3333-333333333333"`,
		"Four weeks of dumbbells",
		"Six weeks of barbell",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("the plans list is missing %q", want)
		}
	}
}

// Rows arrive most-recently-touched first, so the first is the plan
// /app/training resolves to. Saying so is what makes it a list of plans with a
// live one rather than an undifferentiated pile.
func TestPlansPageMarksOnlyTheLivePlan(t *testing.T) {
	t.Parallel()

	html := render(t, PlansPage(users.User{DisplayName: "Ada"}, summaries()))

	if got := strings.Count(html, "Following"); got != 1 {
		t.Errorf("found %d plans marked as being followed, want exactly 1", got)
	}
	// It has to be the first row, not just any one of them.
	first := strings.Index(html, "Four weeks of dumbbells")
	second := strings.Index(html, "Six weeks of barbell")
	marker := strings.Index(html, "Following")
	if first >= marker || marker >= second {
		t.Error("the marker is not on the first plan")
	}
}

func TestPlansPageShowsEachPlansShape(t *testing.T) {
	t.Parallel()

	html := render(t, PlansPage(users.User{DisplayName: "Ada"}, summaries()))

	if !strings.Contains(html, "4 weeks · 3/week") {
		t.Error("the first plan's shape is missing")
	}
	if !strings.Contains(html, "27 Aug 2026") {
		t.Error("the first plan's date is missing")
	}
}

// A page nothing links to may as well not exist.
func TestThePlanPageLinksToTheList(t *testing.T) {
	t.Parallel()

	html := renderPlan(t, planWith(gobletSquat()))
	if !strings.Contains(html, `href="/app/training/plans"`) {
		t.Error("the plan page does not reach the plans list")
	}
}

// Regression: every state-changing control must carry the CSRF token.
//
// This is the bug the render tests could not see. The routes require the token
// — cmd/web proves that — and the markup existed, but the controls were bare
// hx-post buttons with no hidden field, so every edit was answered 403 in a
// real browser. Service tests bypass HTTP; the cmd/web test set the header by
// hand. Nothing connected the two until someone clicked Swap.
//
// A safe method needs no token, which is why the Swap and Add controls that
// only open a panel are hx-get and are not counted here.
func TestEveryStateChangingControlCarriesTheCSRFToken(t *testing.T) {
	t.Parallel()

	p := plan.Plan{
		Name: "Two days", WeeksTotal: 4,
		Days: []plan.PlanDay{{
			Weekday: "Monday", Focus: "lower",
			// Two exercises, so a move control exists in both directions.
			Exercises: []plan.Exercise{gobletSquat(), improvised()},
		}},
	}

	html := render(t, PlanBody(planView(p)))

	posts := strings.Count(html, "hx-post=")
	tokens := strings.Count(html, `name="csrf_token"`)

	if posts == 0 {
		t.Fatal("no state-changing controls rendered; this test is not checking anything")
	}
	if tokens < posts {
		t.Errorf("%d hx-post controls but only %d csrf_token fields — the shortfall is answered 403", posts, tokens)
	}
}

// The picker option is the control that was broken. It has to post the slug as
// a form field now rather than through hx-vals, so the token can ride with it.
func TestThePickerOptionPostsAsAFormWithTheSlugAndToken(t *testing.T) {
	t.Parallel()

	v := planView(planWith(gobletSquat()))
	picker := SwapPicker(v, 0, 0, "Dumbbell Goblet Squat")
	picker.Suggestions = []exercise.Exercise{{
		Slug: "dumbbell-front-squat", Name: "Dumbbell Front Squat", Equipment: "dumbbell",
	}}

	html := render(t, DayCardSwapping(v, 0, v.Plan.Days[0], 0, picker))

	if !strings.Contains(html, `<input type="hidden" name="catalog_slug" value="dumbbell-front-squat">`) {
		t.Error("the slug is not a form field, so it cannot travel with the token")
	}
	if strings.Contains(html, "hx-vals") {
		t.Error("hx-vals is still used; a bare hx-post cannot carry the CSRF field")
	}
	if !strings.Contains(html, `name="csrf_token"`) {
		t.Error("the picker option carries no CSRF token")
	}
}

// Progressive enhancement falls out of using forms: each control names its
// endpoint in action, so it still works if HTMX has not loaded.
func TestStateChangingControlsWorkWithoutJavaScript(t *testing.T) {
	t.Parallel()

	html := renderPlan(t, planWith(gobletSquat(), improvised()))

	for _, want := range []string{
		`action="/app/training/11111111-1111-1111-1111-111111111111/days/0/exercises/0/remove"`,
		`action="/app/training/11111111-1111-1111-1111-111111111111/days/0/exercises/0/move"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("missing no-JS fallback: %s", want)
		}
	}
}
