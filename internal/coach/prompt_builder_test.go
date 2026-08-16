package coach_test

import (
	"strings"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/coach"
	"github.com/NorthAIProject/north-client/internal/users"
)

func promptFor(t *testing.T, user users.User) string {
	t.Helper()

	cc := &coach.Context{User: user, LocalTime: time.Date(2026, 8, 17, 9, 0, 0, 0, time.UTC)}
	prompt, err := coach.NewPromptBuilder().Coach(cc)
	if err != nil {
		t.Fatalf("build prompt: %v", err)
	}
	return prompt
}

// Each tone reaches the model as its own instruction, above the facts.
func TestCoachPromptCarriesTheChosenTone(t *testing.T) {
	cases := map[users.Tone]string{
		users.ToneDirect:     "Answer first",
		users.ToneWarm:       "Lead with the person",
		users.ToneAnalytical: "Show the reasoning",
		users.ToneToughLove:  "Say the uncomfortable thing first",
	}

	for tone, want := range cases {
		prompt := promptFor(t, users.User{DisplayName: "Ana", CoachingTone: tone})

		if !strings.Contains(prompt, want) {
			t.Errorf("tone %q: prompt does not contain %q", tone, want)
		}

		toneAt := strings.Index(prompt, "## Tone")
		contextAt := strings.Index(prompt, "## CONTEXT")
		switch {
		case toneAt < 0:
			t.Errorf("tone %q: no tone section", tone)
		case contextAt < 0:
			t.Errorf("tone %q: no context block", tone)
		case toneAt > contextAt:
			t.Errorf("tone %q: tone section sits inside the context block", tone)
		}
	}
}

// An account from before the column existed, or one holding a value this build
// does not know, still gets a voice rather than an empty section.
func TestCoachPromptFallsBackToTheDefaultTone(t *testing.T) {
	for _, tone := range []users.Tone{"", "shouty"} {
		prompt := promptFor(t, users.User{DisplayName: "Ana", CoachingTone: tone})
		if !strings.Contains(prompt, "Answer first") {
			t.Errorf("tone %q: expected the direct tone as the fallback", tone)
		}
	}
}

// The free-text note stays where it was: a fact about the person, in the
// context block, refining the tone rather than replacing it.
func TestCoachPromptKeepsTheFreeTextStyleInContext(t *testing.T) {
	prompt := promptFor(t, users.User{
		DisplayName:   "Ana",
		CoachingTone:  users.ToneWarm,
		CoachingStyle: "Don't let me skip leg day twice.",
	})

	styleAt := strings.Index(prompt, "Don't let me skip leg day twice.")
	contextAt := strings.Index(prompt, "## CONTEXT")
	if styleAt < 0 {
		t.Fatal("the free-text coaching style never reached the prompt")
	}
	if styleAt < contextAt {
		t.Fatal("the free-text style rendered above the context block")
	}
}

// Reflection sessions are the same coach, so they take the same tone.
func TestReflectionPromptCarriesTheTone(t *testing.T) {
	cc := &coach.Context{User: users.User{DisplayName: "Ana", CoachingTone: users.ToneAnalytical}}

	prompt, err := coach.NewPromptBuilder().Reflection(cc, 2)
	if err != nil {
		t.Fatalf("build reflection prompt: %v", err)
	}
	if !strings.Contains(prompt, "Show the reasoning") {
		t.Fatal("reflection prompt ignored the chosen tone")
	}
}
