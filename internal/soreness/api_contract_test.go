package soreness_test

import (
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/shared/apitest"
	"github.com/NorthAIProject/north-client/internal/soreness"
)

func TestSorenessHistoryShape(t *testing.T) {
	t.Parallel()
	day := func(d int) time.Time { return time.Date(2026, 11, d, 0, 0, 0, 0, time.UTC) }
	apitest.AssertGolden(t, "soreness_history.golden.json", soreness.ProjectHistory(14, day(15), []soreness.Entry{
		{LogDate: day(15), Region: "quads", Severity: 2},
		{LogDate: day(14), Region: "quads", Severity: 3, Note: "leg day"},
		{LogDate: day(12), Region: "knees", Severity: 1},
	}))
}

func TestSorenessTodayShape(t *testing.T) {
	t.Parallel()
	apitest.AssertGolden(t, "soreness.golden.json", soreness.ProjectToday([]soreness.Entry{
		{Region: "quads", Severity: 2}, {Region: "lower_back", Severity: 1, Note: "desk day"},
	}))
}
