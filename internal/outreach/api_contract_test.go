package outreach_test

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/outreach"
	"github.com/NorthAIProject/north-client/internal/shared/apitest"
)

func TestOutreachShape(t *testing.T) {
	t.Parallel()
	o := outreach.Outreach{
		ID:      uuid.MustParse("55555555-5555-5555-5555-555555555555"),
		Day:     time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC),
		Trigger: outreach.TriggerMissedSession,
		Facts:   outreach.Facts{Session: "Upper B", Weekday: "Thursday", TodayFree: true},
		Body:    "Thursday's Upper B didn't happen. Today is free if you want it, or let it go.",
	}
	r := httptest.NewRequest("GET", "/", nil)
	apitest.AssertGolden(t, "outreach_open.golden.json", outreach.OutreachToday{Outreach: ptr(outreach.Project(r, o))})

	acted := time.Date(2026, 10, 16, 9, 12, 0, 0, time.UTC)
	o.Action, o.ActedAt = outreach.ActionToday, &acted
	apitest.AssertGolden(t, "outreach_answered.golden.json", outreach.Project(r, o))
}

func ptr[T any](v T) *T { return &v }
