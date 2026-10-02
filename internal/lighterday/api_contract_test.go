package lighterday

import (
	"testing"

	"github.com/NorthAIProject/north-client/internal/reports"
	"github.com/NorthAIProject/north-client/internal/shared/apitest"
)

func TestLighterShape(t *testing.T) {
	t.Parallel()

	apitest.AssertGolden(t, "today-lighter.golden.json", project(Today{
		Readiness: reports.Readiness{HRV: 38, HRVBaseline: 52, RHR: 61, RHRBaseline: 56, HasHRV: true, HasRHR: true, Low: true},
		Session:   "Lower body",
		Due:       true,
	}))
}
