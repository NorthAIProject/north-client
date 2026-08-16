package eval_test

import (
	"context"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/eval"
	"github.com/NorthAIProject/north-client/internal/ai/fake"
	"github.com/NorthAIProject/north-client/internal/coach"
)

// The offline half of the eval suite. No provider, no database, no build tag,
// so it runs on every push and costs nothing.
//
// What it can prove is narrow but not small: that the facts a case carries
// actually reach the model, in the format production sends. Most eval failures
// in practice are this — a renamed heading, a source that stopped contributing
// — and catching them here means the live tier is only ever asked about model
// behaviour.

// TestFixturesRenderTheFactsTheyCarry grades every case's context against the
// real PromptBuilder, which is the whole point: the fixtures cannot drift from
// production because they are rendered by production's own code.
func TestFixturesRenderTheFactsTheyCarry(t *testing.T) {
	builder := coach.NewPromptBuilder()

	for _, c := range eval.Cases() {
		t.Run(c.ID, func(t *testing.T) {
			system, err := builder.Coach(c.Context)
			if err != nil {
				t.Fatalf("build coach prompt: %v", err)
			}

			for _, failure := range c.GradePrompt(system) {
				t.Errorf("case %s: %s\n\nwhy this case exists: %s\n\ncontext block:\n%s",
					c.ID, failure, c.Why, c.Context.Render())
			}
		})
	}
}

// TestSuiteCoversTheGrowthOSScenarios pins the acceptance criterion in code.
//
// A suite is easy to hollow out by accident — a case loses its assertions in a
// refactor and keeps passing, greenly, forever. Asserting on the shape of the
// suite itself is cheap insurance against that.
func TestSuiteCoversTheGrowthOSScenarios(t *testing.T) {
	cases := eval.Cases()

	if len(cases) < 4 {
		t.Errorf("the eval suite must cover at least 4 growth-OS scenarios, found %d", len(cases))
	}

	seen := make(map[string]bool, len(cases))
	for _, c := range cases {
		switch {
		case c.ID == "":
			t.Error("a case has no ID; failures would be unattributable")
		case seen[c.ID]:
			t.Errorf("duplicate case ID %q: subtest output would be ambiguous", c.ID)
		}
		seen[c.ID] = true

		if strings.TrimSpace(c.Why) == "" {
			t.Errorf("case %s has no Why; a failure would not say what it costs", c.ID)
		}
		if c.Context == nil {
			t.Errorf("case %s has no context fixture", c.ID)
		}
		if strings.TrimSpace(c.Ask) == "" {
			t.Errorf("case %s has no question, so the live tier cannot run it", c.ID)
		}
		if len(c.Prompt) == 0 {
			t.Errorf("case %s asserts nothing about the prompt, so it never runs offline", c.ID)
		}
		if len(c.Reply) == 0 {
			t.Errorf("case %s asserts nothing about the reply, so it never runs live", c.ID)
		}
	}
}

// TestInventedCitationsAreStripped is the one grounding behaviour that can be
// graded without a provider, because the defence is ours rather than the
// model's.
//
// It runs a scripted reply through the fake provider and the real StripRefs,
// and checks both halves of the invariant: production records only the ref that
// was offered, and the eval assertion notices the one that was not. The second
// half matters as much as the first — an assertion that cannot fail is decoration.
func TestInventedCitationsAreStripped(t *testing.T) {
	c := caseByID(t, "citations-when-docs-exist")

	const scripted = "Your notes say to deload every fourth week [[chunk:physio-deload-1]], " +
		"and that sleep drives recovery [[chunk:invented-42]]."

	client := fake.Text(scripted)
	ch, err := client.Chat(context.Background(), ai.Request{
		System:   "irrelevant: the fake replays a script",
		Messages: []ai.Message{ai.UserText(c.Ask)},
	})
	if err != nil {
		t.Fatalf("fake chat: %v", err)
	}

	reply, err := eval.Collect(ch)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if reply != scripted {
		t.Fatalf("the fake did not replay the script intact:\n got: %s\nwant: %s", reply, scripted)
	}

	cleaned, recorded := coach.StripRefs(reply, c.Context.OfferedRefs())

	if len(recorded) != 1 || recorded[0] != "chunk:physio-deload-1" {
		t.Errorf("only the offered ref should be recorded, got %v", recorded)
	}
	if strings.Contains(cleaned, "[[") {
		t.Errorf("the visible reply still contains a machine handle:\n%s", cleaned)
	}

	failures := c.GradeReply(reply)
	if len(failures) == 0 {
		t.Fatal("CitesOnlyOfferedRefs passed a reply citing chunk:invented-42; the assertion cannot fail")
	}
	if !containsSubstring(failures, "chunk:invented-42") {
		t.Errorf("the failure does not name the invented ref, so it is not actionable: %v", failures)
	}
}

// TestAnAbsentFactFailsItsCase is the mutation check, kept rather than run by
// hand: strip a fixture of the thing it asserts on and the suite must go red.
func TestAnAbsentFactFailsItsCase(t *testing.T) {
	c := caseByID(t, "goals-reach-the-prompt")

	stripped := *c.Context
	stripped.Goals = nil

	system, err := coach.NewPromptBuilder().Coach(&stripped)
	if err != nil {
		t.Fatalf("build coach prompt: %v", err)
	}

	failures := c.GradePrompt(system)
	if len(failures) == 0 {
		t.Fatal("a context with no goals passed goals-reach-the-prompt; the case proves nothing")
	}
	if !containsSubstring(failures, "Squat 140kg") {
		t.Errorf("the failure does not name the missing goal, so it is not actionable: %v", failures)
	}
}

func caseByID(t *testing.T, id string) eval.Case {
	t.Helper()

	for _, c := range eval.Cases() {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("no eval case named %q", id)
	return eval.Case{}
}

func containsSubstring(messages []string, want string) bool {
	for _, m := range messages {
		if strings.Contains(m, want) {
			return true
		}
	}
	return false
}
