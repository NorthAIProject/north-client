package main

import (
	"testing"

	"github.com/NorthAIProject/north-client/internal/nudges/nudge"
	"github.com/NorthAIProject/north-client/internal/shared/simulate"
)

// The simulate package spells nudge kinds as plain strings rather than
// importing internal/nudges/nudge, because it lives under internal/shared and
// shared depending on a slice would invert the layering the codebase is
// arranged around.
//
// This test is the price of that choice: it lives here, in the one package that
// already imports both, and it fails if the two vocabularies drift. Without it
// a renamed kind would surface as a CHECK constraint violation halfway through
// a sixteen-week run.
func TestSimulatedNudgeKindsMatchTheRealOnes(t *testing.T) {
	t.Parallel()

	known := map[string]bool{
		nudge.KindMissedCheckIn: true,
		nudge.KindWorkoutToday:  true,
		nudge.KindBriefingReady: true,
	}

	people, err := simulate.Generate(simulate.Options{Users: len(simulate.Personas), Weeks: 8, Seed: 1})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	seen := map[string]bool{}
	for _, p := range people {
		for _, d := range p.Days {
			for _, n := range d.Nudges {
				if !known[n.Kind] {
					t.Errorf("simulate raises nudge kind %q, which is not a kind internal/nudges/nudge defines", n.Kind)
				}
				seen[n.Kind] = true
			}
		}
	}

	// The other direction: if simulate stops raising a kind it used to, the
	// nudge-effectiveness work silently loses a case it was relying on.
	for kind := range known {
		if !seen[kind] {
			t.Errorf("no simulated nudge of kind %q was raised across the whole catalog", kind)
		}
	}
}

// Every kind the writer inserts must have a title, or a simulated nudge lands
// in the bell with the fallback text and looks like a bug in the engine.
func TestEverySimulatedNudgeKindHasATitle(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{
		nudge.KindMissedCheckIn,
		nudge.KindWorkoutToday,
		nudge.KindBriefingReady,
	} {
		if got := simulatedNudgeTitle(kind); got == "" || got == "Nudge" {
			t.Errorf("simulatedNudgeTitle(%q) = %q, want a real title", kind, got)
		}
	}
}
