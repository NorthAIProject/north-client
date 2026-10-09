package lifts

import (
	"testing"

	pages "github.com/NorthAIProject/north-client/web/lifts"
)

func TestParseSetForm(t *testing.T) {
	t.Parallel()
	in, msg := parseSetForm(pages.SetForm{Exercise: "Bench", WeightKg: "82,5", Reps: "5", Kind: "warmup", RIR: "2"})
	if msg != "" || in.WeightKg != 82.5 || in.Reps != 5 || in.Kind != "warmup" || in.RIR == nil || *in.RIR != 2 {
		t.Errorf("parsed = %+v, %q", in, msg)
	}
	if in, msg = parseSetForm(pages.SetForm{Exercise: "Pull-up", Reps: "8"}); msg != "" || in.WeightKg != 0 || in.RIR != nil {
		t.Errorf("bodyweight, no effort = %+v, %q", in, msg)
	}
	for _, bad := range []pages.SetForm{
		{Reps: "5"},
		{Exercise: "Bench", Reps: "five"},
		{Exercise: "Bench", WeightKg: "heavy", Reps: "5"},
		{Exercise: "Bench", Reps: "5", RIR: "a bit"},
	} {
		if _, msg := parseSetForm(bad); msg == "" {
			t.Errorf("%+v accepted", bad)
		}
	}
}
