package supplements_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/shared/apitest"
	"github.com/NorthAIProject/north-client/internal/supplements"
)

func TestSupplementsTodayShape(t *testing.T) {
	t.Parallel()
	apitest.AssertGolden(t, "supplements.golden.json", supplements.ProjectToday([]supplements.Entry{{
		ID: uuid.MustParse("33333333-3333-3333-3333-333333333333"), Name: "Omega-3 (EPA/DHA)", Count: 3,
		Nutrients: []string{"omega3"}, LoggedAt: time.Date(2026, 9, 26, 10, 16, 0, 0, time.UTC),
	}}))
}
