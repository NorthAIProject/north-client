package lifecycle_test

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/shared/lifecycle"
)

// A loader that panics must fail its own request, not the process.
func TestAPanicInAGroupBecomesItsError(t *testing.T) {
	g, ctx := lifecycle.WithContext(context.Background())
	g.Go(func() error { panic("index out of range") })

	err := g.Wait()
	if err == nil || !strings.Contains(err.Error(), "index out of range") {
		t.Fatalf("Wait() = %v, want the panic as an error", err)
	}
	if !strings.Contains(err.Error(), "lifecycle_test.go") {
		t.Errorf("the error lost the stack: %v", err)
	}
	if ctx.Err() == nil {
		t.Error("a panic did not cancel the group's context")
	}
}

func TestAGroupStillReturnsAnOrdinaryError(t *testing.T) {
	want := errors.New("no such row")
	g, _ := lifecycle.WithContext(context.Background())
	g.Go(func() error { return want })

	if err := g.Wait(); !errors.Is(err, want) {
		t.Fatalf("Wait() = %v, want %v", err, want)
	}
}

func TestDrainWaitsForTrackedWork(t *testing.T) {
	tracker := lifecycle.NewTracker(slog.New(slog.DiscardHandler))

	finished := make(chan struct{})
	tracker.Go("slow", func() {
		time.Sleep(30 * time.Millisecond)
		close(finished)
	})

	if !tracker.Drain(5 * time.Second) {
		t.Fatal("Drain gave up on work that finishes")
	}
	select {
	case <-finished:
	default:
		t.Fatal("Drain returned before the work finished")
	}
}

// Past its limit a shutdown has to go ahead, or the kubelet decides for it.
func TestDrainGivesUpAtItsLimit(t *testing.T) {
	tracker := lifecycle.NewTracker(slog.New(slog.DiscardHandler))

	release := make(chan struct{})
	defer close(release)
	tracker.Go("stuck", func() { <-release })

	if tracker.Drain(20 * time.Millisecond) {
		t.Fatal("Drain reported success with work still running")
	}
}

func TestATrackedPanicDoesNotStopTheDrain(t *testing.T) {
	tracker := lifecycle.NewTracker(slog.New(slog.DiscardHandler))
	tracker.Go("bad", func() { panic("bad") })

	if !tracker.Drain(5 * time.Second) {
		t.Fatal("a panic left the tracker waiting")
	}
}

func TestANilTrackerStillRunsAndRecovers(t *testing.T) {
	var tracker *lifecycle.Tracker

	ran := make(chan struct{})
	tracker.Go("nil", func() {
		defer close(ran)
		panic("still recovered")
	})
	<-ran

	if !tracker.Drain(time.Millisecond) {
		t.Fatal("a nil tracker has nothing to wait for")
	}
}
