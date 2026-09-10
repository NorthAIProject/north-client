package activity

import "testing"

func TestMatchResolvesAnExactCode(t *testing.T) {
	met, candidates := Match("running_9_8kmh", 0)
	if met.Code != "running_9_8kmh" || len(candidates) != 0 {
		t.Fatalf("Match(code) = %q, %d candidates", met.Code, len(candidates))
	}
}

func TestMatchResolvesAnExactName(t *testing.T) {
	met, _ := Match("Hiking", 0)
	if met.Code != "hiking" {
		t.Fatalf("Match(Hiking) = %q", met.Code)
	}
}

func TestMatchIsCaseAndWhitespaceInsensitive(t *testing.T) {
	met, _ := Match("  JUMP rope ", 0)
	if met.Code != "jump_rope" {
		t.Fatalf("Match = %q", met.Code)
	}
}

func TestMatchPicksTheRunningPaceFromSpeed(t *testing.T) {
	cases := map[float64]string{
		7:    "running_8kmh",
		9.5:  "running_9_8kmh",
		11:   "running_11_3kmh",
		14.5: "running_fast",
	}
	for speed, want := range cases {
		met, candidates := Match("run", speed)
		if met.Code != want {
			t.Errorf("Match(run, %.1f km/h) = %q (candidates %d), want %q", speed, met.Code, len(candidates), want)
		}
	}
}

func TestMatchUnderstandsSynonyms(t *testing.T) {
	cases := map[string]string{
		"jog":            "running_8kmh",
		"a jog":          "running_8kmh",
		"went for a run": "running_8kmh",
		"brisk walk":     "walking_brisk",
		"swim":           "swimming_moderate",
		"yoga":           "yoga",
	}
	for name, want := range cases {
		met, candidates := Match(name, 8)
		if met.Code != want {
			t.Errorf("Match(%q) = %q (candidates %d), want %q", name, met.Code, len(candidates), want)
		}
	}
}

func TestMatchWithoutSpeedListsTheRunningFamily(t *testing.T) {
	met, candidates := Match("run", 0)
	if met.Code != "" {
		t.Fatalf("Match(run) resolved to %q without a speed", met.Code)
	}
	if len(candidates) < 4 {
		t.Fatalf("got %d candidates, want the running family", len(candidates))
	}
	for _, c := range candidates {
		if c.Category != "cardio" {
			t.Errorf("candidate %q is not a run", c.Code)
		}
	}
}

func TestMatchPrefersTheGeneralEntryOfAFamily(t *testing.T) {
	met, _ := Match("strength training", 0)
	if met.Code != "strength_training" {
		t.Fatalf("Match(strength training) = %q", met.Code)
	}
	met, _ = Match("weights", 0)
	if met.Code != "strength_training" {
		t.Fatalf("Match(weights) = %q", met.Code)
	}
}

func TestMatchReportsNothingForUnknownNames(t *testing.T) {
	met, candidates := Match("underwater basket weaving", 0)
	if met.Code != "" || len(candidates) != 0 {
		t.Fatalf("Match = %q, %d candidates", met.Code, len(candidates))
	}
	met, candidates = Match("", 0)
	if met.Code != "" || len(candidates) != 0 {
		t.Fatalf("Match(empty) = %q, %d candidates", met.Code, len(candidates))
	}
}
