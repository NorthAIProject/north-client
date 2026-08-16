// Package metrics is the one place North talks to Prometheus.
//
// Every collector lives here and no feature slice imports client_golang, so the
// whole dependency can be swapped, wrapped, or removed by editing one file —
// the same reason internal/ai keeps the model providers behind an interface.
//
// What belongs here is the shape of a question an operator asks at three in the
// morning: is the coach slow, is a context source broken, are jobs failing. The
// answers that need a user id or a conversation id belong in the logs, which
// already carry them; putting them here as labels is how a metrics endpoint
// turns into an outage.
package metrics

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Outcome labels how something ended. A small closed set on purpose: label
// values are cardinality, and an error string as a label is unbounded.
const (
	OutcomeSuccess = "success"
	OutcomeError   = "error"
)

// Registry holds North's collectors.
//
// Its own registry rather than the client's default: the default is package
// state that anything in the process — including a dependency — can register
// into, and a duplicate registration there panics at startup.
type Registry struct {
	reg *prometheus.Registry

	coachDuration  *prometheus.HistogramVec
	coachTokens    *prometheus.CounterVec
	sourceFailures *prometheus.CounterVec
	jobRuns        *prometheus.CounterVec
	jobDuration    *prometheus.HistogramVec
}

// New builds the collectors and registers them.
func New() *Registry {
	r := &Registry{reg: prometheus.NewRegistry()}

	r.coachDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "north_coach_generation_duration_seconds",
		Help: "How long one model call took, by provider and outcome.",
		// Wider than the default buckets, which top out at 10s. A coach reply
		// routinely takes longer than that, and a histogram whose last bucket
		// catches most observations cannot answer what a slow reply costs.
		Buckets: []float64{0.5, 1, 2, 5, 10, 20, 30, 60, 120},
	}, []string{"provider", "outcome"})

	r.coachTokens = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "north_coach_tokens_total",
		Help: "Tokens spent, by provider and direction.",
	}, []string{"provider", "direction"})

	r.sourceFailures = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "north_context_source_failures_total",
		Help: "Context sources that failed to collect, by source.",
	}, []string{"source"})

	r.jobRuns = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "north_job_runs_total",
		Help: "Background jobs that finished, by kind and outcome.",
	}, []string{"kind", "outcome"})

	r.jobDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "north_job_duration_seconds",
		Help:    "How long a background job took, by kind.",
		Buckets: []float64{0.1, 0.5, 1, 5, 15, 60, 300, 900},
	}, []string{"kind"})

	r.reg.MustRegister(r.coachDuration, r.coachTokens, r.sourceFailures, r.jobRuns, r.jobDuration)
	return r
}

// CoachGeneration records one model call.
//
// Every method here tolerates a nil receiver, so a process with metrics turned
// off calls the same code paths as one with them on. The alternative is a nil
// check at every call site, which is the kind of thing that gets forgotten once
// and then reads as "this never happens".
func (r *Registry) CoachGeneration(provider string, d time.Duration, failed bool) {
	if r == nil {
		return
	}

	outcome := OutcomeSuccess
	if failed {
		outcome = OutcomeError
	}
	r.coachDuration.WithLabelValues(provider, outcome).Observe(d.Seconds())
}

// CoachTokens records what a call spent.
func (r *Registry) CoachTokens(provider string, in, out int) {
	if r == nil {
		return
	}
	if in > 0 {
		r.coachTokens.WithLabelValues(provider, "input").Add(float64(in))
	}
	if out > 0 {
		r.coachTokens.WithLabelValues(provider, "output").Add(float64(out))
	}
}

// ContextSourceFailed records a source that could not collect.
//
// The counter exists because these fail soft: the coach answers without the
// source and the reply still looks fine, so a source can be broken for weeks
// with nothing but a log line nobody reads to say so.
func (r *Registry) ContextSourceFailed(source string) {
	if r == nil {
		return
	}
	r.sourceFailures.WithLabelValues(source).Inc()
}

// JobFinished records a job run and how long it took.
func (r *Registry) JobFinished(kind string, d time.Duration, failed bool) {
	if r == nil {
		return
	}

	outcome := OutcomeSuccess
	if failed {
		outcome = OutcomeError
	}
	r.jobRuns.WithLabelValues(kind, outcome).Inc()
	r.jobDuration.WithLabelValues(kind).Observe(d.Seconds())
}

// Handler serves the exposition format.
//
// Whoever mounts this decides where it listens, and it must not be the public
// router: request rates, model spend and job failure counts describe the
// business to anyone who asks. See the metrics listener in cmd/web.
func (r *Registry) Handler() http.Handler {
	if r == nil {
		return http.NotFoundHandler()
	}
	return promhttp.HandlerFor(r.reg, promhttp.HandlerOpts{})
}
