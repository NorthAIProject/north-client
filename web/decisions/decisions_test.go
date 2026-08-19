package decisions

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/decisions/decision"
	"github.com/NorthAIProject/north-client/internal/users"
)

func TestIndexPageRendersCreateForm(t *testing.T) {
	var buf bytes.Buffer
	err := IndexPage(users.User{DisplayName: "Fernando"}, nil, DecisionForm{}).Render(context.Background(), &buf)
	if err != nil {
		t.Fatal(err)
	}
	body := buf.String()
	for _, want := range []string{
		"Decisions",
		"What you chose, what you turned down, and why.",
		`action="/app/decisions"`,
		`name="title"`,
		`name="options"`,
		`name="rationale"`,
		"Nothing recorded yet",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("index page missing %q", want)
		}
	}
	// Outcome is a later review, not part of making the call.
	if strings.Contains(body, `name="outcome"`) {
		t.Error("create form should not collect outcome")
	}
}

func TestShowPageCollectsOutcome(t *testing.T) {
	d := decision.Decision{
		Title:     "Quit the evening client",
		Rationale: "energy",
		Outcome:   "Slept better within a fortnight",
	}
	var buf bytes.Buffer
	if err := ShowPage(users.User{DisplayName: "Fernando"}, d, FormFor(d)).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	body := buf.String()
	for _, want := range []string{
		`name="outcome"`,
		"How it turned out",
		// FormFor must pre-fill it, or saving any other edit would wipe the
		// stored review now that the posted value wins.
		"Slept better within a fortnight",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("show page missing %q", want)
		}
	}
}

func TestIndexPageListsDecisions(t *testing.T) {
	list := []decision.Decision{{
		Title:     "Quit the evening client",
		Options:   "keep / quit",
		Rationale: "energy",
		Outcome:   "Slept better within a fortnight",
	}}
	var buf bytes.Buffer
	if err := IndexPage(users.User{DisplayName: "Fernando"}, list, DecisionForm{}).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	body := buf.String()
	if strings.Contains(body, "Nothing recorded yet") {
		t.Error("empty-state copy shown when there are decisions")
	}
	for _, want := range []string{
		"Quit the evening client",
		"Options: keep / quit",
		"Why: energy",
		"Outcome: Slept better within a fortnight",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("list missing %q", want)
		}
	}
}
