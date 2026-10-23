package awards

import "time"

// FailMetric makes the named rule metric return an error, to test that one
// failing rule does not stop the others.
func (s *Service) FailMetric(name string) { s.failMetric = metric(name) }

// SetClock replaces the clock Check uses for the monthly challenge.
func (s *Service) SetClock(now func() time.Time) { s.now = now }

// MonthlyHistory and PickMonthly expose the pure picker to the external tests.
type MonthlyHistory = monthlyHistory

func PickMonthly(h monthlyHistory, monthStart time.Time, daysLeft int) (MonthlyMetric, int) {
	return pickMonthly(h, monthStart, daysLeft)
}
