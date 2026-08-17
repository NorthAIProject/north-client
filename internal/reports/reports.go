// Package reports owns the person's generated write-ups — the weekly review and
// the daily briefing — generating them, storing them, and putting the latest
// ones in front of the coach and the dashboard.
package reports

import "github.com/NorthAIProject/north-client/internal/reports/report"

type (
	Report = report.Report
	Kind   = report.Kind
	Status = report.Status
)

const (
	KindWeekly    = report.KindWeekly
	KindDaily     = report.KindDaily
	StatusPending = report.StatusPending
	StatusReady   = report.StatusReady
	StatusFailed  = report.StatusFailed
)
