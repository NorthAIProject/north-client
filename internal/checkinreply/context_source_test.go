package checkinreply_test

import (
	"context"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/checkinreply"
	"github.com/NorthAIProject/north-client/internal/checkins"
	"github.com/NorthAIProject/north-client/internal/coach"
)

// The coach should not contradict the note the person has just read, so it
// is told what the note said. Reading the context never writes one.
func TestContextSourceTellsTheCoachWhatTheNoteSaid(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	src := checkinreply.NewContextSource(f.svc)
	f.checkIn(t, checkins.Input{Mood: 3, Energy: 2})

	var before coach.Context
	if err := src.Collect(ctx, coach.ContextRequest{User: f.user}, &before); err != nil {
		t.Fatal(err)
	}
	if len(before.CheckIns) != 0 || f.writer.calls != 0 {
		t.Fatalf("before a note: %v, %d calls; want nothing and no model call", before.CheckIns, f.writer.calls)
	}

	if _, ok, err := f.svc.ForToday(ctx, f.user); err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	var after coach.Context
	if err := src.Collect(ctx, coach.ContextRequest{User: f.user}, &after); err != nil {
		t.Fatal(err)
	}
	if len(after.CheckIns) != 1 || !strings.Contains(after.CheckIns[0], f.writer.text) {
		t.Fatalf("after a note: %v", after.CheckIns)
	}
	if f.writer.calls != 1 {
		t.Fatalf("calls = %d, want only the one ForToday made", f.writer.calls)
	}
}
