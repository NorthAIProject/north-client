package horizon

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/biometrics"
	"github.com/NorthAIProject/north-client/internal/health"
	"github.com/NorthAIProject/north-client/internal/horizon/forecast"
	"github.com/NorthAIProject/north-client/internal/lifts/lift"
	"github.com/NorthAIProject/north-client/internal/media/analysis"
	"github.com/NorthAIProject/north-client/internal/preferences"
	"github.com/NorthAIProject/north-client/internal/progresschecks/progresscheck"
	"github.com/NorthAIProject/north-client/internal/shared/apitest"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/users"
	"github.com/NorthAIProject/north-client/internal/workouts"
	"github.com/NorthAIProject/north-client/internal/workouts/plan"
)

var at = time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC) // a Wednesday

// person is the slices Horizon reads, as one fake.
type person struct {
	sets    []lift.Set
	weights []health.Stored
	typed   []biometrics.Biometric
	checks  []progresscheck.Check
	plan    *plan.Plan
	target  *float64
	low     int
}

// MatchExercises knows one loose spelling, as the catalog matcher would.
func (p person) MatchExercises(_ context.Context, names []string) (map[string]string, error) {
	out := map[string]string{}
	for _, name := range names {
		switch name {
		case "squat":
			out[name] = "barbell-squat"
		case "bench-press":
			// A matcher that would send one lift's key to another lift.
			out[name] = "barbell-squat"
		}
	}
	return out, nil
}

// MuscleWeights knows the one exercise the tests plan without logging.
func (p person) MuscleWeights(_ context.Context, slugs []string) (lift.Weights, error) {
	out := lift.Weights{}
	for _, slug := range slugs {
		if slug == "barbell-curl" {
			out[slug] = lift.MuscleWeights([]string{"biceps"}, nil)
		}
	}
	return out, nil
}

func (p person) History(context.Context, users.User) ([]lift.Set, lift.Weights, time.Time, error) {
	return p.sets, lift.Weights{
		"barbell-squat": {"quads": 1, "glutes": 0.4},
		"bench-press":   {"chest": 1, "triceps": 0.4},
	}, at, nil
}

func (p person) ActivePlan(context.Context, uuid.UUID) (workouts.StoredPlan, error) {
	if p.plan == nil {
		return workouts.StoredPlan{}, apperr.ErrNotFound
	}
	return workouts.StoredPlan{Plan: *p.plan}, nil
}

func (p person) Between(context.Context, uuid.UUID, string, time.Time, time.Time) ([]health.Stored, error) {
	return p.weights, nil
}

func (p person) LowMornings(context.Context, users.User, time.Time) (int, error) { return p.low, nil }

func (p person) Get(context.Context, uuid.UUID) (preferences.Preferences, error) {
	return preferences.Preferences{TargetWeightKg: p.target}, nil
}

type typed struct{ rows []biometrics.Biometric }

func (t typed) History(context.Context, uuid.UUID, int) ([]biometrics.Biometric, error) {
	return t.rows, nil
}

type checks struct{ rows []progresscheck.Check }

func (c checks) History(context.Context, users.User) ([]progresscheck.Check, error) {
	return c.rows, nil
}

func service(p person) *Service {
	return NewService(Options{
		Lifts: p, Plans: p, Readings: p, Recovery: p, Targets: p,
		Measurements: typed{p.typed}, Checks: checks{p.checks},
	})
}

var user = users.User{ID: uuid.MustParse("11111111-1111-1111-1111-111111111111"), Timezone: "UTC"}

// trained is weeks of a lift done twice a week, three sets of five, its
// estimated max ending at endKg after rising perDay.
func trained(slug, name string, weeks int, endKg, perDay float64) []lift.Set {
	var out []lift.Set
	for d := float64(weeks * 7); d >= 0; d -= 3.5 {
		when := at.Add(-time.Duration(d * float64(forecast.Day)))
		y, m, dd := when.Date()
		w := (endKg - perDay*d) * 6 / 7
		for n := 1; n <= 3; n++ {
			out = append(out, lift.Set{
				ExerciseSlug: slug, ExerciseName: name, Kind: lift.KindWork, SetNumber: n,
				WeightKg: w, Reps: 5, PerformedAt: when, LogDate: time.Date(y, m, dd, 0, 0, 0, 0, time.UTC),
			})
		}
	}
	return out
}

func weighIns(days int, endKg, perDay float64) []health.Stored {
	var out []health.Stored
	for d := days; d >= 0; d-- {
		// A fixed wobble, so the series is not a perfect line.
		wobble := float64((d*7)%5-2) * 0.15
		out = append(out, health.Stored{Metric: metricBodyMass, Value: endKg - perDay*float64(d) + wobble, StartedAt: at.AddDate(0, 0, -d)})
	}
	return out
}

func f(v float64) *float64 { return &v }

func regular() person {
	target := 78.0
	return person{
		sets:    append(trained("barbell-squat", "Barbell Squat", 30, 140, 0.05), trained("bench-press", "Bench Press", 30, 100, 0)...),
		weights: weighIns(70, 80, -0.5/7),
		typed:   []biometrics.Biometric{{WeightKg: 84, HeightCm: 180, CreatedAt: at.AddDate(0, 0, -200)}},
		checks: []progresscheck.Check{
			{MeasuredOn: at.AddDate(0, 0, -56), Measurements: progresscheck.Measurements{WaistCm: f(92), ChestCm: f(104), HipsCm: f(100), WeightKg: f(84)}, WeightCarried: true},
			{MeasuredOn: at.AddDate(0, 0, -28), Measurements: progresscheck.Measurements{WaistCm: f(90.5), ChestCm: f(104), HipsCm: f(100)}},
			{MeasuredOn: at.AddDate(0, 0, -1), Measurements: progresscheck.Measurements{WaistCm: f(89), ChestCm: f(104), HipsCm: f(99)}},
		},
		plan: &plan.Plan{Days: []plan.PlanDay{
			{Weekday: "Monday", Exercises: []plan.Exercise{{Name: "Barbell Squat", CatalogSlug: "barbell-squat", Sets: 4, Reps: "4-6"}}},
			{Weekday: "Thursday", Exercises: []plan.Exercise{{Name: "Bench Press", CatalogSlug: "bench-press", Sets: 4, Reps: "5"}}},
		}},
		target: &target,
	}
}

func TestForecastReadsEverySource(t *testing.T) {
	h, err := service(regular()).Forecast(context.Background(), user, forecast.Scenario{})
	if err != nil {
		t.Fatal(err)
	}
	if !h.Ready() || !h.HasPlan || len(h.Lifts) != 2 || len(h.Frames) != len(forecast.Offsets()) {
		t.Fatalf("ready %v, plan %v, %d lifts, %d frames", h.Ready(), h.HasPlan, len(h.Lifts), len(h.Frames))
	}
	if h.Lifts[0].Plateau != forecast.ProgressProgressing || h.Lifts[1].Plateau != forecast.ProgressPlateau {
		t.Errorf("plateau = %s, %s", h.Lifts[0].Plateau, h.Lifts[1].Plateau)
	}
	if h.Weight.Status != forecast.StatusReady || h.Waist.Status != forecast.StatusReady {
		t.Fatalf("weight %s %+v, waist %s %+v", h.Weight.Status, h.Weight.Needs, h.Waist.Status, h.Waist.Needs)
	}
	// The 84 kg typed 200 days ago and the weight a check only carried over
	// are not weigh-ins: today's level is the scale's.
	if h.Weight.Now < 79 || h.Weight.Now > 81 {
		t.Errorf("weight now = %.2f", h.Weight.Now)
	}
	if !h.WeightETA.Reachable || h.WeightETA.Reached {
		t.Errorf("eta = %+v", h.WeightETA)
	}
	// Losing weight and waist: the figure is slimmer in four weeks.
	today, later := h.Frames[0].Shape, h.Frames[16].Shape
	if h.Frames[16].OffsetDays != 28 || later.Waist >= today.Waist || later.Overall > today.Overall {
		t.Errorf("shape today %+v, in four weeks %+v", today, later)
	}
	if later.Chest != today.Chest {
		t.Errorf("chest moved without a forecast: %v to %v", today.Chest, later.Chest)
	}
	if len(h.Frames[1].Regions) == 0 {
		t.Error("no regions on the figure")
	}
	if h.Deload.Suggested {
		t.Errorf("deload suggested: %+v", h.Deload)
	}
}

func TestWhatIfsNeverRaiseStrength(t *testing.T) {
	svc := service(regular())
	plain, err := svc.Forecast(context.Background(), user, forecast.Scenario{})
	if err != nil {
		t.Fatal(err)
	}
	for _, sc := range []forecast.Scenario{{ExtraSessions: 1}, {DeloadNow: true}, {SkipWeeks: 3}, {SkipWeeks: 9, ExtraSessions: 4, DeloadNow: true}} {
		got, err := svc.Forecast(context.Background(), user, sc)
		if err != nil {
			t.Fatal(err)
		}
		for i, l := range got.Lifts {
			for j, p := range l.Points {
				if p.Mid > plain.Lifts[i].Points[j].Mid+1e-9 {
					t.Fatalf("%+v: %s day %d forecast %.2f above the plain %.2f", sc, l.Name, p.OffsetDays, p.Mid, plain.Lifts[i].Points[j].Mid)
				}
			}
		}
	}
	extra, _ := svc.Forecast(context.Background(), user, forecast.Scenario{ExtraSessions: 1})
	for i, l := range extra.Lifts {
		if got, want := l.Points, plain.Lifts[i].Points; len(got) != len(want) || got[len(got)-1] != want[len(want)-1] {
			t.Errorf("an extra session changed %s's forecast", l.Name)
		}
	}
}

func TestDeloadNeedsTwoSigns(t *testing.T) {
	p := regular()
	p.sets = append(trained("barbell-squat", "Barbell Squat", 30, 140, 0), trained("bench-press", "Bench Press", 30, 100, 0)...)
	p.low = 4
	h, err := service(p).Forecast(context.Background(), user, forecast.Scenario{})
	if err != nil {
		t.Fatal(err)
	}
	if !h.Deload.Suggested || len(h.Deload.Reasons) < 2 {
		t.Errorf("deload = %+v", h.Deload)
	}
}

func TestNewPersonIsToldWhatIsMissing(t *testing.T) {
	p := person{sets: trained("barbell-squat", "Barbell Squat", 1, 100, 0)}
	h, err := service(p).Forecast(context.Background(), user, forecast.Scenario{})
	if err != nil {
		t.Fatal(err)
	}
	if h.Ready() || h.HasPlan {
		t.Fatalf("ready %v, plan %v", h.Ready(), h.HasPlan)
	}
	needs := h.Needs()
	if len(needs) != 3 || needs[0].Code != forecast.NeedLiftSessions || needs[0].Have != 3 || needs[1].Code != forecast.NeedWeighIns || needs[2].Code != forecast.NeedWaistChecks {
		t.Errorf("needs = %+v", needs)
	}
	nobody, err := service(person{}).Forecast(context.Background(), user, forecast.Scenario{})
	if err != nil || nobody.Ready() || nobody.Needs()[0].Subject != "" {
		t.Errorf("nobody: %+v %v", nobody.Needs(), err)
	}
}

// An imported plan names its exercises as the person's file did, with no
// catalog slug, and may hold lifts never logged. Both must still train the
// figure: following a plan is not detraining.
func TestAnImportedPlanStillTrainsTheFigure(t *testing.T) {
	p := regular()
	p.plan = &plan.Plan{Days: []plan.PlanDay{
		{Weekday: "Monday", Exercises: []plan.Exercise{{Name: "squat", Sets: 4, Reps: "4-6"}}},
		{Weekday: "Thursday", Exercises: []plan.Exercise{{Name: "Curl", CatalogSlug: "barbell-curl", Sets: 3, Reps: "8-12"}}},
	}}
	h, err := service(p).Forecast(context.Background(), user, forecast.Scenario{})
	if err != nil {
		t.Fatal(err)
	}
	var week6 BodyFrame
	for _, f := range h.Frames {
		if f.OffsetDays == 42 {
			week6 = f
		}
	}
	region := map[string]RegionLoad{}
	for _, r := range week6.Regions {
		region[r.Region] = r
	}
	// Squats, matched by name, keep the quads trained six weeks on.
	if got := region["quads"]; got.Retained < 0.99 {
		t.Errorf("quads = %+v, want still trained", got)
	}
	// The curl was never logged, so only the plan can have put it there.
	if got, ok := region["biceps"]; !ok || got.Retained < 0.99 {
		t.Errorf("biceps = %+v (present %v), want trained by the plan", got, ok)
	}
	// Bench is no longer planned: the chest is what detrains.
	if got := region["chest"]; got.Retained > 0.9 {
		t.Errorf("chest = %+v, want detraining", got)
	}

	// The imported name finds its rep range too.
	next, err := service(p).NextSets(context.Background(), user, []string{"barbell-squat"})
	if err != nil {
		t.Fatal(err)
	}
	if got := next["barbell-squat"]; got.Reason != forecast.NextAddRep || got.Reps != 6 {
		t.Errorf("squat = %+v, want the plan's 4-6 applied", got)
	}
}

func TestLiftDetailAndNextSets(t *testing.T) {
	svc := service(regular())
	detail, err := svc.Lift(context.Background(), user, "barbell-squat")
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.History) < 50 || len(detail.Past) < forecast.MinScored || detail.Status != forecast.StatusReady {
		t.Errorf("%d history, %d past, %s", len(detail.History), len(detail.Past), detail.Status)
	}
	if _, err = svc.Lift(context.Background(), user, "deadlift"); !apperr.Is(err, apperr.ErrNotFound) {
		t.Errorf("unknown lift: %v", err)
	}

	next, err := svc.NextSets(context.Background(), user, []string{"barbell-squat", "bench-press", "deadlift"})
	if err != nil {
		t.Fatal(err)
	}
	// Squat's plan range is 4-6 and the last session was fives: one more rep.
	if got := next["barbell-squat"]; got.Reason != forecast.NextAddRep || got.Reps != 6 {
		t.Errorf("squat = %+v", got)
	}
	// Bench's range is exactly 5, done, effort unknown: nothing to add yet.
	if got := next["bench-press"]; got.Reason != forecast.NextAddRep || got.Reps != 5 {
		t.Errorf("bench = %+v", got)
	}
	if _, ok := next["deadlift"]; ok {
		t.Error("a suggestion for a lift never done")
	}

	// A client that holds only the typed name asks by it, and gets the same
	// suggestion, plan range included, back under what it asked for.
	named, err := svc.NextSets(context.Background(), user, []string{"barbell squat"})
	if err != nil {
		t.Fatal(err)
	}
	if got := named["barbell squat"]; got != next["barbell-squat"] {
		t.Errorf("by name = %+v, by key = %+v", got, next["barbell-squat"])
	}
	if len(named) != 1 {
		t.Errorf("asked for one lift, got %d", len(named))
	}
	// A lift's own key is never re-read as a name: asked for together, squat
	// and bench stay two lifts whatever the matcher makes of "bench-press".
	if next["bench-press"] == next["barbell-squat"] {
		t.Errorf("bench answered with squat's sets: %+v", next["bench-press"])
	}
	// A looser spelling is resolved the way logging a set resolves it.
	loose, err := svc.NextSets(context.Background(), user, []string{"squat", "nothing like it"})
	if err != nil {
		t.Fatal(err)
	}
	if got := loose["squat"]; got != next["barbell-squat"] || len(loose) != 1 {
		t.Errorf("loose = %+v", loose)
	}
}

// forms is the form slice, newest first as it lists them.
type forms []analysis.Analysis

func (f forms) ListAnalyses(context.Context, uuid.UUID, int) ([]analysis.Analysis, error) {
	return f, nil
}

func TestALiftCarriesItsNewestFormCheck(t *testing.T) {
	p := regular()
	newest := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	svc := NewService(Options{Lifts: p, Forms: forms{
		// Another lift's clip, newer than any of squat's.
		{ID: uuid.New(), ExerciseSlug: "bench-press", CreatedAt: at, Result: &analysis.FormAnalysis{Summary: "Bench."}},
		// A clip filed under no exercise says nothing about any lift.
		{ID: uuid.New(), CreatedAt: at, Result: &analysis.FormAnalysis{Summary: "Unfiled."}},
		{ID: newest, ExerciseSlug: "barbell-squat", CreatedAt: at.AddDate(0, 0, -2), Result: &analysis.FormAnalysis{
			Summary: "Depth is good.", Confidence: analysis.ConfidenceHigh,
			Issues: []analysis.FormIssue{
				{Severity: analysis.SeverityMinor, Correction: "Brace sooner."},
				{Severity: analysis.SeverityModerate, Correction: "Knees out."},
				{Severity: analysis.SeverityModerate, Correction: "Chest up."},
			},
		}},
		{ID: uuid.New(), ExerciseSlug: "barbell-squat", CreatedAt: at.AddDate(0, 0, -30), Result: &analysis.FormAnalysis{Summary: "Older."}},
	}})

	squat, err := svc.Lift(context.Background(), user, "barbell-squat")
	if err != nil {
		t.Fatal(err)
	}
	if squat.Form == nil || squat.Form.AnalysisID != newest || squat.Form.Summary != "Depth is good." {
		t.Fatalf("squat's form = %+v", squat.Form)
	}
	// The most serious thing seen, the earliest of equals.
	if squat.Form.Cue != "Knees out." {
		t.Errorf("cue = %q", squat.Form.Cue)
	}

	// No form slice, or no clip of the lift: no note, and no error.
	plain, err := service(p).Lift(context.Background(), user, "barbell-squat")
	if err != nil || plain.Form != nil {
		t.Errorf("without forms: %+v, %v", plain.Form, err)
	}
	none, err := NewService(Options{Lifts: p, Forms: forms{}}).Lift(context.Background(), user, "barbell-squat")
	if err != nil || none.Form != nil {
		t.Errorf("without a clip: %+v, %v", none.Form, err)
	}
}

func TestHorizonShape(t *testing.T) {
	ctx := context.Background()
	view, err := service(regular()).View(ctx, user, forecast.Scenario{})
	if err != nil {
		t.Fatal(err)
	}
	if view.Lifts[0].TrackRecord == nil || view.Lifts[0].TrackRecord.Kind != RecordBacktest {
		t.Fatalf("track record = %+v", view.Lifts[0].TrackRecord)
	}
	// The contract is every field's shape; two frames show it as well as 25.
	view.Frames = view.Frames[:2]
	apitest.AssertGolden(t, "horizon.golden.json", view)

	empty, err := service(person{sets: trained("barbell-squat", "Barbell Squat", 1, 100, 0)}).View(ctx, user, forecast.Scenario{})
	if err != nil {
		t.Fatal(err)
	}
	empty.Frames = empty.Frames[:1]
	apitest.AssertGolden(t, "horizon-insufficient.golden.json", empty)
}

func TestAWhatIfShowsNoTrackRecord(t *testing.T) {
	view, err := service(regular()).View(context.Background(), user, forecast.Scenario{SkipWeeks: 2})
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range view.Lifts {
		if l.TrackRecord != nil {
			t.Errorf("%s shows a track record beside a what-if", l.Name)
		}
	}
	if view.Scenario.SkipWeeks != 2 {
		t.Errorf("scenario = %+v", view.Scenario)
	}
}

func TestHorizonSpeaksTheReadersLanguage(t *testing.T) {
	for _, locale := range []string{"es", "pt-PT", "pt-BR"} {
		ctx := i18n.WithLocale(context.Background(), locale)
		view, err := service(regular()).View(ctx, user, forecast.Scenario{})
		if err != nil {
			t.Fatal(err)
		}
		en, _ := service(regular()).View(context.Background(), user, forecast.Scenario{})
		if view.Disclaimer == en.Disclaimer || view.Summary[0] == en.Summary[0] || view.Lifts[0].PlateauText == en.Lifts[0].PlateauText {
			t.Errorf("%s: still English", locale)
		}
	}
}

func TestBriefForTheCoach(t *testing.T) {
	svc := service(regular())
	h, err := svc.Forecast(context.Background(), user, forecast.Scenario{})
	if err != nil {
		t.Fatal(err)
	}
	short, full := Brief(h, false), Brief(h, true)
	for _, want := range []string{"ranges, not promises", "Barbell Squat est. 1RM 140.0 kg now, progressing; in 4 weeks 139.7–142.5 kg", "Body weight 80.0 kg now", "target 78.0 kg"} {
		if !strings.Contains(short, want) {
			t.Errorf("brief lacks %q:\n%s", want, short)
		}
	}
	if !strings.Contains(full, "in 12 weeks") || !strings.Contains(full, "would have landed in range 23 of 23 times") || strings.Contains(short, "in 12 weeks") {
		t.Errorf("full brief:\n%s", full)
	}

	off, _ := svc.Forecast(context.Background(), user, forecast.Scenario{SkipWeeks: 3})
	if got := Brief(off, true); !strings.Contains(got, "What-if forecast (3 weeks off") || !strings.Contains(got, "never raises a strength forecast") {
		t.Errorf("what-if brief:\n%s", got)
	}

	nobody, _ := service(person{}).Forecast(context.Background(), user, forecast.Scenario{})
	if got := Brief(nobody, false); got != "" {
		t.Errorf("brief with nothing to say: %q", got)
	}
}

func TestSuggestedVolumeForTheWeeklyReview(t *testing.T) {
	volume, reason, err := service(regular()).SuggestedVolume(context.Background(), user)
	if err != nil || volume != "" || reason != "" {
		t.Errorf("nothing to suggest: %q %q %v", volume, reason, err)
	}

	p := regular()
	p.sets = append(trained("barbell-squat", "Barbell Squat", 30, 140, 0), trained("bench-press", "Bench Press", 30, 100, 0)...)
	p.low = 4
	volume, reason, err = service(p).SuggestedVolume(context.Background(), user)
	if err != nil || volume != "deload" {
		t.Fatalf("volume = %q, %v", volume, err)
	}
	if !strings.Contains(reason, "Two or more lifts have stalled") || !strings.HasSuffix(reason, ".") {
		t.Errorf("reason = %q", reason)
	}
}
