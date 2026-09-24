package media

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/shared/apitest"
)

func TestFormCheckShapes(t *testing.T) {
	t.Parallel()

	done := FormCheckView{
		ID: uuid.MustParse("d0d0d0d0-d0d0-d0d0-d0d0-d0d0d0d0d0d0"), Status: "done",
		CreatedAt: time.Date(2026, 9, 23, 18, 0, 0, 0, time.UTC),
		Result: &FormResultView{
			Exercise: "Goblet squat", Confidence: "high", Summary: "Depth is good; knees drift in on the way up.",
			Issues: []FormIssueView{{At: 4.5, Severity: "medium", Observation: "Knees move inward as you stand.", Correction: "Push the knees out over the toes."}},
		},
	}
	apitest.AssertGolden(t, "form-checks.golden.json", FormCheckList{Checks: []FormCheckView{done}})
	apitest.AssertGolden(t, "form-check.golden.json", FormCheckView{ID: done.ID, Status: "running", CreatedAt: done.CreatedAt})
}
