package stat

import (
	"strings"
	"testing"
	"time"
)

// everyPattern is a run of days varied enough that every split finds a
// difference, so the catalogue is checked against each sentence Patterns can
// write.
func everyPattern(t *testing.T) []Finding {
	t.Helper()
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// A fixed linear congruential sequence: varied enough that no two groups
	// average out level, and the same on every run.
	seed := uint32(13)
	next := func(n int) int {
		seed = seed*1664525 + 1013904223
		return int(seed>>16) % n
	}
	var days []DayFacts
	for i := range 90 {
		sleep := 360 + next(180)
		screen := 60 + next(300)
		energy := 1 + next(5)
		mood := 1 + next(5)
		steps := float64(4000 + next(9000))
		daylight := float64(10 + next(90))
		stand := float64(4 + next(10))
		hrv := float64(40 + next(30))
		rhr := float64(50 + next(15))
		mindful := float64(next(2) * 10)
		days = append(days, DayFacts{
			Date:         start.AddDate(0, 0, i),
			SleepMinutes: &sleep, ScreenMin: &screen, Energy: &energy, Mood: &mood,
			LateCaffeine: next(3) == 0, HadCaffeine: true,
			LateEating: next(4) == 0, AteLogged: true,
			Trained: next(2) == 0, OutdoorWorkout: next(4) == 0,
			Steps: &steps, DaylightMin: &daylight, StandHours: &stand,
			HRV: &hrv, RestingHR: &rhr, MindfulMin: &mindful,
		})
	}
	found := Patterns(days)
	if len(found) != 14 {
		keys := make([]string, len(found))
		for i, f := range found {
			keys[i] = f.Key
		}
		t.Fatalf("fixture finds %d patterns (%v); it should exercise all 14 splits", len(found), keys)
	}
	return found
}

// The coach reads Patterns' own English; a person reads the catalogue's. The
// two must not drift, or an English reader sees a different sentence on the
// page from the one the coach quotes back to them.
func TestCatalogueEnglishMatchesPatterns(t *testing.T) {
	for _, f := range everyPattern(t) {
		got := f.In("en")
		if got.Title != f.Title || got.Detail != f.Detail || got.A.Label != f.A.Label || got.B.Label != f.B.Label {
			t.Errorf("%s: catalogue English drifted from Patterns:\n got %q / %q / %q / %q\nwant %q / %q / %q / %q",
				f.Key, got.Title, got.Detail, got.A.Label, got.B.Label, f.Title, f.Detail, f.A.Label, f.B.Label)
		}
	}
}

func TestFindingsReadInTheReadersLanguage(t *testing.T) {
	for _, tag := range []string{"es", "pt-PT", "pt-BR"} {
		for _, f := range everyPattern(t) {
			got := f.In(tag)
			for name, s := range map[string]string{"title": got.Title, "detail": got.Detail, "a": got.A.Label, "b": got.B.Label} {
				if strings.Contains(s, "stats.") || strings.Contains(s, "%!") {
					t.Errorf("%s %s %s is broken: %q", tag, f.Key, name, s)
				}
			}
			if got.Title == f.Title || got.Detail == f.Detail {
				t.Errorf("%s %s is still English: %q", tag, f.Key, got.Title)
			}
			// Numbers, keys and units are data, not copy.
			if got.Key != f.Key || got.Unit != f.Unit || got.Diff != f.Diff || got.A.Mean != f.A.Mean || got.B.N != f.B.N {
				t.Errorf("%s %s changed its data", tag, f.Key)
			}
		}
	}
}

func TestFindingUsesTheReadersNotation(t *testing.T) {
	f := Finding{
		Key: "mood_steps", Unit: "/5", Diff: 0.6, threshold: 11000,
		A: Group{Mean: 3.8, N: 12}, B: Group{Mean: 3.2, N: 15},
	}
	cases := map[string]string{
		"en":    "Your mood is 0.6 points higher on days with 11,000 steps or more",
		"es":    "Tu ánimo es 0,6 puntos más alto los días con 11.000 pasos o más",
		"pt-PT": "O teu humor é 0,6 pontos mais alto nos dias com 11.000 passos ou mais",
	}
	for tag, want := range cases {
		if got := f.In(tag).Title; got != want {
			t.Errorf("%s: title = %q, want %q", tag, got, want)
		}
	}

	f.Diff = -0.6
	if got, want := f.In("pt-BR").Title, "Seu humor fica 0,6 pontos mais baixo nos dias com 11.000 passos ou mais"; got != want {
		t.Errorf("pt-BR lower: title = %q, want %q", got, want)
	}
	if got, want := f.In("es").A.Label, "Días con 11.000 pasos o más"; got != want {
		t.Errorf("es group label = %q, want %q", got, want)
	}
}

func TestUnknownFindingIsLeftAlone(t *testing.T) {
	f := Finding{Key: "not_a_split", Title: "As written", Detail: "Detail."}
	if got := f.In("es"); got.Title != "As written" || got.Detail != "Detail." {
		t.Errorf("unknown finding was rewritten: %+v", got)
	}
}

func TestBestsReadInTheReadersLanguage(t *testing.T) {
	at := time.Date(2026, 3, 1, 7, 0, 0, 0, time.UTC)
	run := func(day, mins int, km float64) Session {
		return Session{Name: "Running", At: at.AddDate(0, 0, day), Seconds: mins * 60, DistanceM: km * 1000}
	}
	k := KindDetail("Running", []Session{run(0, 30, 5), run(7, 62, 10.5)})
	if len(k.Bests) == 0 {
		t.Fatal("fixture has no bests")
	}

	if en := k.In("en"); en.Bests[0].Label != k.Bests[0].Label {
		t.Errorf("English best label drifted: %q, want %q", en.Bests[0].Label, k.Bests[0].Label)
	}

	es := k.In("es")
	labels := map[string]string{}
	for _, b := range es.Bests {
		labels[b.Key] = b.Label
	}
	if labels["fastest_5k"] != "5 km más rápidos" || labels["longest"] != "Carrera más larga" {
		t.Errorf("es bests = %v", labels)
	}
	if es.Name != "Running" {
		t.Errorf("kind name is an identifier and must stay English, got %q", es.Name)
	}
	if k.Bests[0].Label == es.Bests[0].Label {
		t.Error("In changed the original's bests in place")
	}
}
