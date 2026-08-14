// Package reports owns weekly reviews: generating them, storing them, and
// putting the latest one in front of the coach.
package reports

import "github.com/NorthAIProject/north-client/internal/reports/report"

type (
	Report = report.Report
	Kind   = report.Kind
	Status = report.Status
)

const (
	KindWeekly    = report.KindWeekly
	StatusPending = report.StatusPending
	StatusReady   = report.StatusReady
	StatusFailed  = report.StatusFailed
)
