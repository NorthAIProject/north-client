package dashboard_test

import (
	"testing"

	"github.com/NorthAIProject/north-client/internal/conversations"
	"github.com/NorthAIProject/north-client/internal/dashboard"
	"github.com/NorthAIProject/north-client/internal/goals"
)

func TestPickNextStep(t *testing.T) {
	goal := goals.Goal{Title: "Lift"}
	thread := &conversations.Conversation{}

	tests := []struct {
		name string
		snap dashboard.Snapshot
		ok   bool
		kind string
	}{
		{name: "fresh account names a goal first", snap: dashboard.Snapshot{}, ok: true, kind: "goal"},
		{name: "has goal, no check-in today", snap: dashboard.Snapshot{Goals: []goals.Goal{goal}}, ok: true, kind: "checkin"},
		{name: "checked in, no thread", snap: dashboard.Snapshot{Goals: []goals.Goal{goal}, CheckedInToday: true}, ok: true, kind: "chat"},
		{name: "activated account hides the card", snap: dashboard.Snapshot{Goals: []goals.Goal{goal}, CheckedInToday: true, LastThread: thread}, ok: false},
		// Push waits its turn: an account still missing a goal or a thread is
		// asked for those first, and never for notification permission.
		{name: "push is not offered before activation", snap: dashboard.Snapshot{Goals: []goals.Goal{goal}, PushOffered: true}, ok: true, kind: "checkin"},
		{name: "activated account without a subscription is offered push", snap: dashboard.Snapshot{Goals: []goals.Goal{goal}, CheckedInToday: true, LastThread: thread, PushOffered: true}, ok: true, kind: dashboard.StepKindPush},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := dashboard.PickNextStep(tt.snap)
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			if !ok {
				return
			}
			if got.Kind != tt.kind {
				t.Fatalf("kind = %q, want %q", got.Kind, tt.kind)
			}
			if got.CTA == "" || got.Href == "" || got.Title == "" {
				t.Fatal("next step missing title, CTA, or href")
			}
		})
	}
}

func TestPickNextStepGoalCopy(t *testing.T) {
	got, ok := dashboard.PickNextStep(dashboard.Snapshot{})
	if !ok {
		t.Fatal("fresh account has no next step")
	}
	if got.Title != "Name one thing you are working toward" {
		t.Fatalf("title = %q", got.Title)
	}
	if got.CTA != "Add a goal" {
		t.Fatalf("cta = %q", got.CTA)
	}
	if got.Href != "/app/goals" {
		t.Fatalf("href = %q", got.Href)
	}
}
