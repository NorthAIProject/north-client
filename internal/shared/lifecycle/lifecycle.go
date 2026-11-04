// Package lifecycle owns goroutines that outlive the code that started them.
//
// Echo's recover covers the goroutine a handler runs on and nothing else, so a
// panic anywhere else ends the process, along with every stream and turn in it.
// Separately, work detached from a request is invisible to the server's graceful
// shutdown, so a rollout kills it mid-flight. Group answers the first for fan-out
// inside a request, and Tracker answers both for work that outlives one.
package lifecycle

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

// Group is errgroup.Group with one difference: a panic in a function becomes
// that function's error instead of ending the process. The rest of the API is
// unchanged, so call sites read as they always did.
type Group struct {
	g *errgroup.Group
}

// WithContext is errgroup.WithContext for a Group. Like errgroup, the returned
// context is cancelled by the first error, which here includes a panic.
func WithContext(ctx context.Context) (*Group, context.Context) {
	g, ctx := errgroup.WithContext(ctx)
	return &Group{g: g}, ctx
}

// Go runs f in a new goroutine, subject to any limit set with SetLimit.
func (g *Group) Go(f func() error) {
	g.g.Go(func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("panic: %v\n%s", r, debug.Stack())
			}
		}()
		return f()
	})
}

// SetLimit bounds how many functions run at once. See errgroup.Group.SetLimit.
func (g *Group) SetLimit(n int) { g.g.SetLimit(n) }

// Wait blocks until every function has returned, then returns the first error.
func (g *Group) Wait() error { return g.g.Wait() }

// Recover logs a panic and stops it. Deferred directly by a goroutine that has
// no other way to report one.
func Recover(log *slog.Logger, name string) {
	if r := recover(); r != nil {
		logger(log).Error("goroutine panicked",
			slog.String("name", name), slog.Any("panic", r), slog.String("stack", string(debug.Stack())))
	}
}

// Tracker runs detached work so a shutdown can wait for it.
//
// A nil Tracker is valid and still recovers panics, so a test or a binary with
// no shutdown to coordinate has nothing to wire.
type Tracker struct {
	log *slog.Logger
	wg  sync.WaitGroup
}

// NewTracker returns a Tracker that reports panics to log.
func NewTracker(log *slog.Logger) *Tracker {
	return &Tracker{log: log}
}

// Go runs f in a new goroutine that Drain waits for. A panic is logged and
// stopped. name appears in that log line.
func (t *Tracker) Go(name string, f func()) {
	if t == nil {
		go func() {
			defer Recover(nil, name)
			f()
		}()
		return
	}
	t.wg.Go(func() {
		defer Recover(t.log, name)
		f()
	})
}

// Drain waits for tracked work to finish, for at most limit. It reports whether
// everything finished. Work still running is left to end with the process.
func (t *Tracker) Drain(limit time.Duration) bool {
	if t == nil {
		return true
	}
	done := make(chan struct{})
	go func() {
		t.wg.Wait()
		close(done)
	}()

	timer := time.NewTimer(limit)
	defer timer.Stop()
	select {
	case <-done:
		return true
	case <-timer.C:
		return false
	}
}

func logger(log *slog.Logger) *slog.Logger {
	if log == nil {
		return slog.Default()
	}
	return log
}
