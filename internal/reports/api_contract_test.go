package reports

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/shared/apitest"
)

func TestReportShapes(t *testing.T) {
	t.Parallel()

	generated := time.Date(2026, 9, 21, 7, 0, 0, 0, time.UTC)
	helpful := true
	summary := ReportSummary{
		ID: uuid.MustParse("f6f6f6f6-f6f6-f6f6-f6f6-f6f6f6f6f6f6"), Kind: "weekly", Title: "Week of 14 September",
		PeriodStart: "2026-09-14", PeriodEnd: "2026-09-20", Status: "ready", GeneratedAt: &generated, Helpful: &helpful,
	}
	apitest.AssertGolden(t, "reports.golden.json", ReportList{Reports: []ReportSummary{summary}})
	apitest.AssertGolden(t, "report.golden.json", ReportDetail{
		ReportSummary: summary,
		Body:          "## The week\n\nFour sessions, sleep steady at 7.2 h.",
	})
}
