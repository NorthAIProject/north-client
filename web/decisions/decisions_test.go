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
	if strings.Contains(body, `name="outcome"`) {
		t.Error("v1 form should not collect outcome")
	}
}

func TestIndexPageListsDecisions(t *testing.T) {
	list := []decision.Decision{{
		Title:     "Quit the evening client",
		Options:   "keep / quit",
		Rationale: "energy",
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
	} {
		if !strings.Contains(body, want) {
			t.Errorf("list missing %q", want)
		}
	}
}
