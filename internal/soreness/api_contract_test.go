package soreness_test

import (
	"testing"

	"github.com/NorthAIProject/north-client/internal/shared/apitest"
	"github.com/NorthAIProject/north-client/internal/soreness"
)

func TestSorenessTodayShape(t *testing.T) {
	t.Parallel()
	apitest.AssertGolden(t, "soreness.golden.json", soreness.ProjectToday([]soreness.Entry{
		{Region: "quads", Severity: 2}, {Region: "lower_back", Severity: 1, Note: "desk day"},
	}))
}
