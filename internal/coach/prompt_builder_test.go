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

// Each language reaches the model as its own instruction, above the facts, and
// names the variety rather than just "Portuguese".
func TestCoachPromptCarriesTheChosenLanguage(t *testing.T) {
	cases := map[users.Locale]string{
		users.LocaleEN:   "English (en)",
		users.LocalePTPT: "European Portuguese (pt-PT)",
		users.LocalePTBR: "Brazilian Portuguese (pt-BR)",
		users.LocaleES:   "Spanish (es)",
	}

	for locale, want := range cases {
		prompt := promptFor(t, users.User{DisplayName: "Ana", Locale: locale})

		if !strings.Contains(prompt, "Answer in "+want) {
			t.Errorf("locale %q: prompt does not ask for %q", locale, want)
		}

		langAt := strings.Index(prompt, "## Language")
		contextAt := strings.Index(prompt, "## CONTEXT")
		switch {
		case langAt < 0:
			t.Errorf("locale %q: no language section", locale)
		case contextAt < 0:
			t.Errorf("locale %q: no context block", locale)
		case langAt > contextAt:
			t.Errorf("locale %q: language section sits inside the context block", locale)
		}
	}
}

// The catalogue is English-only, so a translated exercise name cannot be looked
// up again by anyone. Every non-English locale has to be told to leave them be.
func TestNonEnglishPromptKeepsCatalogueNamesInEnglish(t *testing.T) {
	for _, locale := range []users.Locale{users.LocalePTPT, users.LocalePTBR, users.LocaleES} {
		prompt := promptFor(t, users.User{DisplayName: "Ana", Locale: locale})
		if !strings.Contains(prompt, "stay in English") {
			t.Errorf("locale %q: nothing pins catalogue names to English", locale)
		}
	}

	// English does not need the caveat, and carrying it would spend prompt
	// budget telling the model to leave English names in English.
	prompt := promptFor(t, users.User{DisplayName: "Ana", Locale: users.LocaleEN})
	if strings.Contains(prompt, "stay in English") {
		t.Error("the English prompt carries the translation caveat it cannot need")
	}
}

// An account from before the column existed, or one holding a language this
// build has retired, still gets answered rather than getting an empty section.
func TestCoachPromptFallsBackToEnglish(t *testing.T) {
	for _, locale := range []users.Locale{"", "kl-GL"} {
		prompt := promptFor(t, users.User{DisplayName: "Ana", Locale: locale})
		if !strings.Contains(prompt, "Answer in English (en)") {
			t.Errorf("locale %q: expected English as the fallback", locale)
		}
	}
}
