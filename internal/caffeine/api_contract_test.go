package caffeine_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/caffeine"
	"github.com/NorthAIProject/north-client/internal/shared/apitest"
)

func TestCaffeineTodayShape(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 9, 26, 9, 58, 0, 0, time.UTC)
	apitest.AssertGolden(t, "caffeine.golden.json", caffeine.ProjectToday([]caffeine.Entry{
		{ID: uuid.MustParse("11111111-1111-1111-1111-111111111111"), MG: 100, Label: "coffee", LoggedAt: at},
	}, at.Add(5*time.Hour)))
}
