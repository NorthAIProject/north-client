package progresscheck_test

import (
	"strings"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/bodymap"
	"github.com/NorthAIProject/north-client/internal/progresschecks/progresscheck"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

func ptr(v float64) *float64 { return &v }

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

func TestCadenceNext(t *testing.T) {
	t.Parallel()
	on := day(2026, 10, 15)
	cases := map[progresscheck.Cadence]time.Time{
		progresscheck.Weekly:   day(2026, 10, 22),
		progresscheck.Biweekly: day(2026, 10, 29),
		progresscheck.Monthly:  day(2026, 11, 15),
	}
	for c, want := range cases {
		if got := c.Next(on); !got.Equal(want) {
			t.Errorf("%s.Next = %s, want %s", c, got.Format(time.DateOnly), want.Format(time.DateOnly))
		}
	}
}

func TestScheduleDue(t *testing.T) {
	t.Parallel()
	s := progresscheck.Schedule{Cadence: progresscheck.Weekly, NextDueOn: day(2026, 10, 22)}
	if s.Due(day(2026, 10, 21)) {
		t.Error("due the day before")
	}
	if !s.Due(day(2026, 10, 22)) || !s.Due(day(2026, 11, 3)) {
		t.Error("not due on or after the date: a skipped window stays due")
	}
	if (progresscheck.Schedule{}).Due(day(2030, 1, 1)) {
		t.Error("a schedule never set is never due")
	}
}

func TestValidate(t *testing.T) {
	t.Parallel()
	today := day(2026, 10, 15)

	got, err := progresscheck.Validate(progresscheck.Input{
		Measurements: progresscheck.Measurements{WaistCm: ptr(96.04), WeightKg: ptr(100)},
	}, today)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !got.MeasuredOn.Equal(today) || *got.WaistCm != 96 || got.Source != progresscheck.SourceManual {
		t.Errorf("defaults/rounding wrong: %+v waist=%v", got, *got.WaistCm)
	}

	_, err = progresscheck.Validate(progresscheck.Input{
		MeasuredOn:   day(2026, 10, 16),
		Measurements: progresscheck.Measurements{WaistCm: ptr(12)},
	}, today)
	var fe apperr.FieldErrors
	if !apperr.As(err, &fe) {
		t.Fatalf("want field errors, got %v", err)
	}
	msgs := fe.Messages()
	if msgs["measured_on"] == "" || msgs["waist_cm"] == "" {
		t.Errorf("missing field errors: %v", msgs)
	}

	manual, err := progresscheck.Validate(progresscheck.Input{
		Source: progresscheck.SourceManual, AssistConfidence: "high", AssistNotes: "x",
	}, today)
	if err != nil || manual.AssistConfidence != "" || manual.AssistNotes != "" {
		t.Errorf("manual check kept assist fields: %+v %v", manual, err)
	}
}

func TestShape(t *testing.T) {
	t.Parallel()
	base := bodymap.Baseline{
		Waist: bodymap.Girth{From: 0.40, To: 0.70},
		Hips:  bodymap.Girth{From: 0.50, To: 0.80},
		Chest: bodymap.Girth{From: 0.50, To: 0.80},
	}

	tests := []struct {
		name   string
		m      progresscheck.Measurements
		height float64
		bmi    float64
		want   bodymap.Shape
	}{
		{name: "nothing measured", want: bodymap.Shape{}},
		{
			name: "waist halfway", m: progresscheck.Measurements{WaistCm: ptr(110)}, height: 200,
			want: bodymap.Shape{Waist: 0.5},
		},
		{
			name: "below the mesh clamps to zero", m: progresscheck.Measurements{WaistCm: ptr(60), HipsCm: ptr(70)}, height: 180,
			want: bodymap.Shape{},
		},
		{
			name: "far above clamps to one", m: progresscheck.Measurements{ChestCm: ptr(240)}, height: 150,
			want: bodymap.Shape{Chest: 1},
		},
		{
			name: "missing height uses the reference", m: progresscheck.Measurements{WaistCm: ptr(85)},
			want: bodymap.Shape{Waist: 0.333},
		},
		{name: "overall is soft and capped", bmi: 50, want: bodymap.Shape{Overall: 0.4}},
		{name: "overall starts above 22", bmi: 29.5, want: bodymap.Shape{Overall: 0.2}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := progresscheck.Shape(tt.m, tt.height, tt.bmi, base); got != tt.want {
				t.Errorf("Shape = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestSummary(t *testing.T) {
	t.Parallel()
	checks := []progresscheck.Check{
		{
			MeasuredOn: day(2026, 10, 15), Source: progresscheck.SourceAssisted,
			Measurements: progresscheck.Measurements{WaistCm: ptr(96), WeightKg: ptr(99)}, WeightCarried: true,
		},
		{MeasuredOn: day(2026, 10, 8), Measurements: progresscheck.Measurements{WaistCm: ptr(97)}},
		{MeasuredOn: day(2026, 10, 1), Measurements: progresscheck.Measurements{WaistCm: ptr(98), WeightKg: ptr(101)}},
	}
	got := progresscheck.Summary(checks)
	for _, want := range []string{"15 Oct 2026", "waist 96 cm (−2 since 1 Oct)", "approximate"} {
		if !strings.Contains(got, want) {
			t.Errorf("Summary = %q, missing %q", got, want)
		}
	}
	if strings.Contains(got, "weight") {
		t.Errorf("a carried weight is not a reading: %q", got)
	}
	if progresscheck.Summary(nil) != "" {
		t.Error("no checks should say nothing")
	}
}
