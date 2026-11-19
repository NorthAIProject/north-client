package forecast

import (
	"fmt"
	"math"
	"math/rand"
	"reflect"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/lifts/lift"
)

var now = time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC) // a Wednesday

func daysAgo(n float64) time.Time { return now.Add(-time.Duration(n * float64(Day))) }

// line is a series every step days for weeks, ending today: start value at
// the oldest point, perDay slope, noise from a fixed seed.
func line(weeks int, step float64, end, perDay, noise float64, seed int64) []Point {
	rng := rand.New(rand.NewSource(seed))
	var out []Point
	for d := float64(weeks * 7); d >= 0; d -= step {
		out = append(out, Point{At: daysAgo(d), Value: end - perDay*d + noise*rng.NormFloat64()})
	}
	return out
}

func near(t *testing.T, name string, got, want, tol float64) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Errorf("%s = %.4f, want %.4f ± %.4f", name, got, want, tol)
	}
}

func TestFitRecoversASlope(t *testing.T) {
	trend, ok := Fit(line(8, 3.5, 100, 0.1, 0, 1))
	if !ok {
		t.Fatal("no fit")
	}
	near(t, "slope", trend.SlopePerDay, 0.1, 1e-9)
	near(t, "level", trend.Level, 100, 1e-9)
	near(t, "spread", trend.Spread, 0, 1e-9)
}

func TestFitIgnoresAWildPoint(t *testing.T) {
	pts := line(8, 3.5, 100, 0.1, 0, 1)
	pts[5].Value = 40 // a set logged with the wrong plate
	pts[11].Value = 190
	trend, _ := Fit(pts)
	near(t, "slope", trend.SlopePerDay, 0.1, 0.01)
	near(t, "level", trend.Level, 100, 0.5)
}

func TestFitNeedsTwoDays(t *testing.T) {
	if _, ok := Fit(nil); ok {
		t.Error("fit from nothing")
	}
	if _, ok := Fit([]Point{{At: now, Value: 1}}); ok {
		t.Error("fit from one point")
	}
	if _, ok := Fit([]Point{{At: now, Value: 1}, {At: now, Value: 2}}); ok {
		t.Error("fit from two points at one moment")
	}
}

func TestAheadFlattens(t *testing.T) {
	trend := Trend{Level: 100, SlopePerDay: 0.2}
	straight := 100 + 0.2*84
	got := trend.Ahead(84)
	if got >= straight || got <= 100 {
		t.Errorf("Ahead(84) = %.2f, want between 100 and the straight line %.2f", got, straight)
	}
	// Never more than DampingDays' worth of the slope, however far out.
	if far := trend.Ahead(10000); far > 100+0.2*DampingDays+1e-9 {
		t.Errorf("Ahead(far) = %.2f, above the cap", far)
	}
	near(t, "Ahead(0)", trend.Ahead(0), 100, 0)
	// A week out is still close to the straight line.
	near(t, "Ahead(7)", trend.Ahead(7), 101.4, 0.1)
}

// The promise the ranges make: about four in five outcomes land inside.
// Checked out of sample across many synthetic people.
func TestBacktestCoverageIsHonest(t *testing.T) {
	hits, total := 0, 0
	for seed := int64(1); seed <= 60; seed++ {
		pts := line(52, 3.5, 100, 0.05, 2.0, seed)
		misses := Misses(pts, now, 28, liftFit)
		record := Score(misses, 4)
		hits += record.Hits
		total += record.Total
	}
	if total < 500 {
		t.Fatalf("only %d scored forecasts", total)
	}
	coverage := float64(hits) / float64(total)
	if coverage < 0.68 || coverage > 0.92 {
		t.Errorf("coverage = %.3f over %d forecasts, want about 0.80", coverage, total)
	}
}

func TestBandNeedsHistoryAndNeverClaimsCertainty(t *testing.T) {
	if _, ok := BandFrom(make([]Miss, minErrors-1)); ok {
		t.Error("band from too few misses")
	}
	perfect := make([]Miss, 8)
	for i := range perfect {
		perfect[i] = Miss{Predicted: 100, Actual: 100}
	}
	band, ok := BandFrom(perfect)
	if !ok || band.Low > -minWidth || band.High < minWidth {
		t.Errorf("band = %+v, want at least ±%.2f", band, minWidth)
	}
	r := band.Around(100)
	if !(r.Low < r.Mid && r.Mid < r.High) {
		t.Errorf("range = %+v", r)
	}
}

// Twelve weeks of training twice a week is enough to state a record: the
// early forecasts are scored against the wide range they would have had.
func TestTwelveWeeksStateARecord(t *testing.T) {
	pts := line(12, 3.5, 100, 0.05, 2.0, 7)
	misses := Misses(pts, now, recordHorizonDays, liftFit)
	record := Score(misses, recordHorizonDays/7)
	if !record.Shown() || record.Total != len(misses) {
		t.Errorf("record = %+v from %d past forecasts", record, len(misses))
	}
	// Eight weeks is not: too few forecasts have come due.
	short := Misses(line(8, 3.5, 100, 0.05, 2.0, 7), now, recordHorizonDays, liftFit)
	if Score(short, recordHorizonDays/7).Shown() {
		t.Errorf("a record from %d past forecasts", len(short))
	}
}

func TestScoreIsOutOfSample(t *testing.T) {
	// Five calm misses, then one far outside them. Judged against a band
	// that included it, it would count as a hit.
	var misses []Miss
	for _, a := range []float64{100, 101, 99, 100.5, 99.5} {
		misses = append(misses, Miss{Predicted: 100, Actual: a})
	}
	misses = append(misses, Miss{Predicted: 100, Actual: 130})
	// The first five are judged against the scatter of their own day's fit,
	// which is what they would have been drawn with; all five land.
	got := Score(misses, 4)
	if got.Total != 6 || got.Hits != 5 {
		t.Errorf("record = %+v, want 5 of 6", got)
	}
	if few := Score(misses[:MinScored-1], 4); few.Shown() {
		t.Errorf("a record of %d forecasts is shown", few.Total)
	}
}

func liftSets(slug string, weeks int, perWeek int, endKg, perDay float64) []lift.Set {
	var out []lift.Set
	step := 7.0 / float64(perWeek)
	for d := float64(weeks * 7); d >= 0; d -= step {
		at := daysAgo(d)
		y, m, dd := at.Date()
		// e1RM of w×5 is w×7/6; pick w so the max follows the line.
		w := (endKg - perDay*d) * 6 / 7
		for n := 1; n <= 3; n++ {
			out = append(out, lift.Set{
				ExerciseSlug: slug, ExerciseName: slug, Kind: lift.KindWork, SetNumber: n,
				WeightKg: w, Reps: 5, PerformedAt: at, LogDate: time.Date(y, m, dd, 0, 0, 0, 0, time.UTC),
			})
		}
	}
	return out
}

func TestAdjustedE1RM(t *testing.T) {
	rir := func(n int) *int { return &n }
	cases := []struct {
		name string
		set  lift.Set
		want float64
		ok   bool
	}{
		{"as logged", lift.Set{Kind: lift.KindWork, WeightKg: 100, Reps: 5}, lift.E1RM(100, 5), true},
		{"two in reserve", lift.Set{Kind: lift.KindWork, WeightKg: 100, Reps: 5, RIR: rir(2)}, lift.E1RM(100, 7), true},
		{"reserve is capped", lift.Set{Kind: lift.KindWork, WeightKg: 100, Reps: 5, RIR: rir(9)}, lift.E1RM(100, 9), true},
		{"to failure", lift.Set{Kind: lift.KindWork, WeightKg: 100, Reps: 5, RIR: rir(0)}, lift.E1RM(100, 5), true},
		{"warm-up", lift.Set{Kind: lift.KindWarmup, WeightKg: 60, Reps: 5}, 0, false},
		{"bodyweight", lift.Set{Kind: lift.KindWork, Reps: 12}, 0, false},
		{"too many reps", lift.Set{Kind: lift.KindWork, WeightKg: 40, Reps: 20}, 0, false},
	}
	for _, c := range cases {
		got, ok := AdjustedE1RM(c.set)
		if ok != c.ok || math.Abs(got-c.want) > 1e-9 {
			t.Errorf("%s: got %.2f %v, want %.2f %v", c.name, got, ok, c.want, c.ok)
		}
	}
}

func TestForecastLiftReady(t *testing.T) {
	hist := LiftHistories(liftSets("squat", 30, 2, 140, 0.05))
	if len(hist) != 1 {
		t.Fatalf("%d histories", len(hist))
	}
	f := ForecastLift(hist[0], now, Pause{})
	if f.Status != StatusReady {
		t.Fatalf("status = %s, needs %+v", f.Status, f.Needs)
	}
	near(t, "now", f.Now, 140, 0.5)
	last, _ := f.Last()
	if last.OffsetDays != MaxOffset {
		t.Errorf("reaches %d days, want %d", last.OffsetDays, MaxOffset)
	}
	prevWidth := 0.0
	for _, p := range f.Points {
		if !(p.Low <= p.Mid && p.Mid <= p.High) {
			t.Fatalf("day %d out of order: %+v", p.OffsetDays, p.Range)
		}
		width := (p.High - p.Low) / p.Mid
		if width+1e-9 < prevWidth {
			t.Errorf("day %d: range narrows (%.4f after %.4f)", p.OffsetDays, width, prevWidth)
		}
		prevWidth = width
	}
	if first := f.Points[0]; first.Low != first.High {
		t.Errorf("today has a range: %+v", first.Range)
	}
	if !f.Track.Shown() || f.Track.HorizonWeeks != 4 {
		t.Errorf("track = %+v", f.Track)
	}
	if f.Plateau != ProgressProgressing {
		t.Errorf("plateau = %s", f.Plateau)
	}
}

func TestForecastLiftSaysWhatIsMissing(t *testing.T) {
	few := LiftHistories(liftSets("squat", 2, 1, 140, 0))[0] // 3 sessions
	f := ForecastLift(few, now, Pause{})
	want := []Need{{Code: NeedLiftSessions, Subject: "squat", Have: 3, Want: LiftMinSessions}}
	if f.Status != StatusInsufficient || !reflect.DeepEqual(f.Needs, want) || len(f.Points) != 0 {
		t.Errorf("got %s %+v", f.Status, f.Needs)
	}

	stale := LiftHistories(liftSets("squat", 12, 2, 140, 0.05))[0]
	later := now.Add(30 * Day)
	f = ForecastLift(stale, later, Pause{})
	if f.Status != StatusInsufficient || f.Needs[0].Code != NeedLiftRecent {
		t.Errorf("stale lift: %s %+v", f.Status, f.Needs)
	}
}

func TestForecastStopsWhereHistoryStops(t *testing.T) {
	f := ForecastLift(LiftHistories(liftSets("squat", 5, 2, 140, 0.05))[0], now, Pause{})
	if f.Status != StatusReady {
		t.Fatalf("status = %s %+v", f.Status, f.Needs)
	}
	last, _ := f.Last()
	if last.OffsetDays > 35 {
		t.Errorf("five weeks of history forecast %d days ahead", last.OffsetDays)
	}
}

// The honest what-if: time away never helps, and the plain forecast is
// never below it.
func TestPauseOnlyCosts(t *testing.T) {
	hist := LiftHistories(liftSets("squat", 30, 2, 140, 0.05))[0]
	plain := ForecastLift(hist, now, Pause{})
	for weeks := 1; weeks <= MaxSkipWeeks; weeks++ {
		away := ForecastLift(hist, now, Scenario{SkipWeeks: weeks}.Pause())
		for i, p := range away.Points {
			if p.Mid > plain.Points[i].Mid+1e-9 {
				t.Fatalf("%d weeks off, day %d: %.2f above the plain %.2f", weeks, p.OffsetDays, p.Mid, plain.Points[i].Mid)
			}
		}
		end, _ := away.Last()
		plainEnd, _ := plain.Last()
		if end.Mid >= plainEnd.Mid {
			t.Errorf("%d weeks off costs nothing by week 12", weeks)
		}
	}
	// While away the lift stands still: no gain, and no loss claimed.
	four := ForecastLift(hist, now, Pause{Days: 28})
	for _, day := range []int{0, 14, 28} {
		p, _ := four.At(day)
		near(t, fmt.Sprintf("day %d of four weeks off", day), p.Mid, plain.Now, 0.01)
	}
	// Back in the gym it is the plain forecast, four weeks late.
	late, _ := four.At(56)
	onTime, _ := plain.At(28)
	near(t, "four weeks after coming back", late.Mid, onTime.Mid, 0.01)
}

// A lift already sliding is not shown as rescued by a break.
func TestPauseNeverHelpsASlidingLift(t *testing.T) {
	hist := LiftHistories(liftSets("squat", 30, 2, 140, -0.08))[0]
	plain := ForecastLift(hist, now, Pause{})
	away := ForecastLift(hist, now, Pause{Days: 28})
	for i, p := range away.Points {
		if p.Mid > plain.Points[i].Mid+1e-9 {
			t.Fatalf("day %d: %.2f above the plain %.2f", p.OffsetDays, p.Mid, plain.Points[i].Mid)
		}
	}
}

func TestScenarioClamp(t *testing.T) {
	got := Scenario{SkipWeeks: 99, ExtraSessions: -3}.Clamp()
	if got.SkipWeeks != MaxSkipWeeks || got.ExtraSessions != 0 {
		t.Errorf("clamp = %+v", got)
	}
	if !(Scenario{}).IsZero() || (Scenario{DeloadNow: true}).IsZero() {
		t.Error("IsZero")
	}
}

func TestProgressStates(t *testing.T) {
	flat := func(v float64) []Point { return line(8, 3.5, v, 0, 0, 1) }
	sliding := line(8, 3.5, 95, -0.12, 0, 1)
	holding := flat(100)
	holding[len(holding)-2].Value = 104 // a recent best on a flat trend
	cases := []struct {
		name string
		pts  []Point
		want string
	}{
		{"rising", line(8, 3.5, 100, 0.08, 0, 1), ProgressProgressing},
		{"flat", flat(100), ProgressPlateau},
		{"falling", sliding, ProgressSliding},
		{"flat with a recent best", holding, ProgressHolding},
		{"too few sessions", line(8, 14, 100, 0.1, 0, 1), ProgressInsufficient},
		{"nothing", nil, ProgressInsufficient},
	}
	for _, c := range cases {
		if got := Progress(c.pts, now); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
}

func TestSuggestDeloadNeedsTwoSigns(t *testing.T) {
	cases := []struct {
		name string
		in   DeloadInput
		want bool
		n    int
	}{
		{"nothing", DeloadInput{WeeksSinceLighter: -1}, false, 0},
		{"one stuck lift", DeloadInput{Progress: []string{ProgressPlateau, ProgressProgressing}}, false, 0},
		{"two stuck lifts alone", DeloadInput{Progress: []string{ProgressPlateau, ProgressSliding}}, false, 1},
		{"stuck and long since", DeloadInput{Progress: []string{ProgressPlateau, ProgressSliding}, WeeksSinceLighter: 7}, true, 2},
		{"low recovery and reps falling", DeloadInput{LowRecoveryDays: 3, RepsFalling: true}, true, 2},
		{"two low mornings is not enough", DeloadInput{LowRecoveryDays: 2, RepsFalling: true}, false, 1},
	}
	for _, c := range cases {
		got := SuggestDeload(c.in)
		if got.Suggested != c.want || len(got.Reasons) != c.n {
			t.Errorf("%s: %+v", c.name, got)
		}
	}
}

func weekOfSets(weeksAgo, n int) []lift.Set {
	out := make([]lift.Set, n)
	for i := range out {
		// Tuesday of that week.
		out[i] = lift.Set{Kind: lift.KindWork, ExerciseSlug: "squat", WeightKg: 100, Reps: 5, PerformedAt: now.AddDate(0, 0, -1-7*weeksAgo)}
	}
	return out
}

func TestWeeksSinceLighter(t *testing.T) {
	build := func(counts ...int) []lift.Set { // oldest first, ending last week
		var out []lift.Set
		for i, n := range counts {
			out = append(out, weekOfSets(len(counts)-i, n)...)
		}
		return out
	}
	cases := []struct {
		name string
		sets []lift.Set
		want int
	}{
		{"no sets", nil, -1},
		{"too little history", build(40, 40, 40), -1},
		{"steady for seven weeks", build(40, 40, 40, 40, 40, 40, 40), 3},
		{"lighter last week", build(40, 40, 40, 40, 20), 0},
		{"lighter three weeks ago", build(40, 40, 40, 40, 20, 40, 40, 40), 3},
		{"a week off counts", build(40, 40, 40, 40, 0, 40), 1},
		{"this week is not judged", append(build(40, 40, 40, 40, 40), weekOfSets(0, 3)...), 1},
	}
	for _, c := range cases {
		if got := WeeksSinceLighter(c.sets, now, time.UTC); got != c.want {
			t.Errorf("%s: %d, want %d", c.name, got, c.want)
		}
	}
}

func sessionSets(slug string, daysBack int, weight float64, reps ...int) []lift.Set {
	at := daysAgo(float64(daysBack))
	y, m, d := at.Date()
	out := make([]lift.Set, len(reps))
	for i, r := range reps {
		out[i] = lift.Set{ExerciseSlug: slug, Kind: lift.KindWork, WeightKg: weight, Reps: r, SetNumber: i + 1, PerformedAt: at, LogDate: time.Date(y, m, d, 0, 0, 0, 0, time.UTC)}
	}
	return out
}

func join(groups ...[]lift.Set) []lift.Set {
	var out []lift.Set
	for _, g := range groups {
		out = append(out, g...)
	}
	return out
}

func TestRepsFalling(t *testing.T) {
	falling := join(sessionSets("bench", 14, 80, 8, 7), sessionSets("bench", 7, 80, 7, 6), sessionSets("bench", 1, 80, 6, 5))
	heavier := join(sessionSets("bench", 14, 80, 8), sessionSets("bench", 7, 82.5, 7), sessionSets("bench", 1, 85, 6))
	once := join(sessionSets("bench", 14, 80, 8), sessionSets("bench", 7, 80, 8), sessionSets("bench", 1, 80, 6))
	if !RepsFalling(falling, now) {
		t.Error("same bar, fewer reps twice: not flagged")
	}
	if RepsFalling(heavier, now) {
		t.Error("fewer reps at a heavier bar flagged")
	}
	if RepsFalling(once, now) {
		t.Error("one bad session flagged")
	}
}

func TestForecastWeight(t *testing.T) {
	// Losing 0.5 kg a week, weighed most days, twice some mornings.
	pts := line(10, 1, 80, -0.5/7, 0.3, 4)
	pts = append(pts, Point{At: daysAgo(0.01), Value: 80.2})
	s := ForecastWeight(pts, now)
	if s.Status != StatusReady {
		t.Fatalf("status %s %+v", s.Status, s.Needs)
	}
	near(t, "now", s.Now, 80, 0.4)
	p, ok := s.At(28)
	if !ok || p.Mid >= s.Now || p.Mid < 77 {
		t.Errorf("in four weeks: %+v", p)
	}

	if s := ForecastWeight(line(1, 2, 80, 0, 0, 1), now); s.Status != StatusInsufficient || s.Needs[0].Code != NeedWeighIns {
		t.Errorf("four weigh-ins: %s %+v", s.Status, s.Needs)
	}
	// Plenty of weigh-ins, all months old: not a forecast of today.
	old := line(6, 1, 80, 0, 0.2, 2)
	if s := ForecastWeight(old, now.Add(90*Day)); s.Status != StatusInsufficient {
		t.Errorf("stale weigh-ins forecast: %s", s.Status)
	}
}

func TestDailyMeans(t *testing.T) {
	got := DailyMeans([]Point{
		{At: time.Date(2026, 11, 1, 7, 0, 0, 0, time.UTC), Value: 80},
		{At: time.Date(2026, 11, 1, 21, 0, 0, 0, time.UTC), Value: 82},
		{At: time.Date(2026, 11, 2, 7, 0, 0, 0, time.UTC), Value: 79},
	})
	if len(got) != 2 || got[0].Value != 81 || got[1].Value != 79 {
		t.Errorf("got %+v", got)
	}
}

func TestForecastWaistNeedsItsOwnChecks(t *testing.T) {
	s := ForecastWaist([]Point{{At: daysAgo(30), Value: 90}, {At: daysAgo(2), Value: 89}}, now)
	want := []Need{{Code: NeedWaistChecks, Have: 2, Want: WaistMinChecks}}
	if s.Status != StatusInsufficient || !reflect.DeepEqual(s.Needs, want) {
		t.Errorf("got %s %+v", s.Status, s.Needs)
	}
	s = ForecastWaist(line(10, 14, 88, -0.02, 0, 1), now)
	if s.Status != StatusReady {
		t.Errorf("six checks over ten weeks: %s %+v", s.Status, s.Needs)
	}
}

func TestETA(t *testing.T) {
	s := ForecastWeight(line(10, 1, 80, -0.5/7, 0.3, 4), now)
	eta := ETAFor(s, 78.5)
	if !eta.Reachable || eta.Reached || eta.EarliestDays <= 0 {
		t.Fatalf("eta = %+v", eta)
	}
	if !eta.Open && eta.LatestDays < eta.EarliestDays {
		t.Errorf("latest before earliest: %+v", eta)
	}
	if got := ETAFor(s, 95); got.Reachable {
		t.Errorf("gaining target on a falling trend reachable: %+v", got)
	}
	if got := ETAFor(s, s.Now+0.0); !got.Reached {
		t.Errorf("at target: %+v", got)
	}
	if got := ETAFor(Series{Status: StatusInsufficient}, 70); got.Reachable {
		t.Error("eta without a forecast")
	}
}

var weights = lift.Weights{
	"squat": {"quads": 1, "glutes": 0.4},
	"bench": {"chest": 1, "triceps": 0.4},
}

var plan = []PlannedSession{
	{Weekday: time.Monday, Exercises: []PlannedExercise{{Slug: "squat", Sets: 4}}},
	{Weekday: time.Thursday, Exercises: []PlannedExercise{{Slug: "bench", Sets: 4}, {Slug: "", Sets: 3}}},
}

func muscle(l lift.Load, name string) lift.MuscleLoad {
	for _, m := range l.Muscles {
		if m.Muscle == name {
			return m
		}
	}
	return lift.MuscleLoad{}
}

func TestFramesFollowThePlan(t *testing.T) {
	history := join(sessionSets("squat", 2, 100, 5, 5, 5, 5), sessionSets("bench", 6, 80, 5, 5, 5, 5))
	frames := Frames(history, plan, weights, now, Scenario{})
	if len(frames) != len(Offsets()) || frames[0].OffsetDays != 0 || !frames[0].At.Equal(now) {
		t.Fatalf("%d frames, first %+v", len(frames), frames[0].OffsetDays)
	}
	// Today is Wednesday: bench is tomorrow evening, so chest is fresher on
	// Thursday morning than on Friday morning.
	thu, fri := muscle(frames[1].Load, "chest"), muscle(frames[2].Load, "chest")
	if fri.Fatigue <= thu.Fatigue {
		t.Errorf("chest fatigue Thursday %.3f, Friday %.3f", thu.Fatigue, fri.Fatigue)
	}
	// Followed for twelve weeks, nothing detrains.
	end := frames[len(frames)-1].Load
	for _, m := range end.Muscles {
		if m.Detrained() {
			t.Errorf("%s detrained on the plan", m.Muscle)
		}
	}
}

func TestFramesWhatIfs(t *testing.T) {
	history := join(sessionSets("squat", 2, 100, 5, 5, 5, 5), sessionSets("bench", 6, 80, 5, 5, 5, 5))
	plain := Frames(history, plan, weights, now, Scenario{})
	// Day 9 is the Friday of next week, the morning after bench.
	const day = 9

	skip := Frames(history, plan, weights, now, Scenario{SkipWeeks: 4})
	if got, was := muscle(skip[day].Load, "chest").Fatigue, muscle(plain[day].Load, "chest").Fatigue; got >= was {
		t.Errorf("time off: chest fatigue %.3f, plain %.3f", got, was)
	}
	at28 := skip[16] // week 4
	if at28.OffsetDays != 28 || muscle(at28.Load, "quads").Strength >= 1 {
		t.Errorf("four weeks off: day %d quads strength %.3f", at28.OffsetDays, muscle(at28.Load, "quads").Strength)
	}

	light := Frames(history, plan, weights, now, Scenario{DeloadNow: true})
	if got, was := muscle(light[2].Load, "chest").Fatigue, muscle(plain[2].Load, "chest").Fatigue; got >= was {
		t.Errorf("lighter week: chest fatigue %.3f, plain %.3f", got, was)
	}
	if got, was := muscle(light[day].Load, "chest").Fatigue, muscle(plain[day].Load, "chest").Fatigue; math.Abs(got-was) > 0.02 {
		t.Errorf("lighter week still lighter next week: %.3f vs %.3f", got, was)
	}

	extra := Frames(history, plan, weights, now, Scenario{ExtraSessions: 1})
	more := false
	for i := range extra {
		if muscle(extra[i].Load, "quads").Fatigue > muscle(plain[i].Load, "quads").Fatigue+1e-9 {
			more = true
		}
	}
	if !more {
		t.Error("an extra session adds no fatigue anywhere")
	}

	none := Frames(history, nil, weights, now, Scenario{ExtraSessions: 1})
	if !muscle(none[len(none)-1].Load, "quads").Detrained() {
		t.Error("no plan for twelve weeks and nothing detrains")
	}
}

func TestParseReps(t *testing.T) {
	cases := []struct {
		text string
		want RepRange
		ok   bool
	}{
		{"8-12", RepRange{8, 12}, true},
		{"8–12", RepRange{8, 12}, true},
		{"6 to 8 reps", RepRange{6, 8}, true},
		{"5", RepRange{5, 5}, true},
		{"3x8-10", RepRange{8, 10}, true},
		{"4 × 12", RepRange{12, 12}, true},
		{"12-8", RepRange{8, 12}, true},
		{"AMRAP", RepRange{}, false},
		{"", RepRange{}, false},
		{"0", RepRange{}, false},
	}
	for _, c := range cases {
		got, ok := ParseReps(c.text)
		if got != c.want || ok != c.ok {
			t.Errorf("%q: %+v %v", c.text, got, ok)
		}
	}
}

func withRIR(sets []lift.Set, rir int) []lift.Set {
	for i := range sets {
		sets[i].RIR = &rir
	}
	return sets
}

func TestSuggestNext(t *testing.T) {
	first := sessionSets("bench", 7, 80, 10, 10, 9)
	rng := RepRange{8, 12}
	cases := []struct {
		name    string
		last    []lift.Set
		lighter bool
		weight  float64
		reps    int
		reason  string
	}{
		{"top of range with reps to spare", withRIR(sessionSets("bench", 1, 80, 12, 12, 12), 2), false, 82.5, 8, NextAddLoad},
		{"top of range, effort unknown", sessionSets("bench", 1, 80, 12, 12, 12), false, 80, 12, NextAddRep},
		{"top of range to failure", withRIR(sessionSets("bench", 1, 80, 12, 12, 12), 0), false, 80, 12, NextAddRep},
		{"mid range", sessionSets("bench", 1, 80, 10, 9, 9), false, 80, 10, NextAddRep},
		{"under the range", sessionSets("bench", 1, 80, 8, 7, 6), false, 80, 8, NextRepeat},
		{"lighter day", withRIR(sessionSets("bench", 1, 80, 12, 12, 12), 2), true, 72.5, 8, NextLighter},
		{"back-off sets do not count", join(withRIR(sessionSets("bench", 1, 80, 12, 12), 1), sessionSets("bench", 1, 60, 9)), false, 82.5, 8, NextAddLoad},
	}
	for _, c := range cases {
		got, ok := SuggestNext(NextSetInput{Sets: join(first, c.last), Range: rng, Lighter: c.lighter, Now: now})
		if !ok || got.WeightKg != c.weight || got.Reps != c.reps || got.Reason != c.reason {
			t.Errorf("%s: %+v %v", c.name, got, ok)
		}
	}

	if _, ok := SuggestNext(NextSetInput{Sets: first, Range: rng, Now: now}); ok {
		t.Error("suggestion from one session")
	}
	body := join(sessionSets("pull-up", 7, 0, 8), sessionSets("pull-up", 1, 0, 9))
	if _, ok := SuggestNext(NextSetInput{Sets: body, Now: now}); ok {
		t.Error("load suggested for a bodyweight lift")
	}
	// A 2.5 kg step on a 15 kg lift is a sixth of it: reps instead.
	light := join(sessionSets("raise", 7, 15, 12), withRIR(sessionSets("raise", 1, 15, 12, 12), 2))
	got, _ := SuggestNext(NextSetInput{Sets: light, Range: rng, Now: now})
	if got.WeightKg != 15 || got.Reason != NextAddRep || got.Reps != 13 {
		t.Errorf("small lift: %+v", got)
	}
}

// Same input, same output, byte for byte: nothing here may depend on map
// order or the clock.
func TestDeterministic(t *testing.T) {
	sets := join(liftSets("squat", 30, 2, 140, 0.05), liftSets("bench", 30, 2, 100, 0.02), liftSets("row", 30, 1, 90, 0))
	run := func() string {
		var out string
		for _, h := range LiftHistories(sets) {
			out += fmt.Sprintf("%+v\n", ForecastLift(h, now, Pause{Days: 7}))
		}
		out += fmt.Sprintf("%+v\n", Frames(sets, plan, weights, now, Scenario{ExtraSessions: 1}))
		out += fmt.Sprintf("%v %v\n", WeeksSinceLighter(sets, now, time.UTC), RepsFalling(sets, now))
		return out
	}
	want := run()
	for i := 0; i < 20; i++ {
		if got := run(); got != want {
			t.Fatal("output differs between runs")
		}
	}
}
