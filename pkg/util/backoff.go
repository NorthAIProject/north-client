package util

import (
	"math"
	"math/rand"
	"time"
)

type Backoff func(attempt int) time.Duration

type backoffOptions struct {
	limit    time.Duration
	jitter   time.Duration
	strategy func(attempt int) time.Duration
}

type BackoffOption interface {
	apply(*backoffOptions)
}

type backoffMaxOption time.Duration

func (o backoffMaxOption) apply(opts *backoffOptions) {
	opts.limit = time.Duration(o)
}

func BackoffWithMax(max time.Duration) BackoffOption {
	return backoffMaxOption(max)
}

// Jitter backoff
type backoffJitterOption time.Duration

func (o backoffJitterOption) apply(opts *backoffOptions) {
	opts.jitter = time.Duration(o)
}

func BackoffWithJitter(jitter time.Duration) BackoffOption {
	return backoffJitterOption(jitter)
}

// Static backoff
type backoffStrategyStaticOption time.Duration

func (o backoffStrategyStaticOption) apply(opts *backoffOptions) {
	opts.strategy = func(_ int) time.Duration {
		return time.Duration(o)
	}
}

func BackoffStrategyStatic(static time.Duration) BackoffOption {
	return backoffStrategyStaticOption(static)
}

// Exponential backoff
type backoffStrategyExponentialOption struct {
	initial time.Duration
	base    float64
}

func (o backoffStrategyExponentialOption) apply(opts *backoffOptions) {
	opts.strategy = func(attempt int) time.Duration {
		return o.initial * time.Duration(math.Pow(o.base, float64(attempt)))
	}
}

func BackoffStrategyExponential(initial time.Duration, base int) BackoffOption {
	return backoffStrategyExponentialOption{initial: initial, base: float64(base)}
}

func NewBackoff(opts ...BackoffOption) Backoff {
	options := &backoffOptions{}

	for _, o := range opts {
		o.apply(options)
	}

	return backoffWithOptions(options)
}

func backoffWithOptions(options *backoffOptions) Backoff {
	return func(attempt int) time.Duration {
		backoff := options.strategy(attempt)
		if options.jitter != 0 {
			backoff += time.Duration(rand.Int63n(int64(options.jitter)))
		}
		if options.limit != 0 {
			backoff = time.Duration(math.Min(float64(options.limit), float64(backoff)))
		}
		return backoff
	}
}
