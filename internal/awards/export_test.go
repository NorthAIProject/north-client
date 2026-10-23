package awards

// FailMetric makes the named rule metric return an error, to test that one
// failing rule does not stop the others.
func (s *Service) FailMetric(name string) { s.failMetric = metric(name) }
