package meal

import (
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
)

// active is a 150 g protein / 70 g fat / 200 g carb target.
var active = MacrosFromGrams(150, 70, 200)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func f64(v float64) *float64 { return &v }

func planType(t PlanType) *PlanType { return &t }

func TestBandDefaultsAreTheirMidpoints(t *testing.T) {
	t.Parallel()

	for typ, want := range map[PlanType]float64{NoCarb: 2.5, LowCarb: 15.5, MidCarb: 35.5, HighCarb: 55.5} {
		b, ok := BandFor(typ)
		if !ok {
			t.Fatalf("%s has no band", typ)
		}
		if !near(b.DefaultPct(), want) || !near(b.CarbG(200), want*2) {
			t.Errorf("%s: default %v%% = %v g, want %v%% = %v g", typ, b.DefaultPct(), b.CarbG(200), want, want*2)
		}
	}
	if _, ok := BandFor(Custom); ok {
		t.Error("custom has a preset band")
	}
	if !Custom.Valid() || PlanType("keto").Valid() || !Easy.Valid() || Mode("expert").Valid() {
		t.Error("Valid accepts the wrong values")
	}
}

func TestResolveDayTarget(t *testing.T) {
	t.Parallel()

	mid := PlanSettings{Type: MidCarb, Mode: Advanced}
	for _, tc := range []struct {
		name    string
		plan    PlanSettings
		day     DayOverride
		p, f, c float64
	}{
		{name: "plan band", plan: mid, p: 150, f: 70, c: 71},
		{name: "custom share", plan: PlanSettings{Type: Custom, CustomCarbPct: f64(30), Mode: Advanced}, p: 150, f: 70, c: 60},
		{name: "lower-carb day", plan: mid, day: DayOverride{CarbType: planType(LowCarb)}, p: 150, f: 70, c: 31},
		{name: "higher-carb day", plan: mid, day: DayOverride{CarbType: planType(HighCarb)}, p: 150, f: 70, c: 111},
		{name: "carb grams", plan: mid, day: DayOverride{CarbG: f64(120)}, p: 150, f: 70, c: 120},
		{name: "protein and fat", plan: mid, day: DayOverride{ProteinG: f64(120), FatG: f64(50)}, p: 120, f: 50, c: 71},
		{
			name: "capped at the active target", plan: mid,
			day: DayOverride{CarbG: f64(260), ProteinG: f64(180), FatG: f64(90)}, p: 150, f: 70, c: 200,
		},
	} {
		got := ResolveDayTarget(active, tc.plan, tc.day)
		want := MacrosFromGrams(tc.p, tc.f, tc.c)
		if !near(got.ProteinG, want.ProteinG) || !near(got.FatG, want.FatG) || !near(got.CarbG, want.CarbG) || !near(got.Calories, want.Calories) {
			t.Errorf("%s: target = %+v, want %+v", tc.name, got, want)
		}
	}
}

func TestStatusIsSignedAndToleratesRounding(t *testing.T) {
	t.Parallel()

	st := StatusOf(MacrosFromGrams(100, 50, 80), MacrosFromGrams(60, 50.4, 95))
	if !near(st.Remaining.ProteinG, 40) || !near(st.Remaining.CarbG, -15) || !near(st.Over.CarbG, 15) || st.Over.ProteinG != 0 {
		t.Fatalf("status = %+v", st)
	}
	if !st.IsOver() {
		t.Error("15 g over on carbs is not over")
	}
	if StatusOf(MacrosFromGrams(100, 50, 80), MacrosFromGrams(100.4, 50, 80)).IsOver() {
		t.Error("0.4 g past a target counts as over")
	}
}

func TestCheckWrite(t *testing.T) {
	t.Parallel()

	monday, saturday := uuid.New(), uuid.New()
	state := func(mode Mode, mondayCarbs, saturdayCarbs float64) PlanState {
		return PlanState{
			Settings: PlanSettings{Type: MidCarb, Mode: mode}, // 71 g carbs a day
			Days: []DayState{
				{ID: monday, Weekday: time.Monday, Consumed: MacrosFromGrams(0, 0, mondayCarbs)},
				{ID: saturday, Weekday: time.Saturday, Consumed: MacrosFromGrams(0, 0, saturdayCarbs)},
			},
		}
	}
	ptr := func(s PlanState) *PlanState { return &s }

	for _, tc := range []struct {
		name       string
		before     *PlanState
		after      PlanState
		confirm    bool
		allowed    bool
		canConfirm bool
		over       []uuid.UUID
	}{
		{name: "within target", before: ptr(state(Easy, 0, 0)), after: state(Easy, 70, 0), allowed: true},
		{name: "easy goes over", before: ptr(state(Easy, 60, 0)), after: state(Easy, 80, 0), over: []uuid.UUID{monday}},
		{name: "easy ignores confirmation", before: ptr(state(Easy, 60, 0)), after: state(Easy, 80, 0), confirm: true, over: []uuid.UUID{monday}},
		{
			name: "advanced asks", before: ptr(state(Advanced, 60, 0)), after: state(Advanced, 80, 0),
			canConfirm: true, over: []uuid.UUID{monday},
		},
		{
			name: "advanced confirmed", before: ptr(state(Advanced, 60, 0)), after: state(Advanced, 80, 0),
			confirm: true, allowed: true, over: []uuid.UUID{monday},
		},
		{name: "over day shrinks", before: ptr(state(Advanced, 90, 0)), after: state(Advanced, 80, 0), allowed: true},
		{name: "over day unchanged", before: ptr(state(Advanced, 90, 0)), after: state(Advanced, 90, 30), allowed: true},
		{name: "entering easy while over", before: nil, after: state(Easy, 90, 0), over: []uuid.UUID{monday}},
		{
			name: "several days", before: ptr(state(Advanced, 0, 0)), after: state(Advanced, 80, 100),
			canConfirm: true, over: []uuid.UUID{monday, saturday},
		},
	} {
		v := CheckWrite(active, tc.before, tc.after, tc.confirm)
		if v.Allowed != tc.allowed || v.CanConfirm != tc.canConfirm || len(v.Over) != len(tc.over) {
			t.Errorf("%s: verdict = %+v", tc.name, v)
			continue
		}
		for i, id := range tc.over {
			if v.Over[i].DayID != id {
				t.Errorf("%s: over day %d = %v, want %v", tc.name, i, v.Over[i].DayID, id)
			}
		}
	}
}

func TestWeekdays(t *testing.T) {
	t.Parallel()

	if got := EasyWeekdays(7); got[0] != time.Monday || got[6] != time.Sunday {
		t.Errorf("a week = %v, want Monday to Sunday", got)
	}
	if got := EasyWeekdays(2); len(got) != 2 || got[1] != time.Tuesday {
		t.Errorf("two days = %v", got)
	}
	if wd, ok := NextFreeWeekday([]time.Weekday{time.Monday, time.Wednesday}); !ok || wd != time.Tuesday {
		t.Errorf("next free = %v, %v; want Tuesday", wd, ok)
	}
	if _, ok := NextFreeWeekday(EasyWeekdays(7)); ok {
		t.Error("a full week has a free day")
	}
}
