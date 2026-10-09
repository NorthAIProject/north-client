package hevy_test

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/lifts/hevy"
)

func TestParseReadsSetsKindsAndEffort(t *testing.T) {
	t.Parallel()
	f, err := os.Open("testdata/workouts.csv")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	lisbon, _ := time.LoadLocation("Europe/Lisbon")

	got, err := hevy.Parse(f, lisbon)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Workouts) != 2 {
		t.Fatalf("workouts = %d, want 2", len(got.Workouts))
	}
	// The treadmill row has no reps; the 900 kg squat is a typo.
	if got.Skipped != 2 {
		t.Errorf("skipped = %d, want 2", got.Skipped)
	}

	push := got.Workouts[0]
	if push.Title != "Push Day" || !push.Start.Equal(time.Date(2026, 10, 8, 18, 5, 0, 0, lisbon)) || push.End.Sub(push.Start) != 65*time.Minute {
		t.Errorf("push = %q %v–%v", push.Title, push.Start, push.End)
	}
	if len(push.Sets) != 5 {
		t.Fatalf("push sets = %d, want 5", len(push.Sets))
	}
	want := []struct {
		kind   string
		number int
		kg     float64
		rir    int // -1 for none
	}{
		{hevy.KindWarmup, 1, 40, -1},
		{hevy.KindWork, 2, 80, 2}, // RPE 8
		{hevy.KindWork, 3, 80, 0}, // failure
		{hevy.KindDrop, 4, 60, -1},
		{hevy.KindWork, 1, 0, -1}, // bodyweight pull-up, its own numbering
	}
	for i, w := range want {
		s := push.Sets[i]
		rir := -1
		if s.RIR != nil {
			rir = *s.RIR
		}
		if s.Kind != w.kind || s.Number != w.number || s.WeightKg != w.kg || rir != w.rir {
			t.Errorf("set %d = %+v (rir %d), want %+v", i, s, rir, w)
		}
	}
	if leg := got.Workouts[1]; len(leg.Sets) != 1 || *leg.Sets[0].RIR != 1 { // RPE 9.5 rounds to 1 in reserve
		t.Errorf("leg day = %+v", leg)
	}
}

func TestParseConvertsPounds(t *testing.T) {
	t.Parallel()
	csv := "title,start_time,end_time,exercise_title,set_type,weight_lbs,reps\n" +
		"A,1 Oct 2026 10:00,1 Oct 2026 11:00,Deadlift (Barbell),normal,225,5\n"
	got, err := hevy.Parse(strings.NewReader(csv), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if kg := got.Workouts[0].Sets[0].WeightKg; kg != 102.1 {
		t.Errorf("225 lb = %v kg, want 102.1", kg)
	}
}

func TestParseRejectsOtherFiles(t *testing.T) {
	t.Parallel()
	for _, csv := range []string{
		"Date,Workout Name,Exercise Name,Set Order,Weight,Reps\n", // Strong's header
		"",
	} {
		if _, err := hevy.Parse(strings.NewReader(csv), time.UTC); !errors.Is(err, hevy.ErrNotHevy) {
			t.Errorf("%q: err = %v, want ErrNotHevy", csv, err)
		}
	}
}
