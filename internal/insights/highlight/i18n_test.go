package highlight

import (
	"strings"
	"testing"
)

// A reader with a locale gets the sentences in their language, built around
// labels that arrive already translated.
func TestFindWritesInTheInputLocale(t *testing.T) {
	got := Find(Input{
		Locale: "es",
		Series: []Series{{
			Label: "Agua", Unit: "L", Decimals: 1, Period: "day",
			Labels: []string{"1 oct", "2 oct", "3 oct", "4 oct", "5 oct", "6 oct", "7 oct"},
			Values: []float64{2, 0, 0, 0, 0, 2, 2},
		}},
		Comparisons: []Comparison{{Label: "Gasto calórico", Current: 1200, Prior: 800}},
	}, 3)

	joined := strings.Join(got, " | ")
	for _, want := range []string{"Estuviste 4 días sin registrar agua.", "Gasto calórico: un 50% más que en el periodo anterior."} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in %s", want, joined)
		}
	}
	for _, leak := range []string{"unlogged", "higher", "days"} {
		if strings.Contains(joined, leak) {
			t.Errorf("English %q leaked into %s", leak, joined)
		}
	}
}

// lower must handle a label that starts with a multi-byte letter: "Água"
// mid-sentence is "água", not a broken byte.
func TestLowerHandlesAccentedLabels(t *testing.T) {
	if got := lower("Água"); got != "água" {
		t.Errorf("lower(Água) = %q", got)
	}
	if got := lower("HRV"); got != "HRV" {
		t.Errorf("lower(HRV) = %q, an acronym must stay as it is", got)
	}
}
