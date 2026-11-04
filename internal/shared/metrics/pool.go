package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// PoolStats is what the database pool reports about itself, as the plain values
// this package needs. A struct rather than pgx's own type so that database is
// not one more thing behind this package's import line.
type PoolStats struct {
	// AcquireDuration is the time every acquire so far has spent, added up.
	AcquireDuration time.Duration
	// EmptyAcquireCount is how many acquires so far found no idle connection
	// and had to wait for one, or for one to be opened.
	EmptyAcquireCount int64
	AcquiredConns     int32
	MaxConns          int32
}

// WatchPool exports a database pool's numbers, read fresh on every scrape.
//
// They exist to answer one question before anyone changes a limit: are requests
// queueing for a connection? One page load fans out to more queries than the
// pool has connections, and whether that ever costs a visible wait is a
// measurement rather than an argument.
//
// Call it once per registry: a second registration panics at startup, which is
// the time to find out.
func (r *Registry) WatchPool(stats func() PoolStats) {
	if r == nil {
		return
	}
	r.reg.MustRegister(&poolCollector{stats: stats})
}

// Shared across collectors, because a Desc is only a description.
var (
	poolAcquireDuration = prometheus.NewDesc("north_db_pool_acquire_duration_seconds_total",
		"Time spent acquiring database connections, added up across every acquire.", nil, nil)
	poolEmptyAcquires = prometheus.NewDesc("north_db_pool_empty_acquire_total",
		"Acquires that found no idle connection and had to wait.", nil, nil)
	poolAcquiredConns = prometheus.NewDesc("north_db_pool_acquired_conns",
		"Database connections checked out right now.", nil, nil)
	poolMaxConns = prometheus.NewDesc("north_db_pool_max_conns",
		"The most database connections this pool may hold.", nil, nil)
)

// poolCollector reads the pool when scraped rather than on a timer, so there is
// no goroutine to stop and nothing goes stale between scrapes.
type poolCollector struct {
	stats func() PoolStats
}

func (*poolCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- poolAcquireDuration
	ch <- poolEmptyAcquires
	ch <- poolAcquiredConns
	ch <- poolMaxConns
}

func (c *poolCollector) Collect(ch chan<- prometheus.Metric) {
	s := c.stats()
	ch <- prometheus.MustNewConstMetric(poolAcquireDuration, prometheus.CounterValue, s.AcquireDuration.Seconds())
	ch <- prometheus.MustNewConstMetric(poolEmptyAcquires, prometheus.CounterValue, float64(s.EmptyAcquireCount))
	ch <- prometheus.MustNewConstMetric(poolAcquiredConns, prometheus.GaugeValue, float64(s.AcquiredConns))
	ch <- prometheus.MustNewConstMetric(poolMaxConns, prometheus.GaugeValue, float64(s.MaxConns))
}
