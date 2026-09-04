package eval_test

import (
	"strings"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/capture"
	"github.com/NorthAIProject/north-client/internal/capture/eval"
	"github.com/NorthAIProject/north-client/internal/users"
)

// Every case's prompt assertions, with no provider and no database.
//
// This is the tier that runs on every push, and it grades capture.RenderPrompt
// itself rather than a copy, so an edit that drops the habit list or the local
// date fails here rather than degrading quietly in production.
func TestPromptsAreGrounded(t *testing.T) {
	t.Parallel()

	for _, c := range eval.Cases() {
		t.Run(c.ID, func(t *testing.T) {
			t.Parallel()

			system := eval.RenderFor(t, c)
			for _, failure := range c.GradePrompt(system) {
				t.Errorf("%s\nwhy this matters: %s", failure, c.Why)
			}
		})
	}
}

// The rules the corpus depends on have to actually be in the prompt. Each of
// these is an instruction a case is grading the model against, and a case that
// grades an instruction the prompt no longer carries is grading nothing.
func TestThePromptCarriesTheRulesTheCasesGrade(t *testing.T) {
	t.Parallel()

	system := eval.RenderFor(t, eval.Cases()[0])

	for _, want := range []string{
		// "never invent an entry from something they did not say"
		"Never invent an entry",
		// the check-in half-score rule. Matched without the surrounding
		// markdown emphasis, which is formatting rather than instruction.
		"guess the other",
		// habits are never created by a capture
		"do not invent an entry",
		// the leftovers contract
		"unparsed",
	} {
		if !strings.Contains(strings.ToLower(system), strings.ToLower(want)) {
			t.Errorf("the prompt no longer says %q, but a case still grades it", want)
		}
	}
}

// The person's own day, not the server's. Sleep is filed against a local date,
// and a prompt carrying UTC would have the model resolve "last night" against
// the wrong one for most of the planet.
func TestThePromptCarriesTheirLocalTime(t *testing.T) {
	t.Parallel()

	user := users.User{Timezone: "Pacific/Auckland"}
	now := time.Date(2026, 9, 4, 9, 30, 0, 0, user.Location())

	system, err := capture.RenderPrompt(user, "slept 6h", nil, now)
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	for _, want := range []string{"Pacific/Auckland", "Friday 4 September 2026"} {
		if !strings.Contains(system, want) {
			t.Errorf("the prompt is missing %q", want)
		}
	}
}

// A person with no habits must be told so, or the model has nothing to stop it
// naming one.
func TestAPersonWithNoHabitsIsSaidSoOutLoud(t *testing.T) {
	t.Parallel()

	system, err := capture.RenderPrompt(users.User{Timezone: "UTC"}, "meditated", nil, time.Now())
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	if !strings.Contains(system, "keeps no habits yet") {
		t.Error("the prompt does not tell the model there are no habits to name")
	}
	if !strings.Contains(system, "never produce a habit entry") {
		t.Error("the prompt does not forbid inventing one")
	}
}

// The person's text has to reach the model. Obvious, and exactly the kind of
// thing a template edit breaks silently.
func TestTheirWordsReachThePrompt(t *testing.T) {
	t.Parallel()

	for _, c := range eval.Cases() {
		system := eval.RenderFor(t, c)
		if !strings.Contains(system, c.Text) {
			t.Errorf("%s: the person's own text is not in the prompt", c.ID)
		}
	}
}
