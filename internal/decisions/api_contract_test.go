package decisions

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/shared/apitest"
)

func TestDecisionShapes(t *testing.T) {
	t.Parallel()

	apitest.AssertGolden(t, "decisions.golden.json", DecisionList{Decisions: []DecisionView{{
		ID: uuid.MustParse("66666666-aaaa-aaaa-aaaa-666666666666"), Title: "Run the half in December or March",
		Options: "December: sooner, colder. March: more base.", Rationale: "The knee needs the extra weeks.",
		DecidedAt: time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC),
	}}})
}
