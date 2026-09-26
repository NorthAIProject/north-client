package fasting_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/fasting"
	"github.com/NorthAIProject/north-client/internal/shared/apitest"
)

func TestFastingShape(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 9, 26, 2, 0, 0, 0, time.UTC)
	v := fasting.ProjectFast(fasting.Session{ID: uuid.MustParse("22222222-2222-2222-2222-222222222222"), StartedAt: start, TargetHours: 16},
		start.Add(12*time.Hour+46*time.Minute))
	apitest.AssertGolden(t, "fasting.golden.json", fasting.FastingView{Current: &v})
}
