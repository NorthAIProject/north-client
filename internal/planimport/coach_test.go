package planimport_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/calculator"
	"github.com/NorthAIProject/north-client/internal/meals"
	"github.com/NorthAIProject/north-client/internal/meals/meal"
	"github.com/NorthAIProject/north-client/internal/planimport"
	"github.com/NorthAIProject/north-client/internal/quota"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

// countingQuota stands in for the quota service: it counts what was spent and
// refuses when told to.
type countingQuota struct {
	spent  int
	refuse bool
}

func (q *countingQuota) Consume(_ context.Context, _ uuid.UUID, _ string, action quota.Action) (quota.Decision, error) {
	if action != quota.PlanImport {
		return quota.Decision{}, fmt.Errorf("spent %q, want %q", action, quota.PlanImport)
	}
	q.spent++
	if q.refuse {
		return quota.Decision{Allowed: false, RetryAfter: 20 * time.Minute}, nil
	}
	return quota.Decision{Allowed: true}, nil
}

// stubReader answers every meal read with the same reading and remembers the
// hint it was given. No model is involved.
type stubReader struct {
	meal  planimport.MealReading
	hint  string
	reads int
	// started and release, when set, hold the read open until the test
	// lets it finish.
	started, release chan struct{}
}

func (r *stubReader) ReadWorkout(context.Context, users.User, planimport.Source, string) (string, []planimport.WorkoutRow, []string, error) {
	return "", nil, nil, errors.New("stubReader reads meals only")
}

func (r *stubReader) ReadMeal(_ context.Context, _ users.User, _ planimport.Source, hint string) (planimport.MealReading, error) {
	r.hint = hint
	r.reads++
	if r.started != nil {
		close(r.started)
		<-r.release
	}
	return r.meal, nil
}

type noGoal struct{}

func (noGoal) Current(context.Context, uuid.UUID) (calculator.MacroPlan, error) {
	return calculator.MacroPlan{}, apperr.ErrNotFound
}

// coachService is the fixture's service with a reader and a quota wired in.
func (f fixture) coachService(mealPlans *meals.MealPlanService, reader planimport.Reader, q planimport.QuotaConsumer) *planimport.Service {
	if mealPlans == nil {
		mealPlans = f.mealPlans
	}
	return planimport.NewService(planimport.Options{
		Reader:      reader,
		Workouts:    f.workouts,
		MealPlans:   mealPlans,
		Ingredients: f.ingredients,
		Quota:       q,
	})
}

func (f fixture) addFoods(t *testing.T) {
	t.Helper()
	for _, in := range []meals.IngredientInput{
		{Name: "Grilled chicken breast", Category: meals.CategoryProtein, Per100g: meals.Macros{Calories: 165, ProteinG: 31, FatG: 3.6}},
		{Name: "Plain white rice", Category: meals.CategoryCarb, Per100g: meals.Macros{Calories: 130, ProteinG: 2.7, FatG: 0.3, CarbG: 28}},
	} {
		if _, err := f.ingredients.Create(context.Background(), f.user.ID, in); err != nil {
			t.Fatal(err)
		}
	}
}

func TestImportForCoachSavesWhatMatchesAndReportsTheRest(t *testing.T) {
	f := newFixture(t)
	f.addFoods(t)
	ctx := context.Background()
	q := &countingQuota{}
	svc := f.coachService(nil, nil, q)

	// Monday's lunch loses option A, so B becomes the one counted and keeps
	// its label. Monday's dinner and all of Tuesday match nothing. Wednesday
	// is 140 g of carbs: over a mid-carb day's 71 g, and saved anyway because
	// the approval card was the confirmation.
	csv := "Day,Meal,Option,Food,Quantity,Unit,Protein,Carbs,Fat\n" +
		"Monday,Lunch,A,Zzqx stew,1,bowl,,,\n" +
		"Monday,Lunch,B,Grilled chicken breast,150,g,,,\n" +
		"Monday,Lunch,C,Plain white rice,100,g,,,\n" +
		"Monday,Dinner,,Qqzx pie,200,g,,,\n" +
		"Tuesday,Snack,,Xqzz jam,1,jar,,,\n" +
		"Wednesday,Lunch,,Plain white rice,500,g,,,\n"

	res, err := svc.ImportForCoach(ctx, f.user, "week.csv", []byte(csv), planimport.CoachImport{Kind: planimport.CoachImportMeal})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if q.spent != 1 {
		t.Fatalf("quota spent %d times, want once", q.spent)
	}
	if res.MealPlan == nil || res.Workout != nil {
		t.Fatalf("result = %+v, want a meal plan only", res)
	}

	p := res.MealPlan
	if p.Settings.Type != meal.MidCarb || p.Settings.Mode != meal.Advanced {
		t.Fatalf("settings = %+v, want mid carb, advanced", p.Settings)
	}
	if len(p.Days) != 2 || p.Days[0].Weekday != time.Monday || p.Days[1].Weekday != time.Wednesday {
		t.Fatalf("days = %+v, want Monday and Wednesday", p.Days)
	}
	mon := p.Days[0]
	if len(mon.Meals) != 1 {
		t.Fatalf("Monday meals = %+v, want lunch alone", mon.Meals)
	}
	lunch := mon.Meals[0]
	if lunch.OptionLabel != "B" || len(lunch.Alternatives) != 1 || lunch.Alternatives[0].OptionLabel != "C" {
		t.Fatalf("lunch = %+v, want B counted and C its one alternative", lunch)
	}

	if len(res.Skipped) != 3 {
		t.Fatalf("skipped = %q, want three lines", res.Skipped)
	}
	// Each is the line as the file wrote it, then why it was left out.
	for i, line := range []string{"1 bowl Zzqx stew: ", "200 g Qqzx pie: ", "1 jar Xqzz jam: "} {
		if !strings.HasPrefix(res.Skipped[i], line) || !strings.Contains(res.Skipped[i], "Nothing in the catalog matches") {
			t.Fatalf("skipped[%d] = %q, want %q and why", i, res.Skipped[i], line)
		}
	}
	if len(res.Over) != 1 || !strings.HasPrefix(res.Over[0], "Wednesday: carbs over by") {
		t.Fatalf("over = %q, want Wednesday's carbs", res.Over)
	}
}

func TestImportForCoachReadsAnEveryDayPlanWithTheHintAndSavesSevenDays(t *testing.T) {
	f := newFixture(t)
	f.addFoods(t)
	ctx := context.Background()
	reader := &stubReader{meal: planimport.MealReading{
		Name:     "Cut",
		Rows:     []planimport.MealRow{{Meal: "Lunch", Food: "Grilled chicken breast", Quantity: "150", Unit: "g"}},
		Unparsed: []string{"Drink water"},
		Notes:    "Eat slowly.",
	}}
	svc := f.coachService(nil, reader, nil)

	res, err := svc.ImportForCoach(ctx, f.user, "plan.txt", []byte("Lunch: 150 g frango"), planimport.CoachImport{
		Kind: planimport.CoachImportMeal, PlanType: string(meal.LowCarb), Hint: "only the lunch",
	})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if reader.reads != 1 || reader.hint != "only the lunch" {
		t.Fatalf("reader reads = %d with hint %q, want one read with the person's hint", reader.reads, reader.hint)
	}
	p := res.MealPlan
	if p == nil || len(p.Days) != 7 || p.Settings.Type != meal.LowCarb {
		t.Fatalf("plan = %+v, want seven low-carb days", p)
	}
	for i, d := range p.Days {
		if d.Weekday != meal.WeekOrder[i] {
			t.Fatalf("day %d = %s, want %s", i, d.Weekday, meal.WeekOrder[i])
		}
	}
	if res.Notes != "Eat slowly." || len(res.Unparsed) != 1 || res.Unparsed[0] != "Drink water" {
		t.Fatalf("notes = %q, unparsed = %q", res.Notes, res.Unparsed)
	}
}

func TestImportForCoachRefusesAMealPlanWithoutATarget(t *testing.T) {
	f := newFixture(t)
	f.addFoods(t)
	ctx := context.Background()
	q := &countingQuota{}
	svc := f.coachService(meals.NewMealPlanService(f.repo, noGoal{}), nil, q)

	csv := "Day,Meal,Food,Quantity,Unit\nMonday,Lunch,Grilled chicken breast,150,g\n"
	_, err := svc.ImportForCoach(ctx, f.user, "week.csv", []byte(csv), planimport.CoachImport{Kind: planimport.CoachImportMeal})
	if !apperr.Is(err, apperr.ErrValidation) || !strings.Contains(err.Error(), "macro target") {
		t.Fatalf("err = %v, want a validation error about the macro target", err)
	}
	if q.spent != 0 {
		t.Fatalf("a refused import spent the quota")
	}
	if plans, _ := f.mealPlans.ListPlans(ctx, f.user.ID); len(plans) != 0 {
		t.Fatalf("a refused import stored a plan")
	}
}

func TestImportForCoachRefusesAFileWithNothingToSave(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	svc := f.coachService(nil, nil, nil)

	csv := "Day,Meal,Food,Quantity,Unit\nMonday,Lunch,Zzqx stew,1,bowl\n"
	_, err := svc.ImportForCoach(ctx, f.user, "week.csv", []byte(csv), planimport.CoachImport{Kind: planimport.CoachImportMeal})
	if !apperr.Is(err, apperr.ErrValidation) || !strings.Contains(err.Error(), "Nothing in week.csv could be matched to foods") {
		t.Fatalf("err = %v, want nothing-matched", err)
	}
	if plans, _ := f.mealPlans.ListPlans(ctx, f.user.ID); len(plans) != 0 {
		t.Fatalf("an empty import stored a plan")
	}
}

func TestImportForCoachStopsWhenTheQuotaIsSpent(t *testing.T) {
	f := newFixture(t)
	reader := &stubReader{}
	svc := f.coachService(nil, reader, &countingQuota{refuse: true})

	_, err := svc.ImportForCoach(context.Background(), f.user, "plan.txt", []byte("Lunch"), planimport.CoachImport{Kind: planimport.CoachImportMeal})
	if !errors.Is(err, planimport.ErrQuotaUsed) {
		t.Fatalf("err = %v, want ErrQuotaUsed", err)
	}
	if reader.reads != 0 {
		t.Fatalf("a refused import still read the file")
	}
	// The refusal let go of the person's import slot: the next try is
	// refused for the quota again, not as busy.
	if _, err := svc.ImportForCoach(context.Background(), f.user, "plan.txt", []byte("Lunch"), planimport.CoachImport{Kind: planimport.CoachImportMeal}); !errors.Is(err, planimport.ErrQuotaUsed) {
		t.Fatalf("second try: err = %v, want ErrQuotaUsed", err)
	}
}

func TestImportForCoachRefusesAnUnknownKindOrPlanType(t *testing.T) {
	f := newFixture(t)
	q := &countingQuota{}
	svc := f.coachService(nil, nil, q)
	csv := []byte("Day,Meal,Food,Quantity,Unit\nMonday,Lunch,Grilled chicken breast,150,g\n")

	for _, req := range []planimport.CoachImport{{Kind: "recipe"}, {Kind: planimport.CoachImportMeal, PlanType: "keto-ish"}} {
		if _, err := svc.ImportForCoach(context.Background(), f.user, "week.csv", csv, req); !apperr.Is(err, apperr.ErrValidation) {
			t.Fatalf("%+v: err = %v, want validation", req, err)
		}
	}
	if q.spent != 0 {
		t.Fatalf("a refused request spent the quota")
	}
}

func TestImportForCoachSpreadsWorkoutDaysOverTheWeek(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	q := &countingQuota{}
	svc := f.coachService(nil, nil, q)

	csv := "Day,Exercise,Sets,Reps\n" +
		"Day 1,Back Squat,3,5\n" +
		"Day 2,Bench Press,3,5\n" +
		"Day 3,Deadlift,1,5\n"

	res, err := svc.ImportForCoach(ctx, f.user, "block.csv", []byte(csv), planimport.CoachImport{Kind: planimport.CoachImportWorkout})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if q.spent != 1 || res.Workout == nil || res.MealPlan != nil {
		t.Fatalf("spent = %d, result = %+v", q.spent, res)
	}
	if got := workoutWeekdays(res); got != "Monday,Wednesday,Friday" {
		t.Fatalf("weekdays = %s, want Monday,Wednesday,Friday", got)
	}

	// Preferred days are used in order; a day the file names keeps it.
	csv = "Day,Exercise,Sets,Reps\n" +
		"Day 1,Back Squat,3,5\n" +
		"Thursday,Bench Press,3,5\n" +
		"Day 3,Deadlift,1,5\n"
	res, err = svc.ImportForCoach(ctx, f.user, "block.csv", []byte(csv), planimport.CoachImport{
		Kind: planimport.CoachImportWorkout, Weekdays: []time.Weekday{time.Thursday, time.Tuesday, time.Saturday},
	})
	if err != nil {
		t.Fatalf("import with preferred days: %v", err)
	}
	if got := workoutWeekdays(res); got != "Tuesday,Thursday,Saturday" {
		t.Fatalf("weekdays = %s, want Tuesday,Thursday,Saturday", got)
	}
}

func workoutWeekdays(res planimport.CoachResult) string {
	var days []string
	for _, d := range res.Workout.Plan.Days {
		days = append(days, d.Weekday)
	}
	return strings.Join(days, ",")
}

func TestImportForCoachSpendsNothingOnARefusedFileOrWhileBusy(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	q := &countingQuota{}
	reader := &stubReader{started: make(chan struct{}), release: make(chan struct{})}
	svc := f.coachService(nil, reader, q)
	req := planimport.CoachImport{Kind: planimport.CoachImportMeal}

	if _, err := svc.ImportForCoach(ctx, f.user, "plan.exe", []byte("MZ"), req); planimport.ReasonOf(err) != planimport.ReasonUnsupported {
		t.Fatalf("unsupported file: err = %v", err)
	}
	if q.spent != 0 {
		t.Fatalf("an unsupported file spent the quota")
	}

	done := make(chan error)
	go func() {
		_, err := svc.ImportForCoach(ctx, f.user, "plan.txt", []byte("Lunch: frango"), req)
		done <- err
	}()
	<-reader.started

	if _, err := svc.ImportForCoach(ctx, f.user, "plan.txt", []byte("Lunch: frango"), req); planimport.ReasonOf(err) != planimport.ReasonBusy {
		t.Fatalf("second import: err = %v, want busy", err)
	}
	close(reader.release)
	<-done
	if q.spent != 1 {
		t.Fatalf("quota spent %d times, want once: the busy import must spend nothing", q.spent)
	}
}
