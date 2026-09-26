package screentime_test

import (
	"testing"

	"github.com/NorthAIProject/north-client/internal/screentime"
	"github.com/NorthAIProject/north-client/internal/shared/apitest"
)

func TestScreenTimeShape(t *testing.T) {
	t.Parallel()
	apitest.AssertGolden(t, "screen_time.golden.json", screentime.ScreenTimeView{Date: "2026-09-26", Minutes: 248, Source: "shortcut"})
}
