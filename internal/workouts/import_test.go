package workouts_test

import (
	"context"
	"testing"

	"github.com/NorthAIProject/north-client/internal/ai/fake"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/workouts"
)

func importedPlan() workouts.Plan {
	return workouts.Plan{
		Name: "Coach Sam — Block 3",
		Days: []workouts.PlanDay{
			{Weekday: "monday", Exercises: []workouts.Exercise{
				{Name: "Back Squat", Sets: 3, Reps: "5", Load: "100 kg", RestSeconds: 180, FormCues: "brace hard"},
				// Nothing stated but the name: stays that way.
				{Name: "Romanian Deadlift"},
			}},
			{Weekday: "Thursday", Exercises: []workouts.Exercise{{Name: "Bench Press", Reps: "AMRAP"}}},
		},
	}
}

func TestImportPlanStoresThePlanAsWrittenAndCallsNoModel(t *testing.T) {
	client := fake.Text("")
	svc, user := newService(t, client)
	ctx := context.Background()

	stored, err := svc.ImportPlan(ctx, user, importedPlan())
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if len(client.Calls()) != 0 {
		t.Fatalf("import called the model %d times", len(client.Calls()))
	}
	if stored.Source != workouts.SourceImported {
		t.Fatalf("source = %q", stored.Source)
	}

	got, problems, err := svc.PlanForDisplay(ctx, stored.ID, user.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(problems) != 0 {
		t.Fatalf("problems = %q, want none: an imported plan has no intake to fall short of", problems)
	}
	if got.Plan.Days[0].Weekday != "Monday" {
		t.Fatalf("weekday = %q, want the canonical spelling", got.Plan.Days[0].Weekday)
	}
	squat := got.Plan.Days[0].Exercises[0]
	if squat.Load != "100 kg" || squat.Sets != 3 || squat.RestSeconds != 180 || squat.FormCues != "brace hard" {
		t.Fatalf("squat = %+v", squat)
	}
	rdl := got.Plan.Days[0].Exercises[1]
	if rdl.Sets != 0 || rdl.Reps != "" || rdl.RestSeconds != 0 || rdl.Load != "" {
		t.Fatalf("rdl = %+v, want nothing filled in", rdl)
	}

	// The placeholder intake must never pre-fill the generator's form.
	if _, err := svc.LatestIntake(ctx, user.ID); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("latest intake err = %v, want not found", err)
	}

	// And the plan edits like any other.
	edited, err := svc.SetPrescription(ctx, user, stored.ID, 0, 1, 3, "8", 90)
	if err != nil {
		t.Fatalf("edit imported plan: %v", err)
	}
	if edited.Plan.Days[0].Exercises[1].Sets != 3 || edited.Plan.Days[0].Exercises[0].Load != "100 kg" {
		t.Fatalf("edited = %+v", edited.Plan.Days[0].Exercises)
	}
}

func TestImportPlanRefusesWhatCannotBeTrainedFrom(t *testing.T) {
	svc, user := newService(t, fake.Text(""))
	ctx := context.Background()

	cases := map[string]func(*workouts.Plan){
		"no name":          func(p *workouts.Plan) { p.Name = " " },
		"unassigned day":   func(p *workouts.Plan) { p.Days[1].Weekday = "Day 2" },
		"duplicate day":    func(p *workouts.Plan) { p.Days[1].Weekday = "Monday" },
		"nameless lift":    func(p *workouts.Plan) { p.Days[0].Exercises[0].Name = "" },
		"empty day":        func(p *workouts.Plan) { p.Days[1].Exercises = nil },
		"negative sets":    func(p *workouts.Plan) { p.Days[0].Exercises[0].Sets = -1 },
		"no training days": func(p *workouts.Plan) { p.Days = nil },
	}
	for name, mutate := range cases {
		p := importedPlan()
		mutate(&p)
		if _, err := svc.ImportPlan(ctx, user, p); !apperr.Is(err, apperr.ErrValidation) {
			t.Errorf("%s: err = %v, want validation", name, err)
		}
	}

	plans, err := svc.ListPlans(ctx, user.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 0 {
		t.Fatalf("%d plans stored by refused imports", len(plans))
	}
}
