package insights

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/stats/stat"
)

// The usual range's sentence is read, so it takes the reader's decimal mark;
// its Low and High are the API's string fields, which keep a point for every
// client.
func TestUsualRangeReadsInTheReadersMarkAndTheAPIKeepsAPoint(t *testing.T) {
	m := metric{Key: "vo2max", Label: "VO2 max", Decimals: 1}
	u := Usual{
		Latest:   point{At: time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC), Value: 48.6},
		Baseline: stat.Baseline{Mean: 46.2, SD: 1.1, Days: 28},
	}

	es := usualView(i18n.WithLocale(context.Background(), "es"), m, u)
	if es.Low != "45.1" || es.High != "47.3" {
		t.Errorf("API range = %q–%q, want 45.1–47.3", es.Low, es.High)
	}
	for _, want := range []string{"48,6", "45,1", "47,3"} {
		if !strings.Contains(es.Text, want) {
			t.Errorf("Spanish sentence %q lacks %q", es.Text, want)
		}
	}

	en := usualView(context.Background(), m, u)
	if !strings.Contains(en.Text, "48.6") {
		t.Errorf("English sentence %q lacks 48.6", en.Text)
	}
	if got := readMetric(i18n.WithLocale(context.Background(), "pt-PT"), m, 48.62); got != "48,6" {
		t.Errorf("readMetric(pt-PT) = %q, want 48,6", got)
	}
}
