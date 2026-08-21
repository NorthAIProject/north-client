package prompts

import (
	"strings"
	"testing"
)

// Every embedded prompt must parse. A broken template would otherwise surface
// as a failed coaching request in production rather than a failed build.
func TestAllPromptsParse(t *testing.T) {
	t.Parallel()

	names, err := Names()
	if err != nil {
		t.Fatalf("list prompts: %v", err)
	}
	if len(names) == 0 {
		t.Fatal("no prompts embedded; the go:embed directive is not matching")
	}

	for _, name := range names {
		if _, err := load(name); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// The named constants must point at files that exist. Without this a rename
// becomes a runtime error on a path that may be rarely exercised.
func TestNamedPromptsExist(t *testing.T) {
	t.Parallel()

	for _, name := range []string{CoachSystem, WorkoutPlan, FormAnalysis, ConversationTitle, ReflectionSession} {
		body, err := Raw(name)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if len(body) < 100 {
			t.Errorf("%s: suspiciously short (%d bytes)", name, len(body))
		}
	}
}

// The grounding rules are the product's defence against a coach that invents
// facts. This asserts they are actually present, so that a well-meaning edit
// that trims the prompt cannot silently remove them.
func TestCoachPromptStatesGroundingRules(t *testing.T) {
	t.Parallel()

	body, err := Raw(CoachSystem)
	if err != nil {
		t.Fatalf("read coach prompt: %v", err)
	}
	lower := strings.ToLower(body)

	required := map[string]string{
		"instructs the model not to invent facts": "never state a fact about this person that is not in the context",
		"instructs the model to admit ignorance":  "say so plainly and ask",
		"forbids invented history":                "never invent history",
		"forbids medical diagnosis":               "do not give medical diagnoses",
		"bounds media analysis":                   "stay inside your evidence",
		"treats approved memories as known facts": "treat \"known about them\" as facts",
		"admits a missing memory is unknown":      "if a fact you need is not listed there",
		"states the growth mission":               "your mission is this person's growth",
		"refuses off-mission questions":           "refuse off-mission questions",
		"never discusses hosting":                 "never discuss where khepri is hosted",
		"starts the first week with evidence":     "ask for one piece of evidence",
		"commits to a next check":                 "when you will check",
	}

	for what, phrase := range required {
		if !strings.Contains(lower, phrase) {
			t.Errorf("the coach prompt no longer %s (looked for %q)", what, phrase)
		}
	}
}

func TestReflectionPromptStatesTheSessionRules(t *testing.T) {
	t.Parallel()

	body, err := Raw(ReflectionSession)
	if err != nil {
		t.Fatalf("read reflection prompt: %v", err)
	}
	lower := strings.ToLower(body)
	for _, phrase := range []string{
		"reflection session",
		"one question at a time",
		"at least 3 and at most 5",
		"## reflection summary",
		"{{.questionsasked}}",
		"refuse off-mission questions",
		"where khepri is hosted",
	} {
		if !strings.Contains(lower, phrase) {
			t.Errorf("reflection prompt missing %q", phrase)
		}
	}
}

func TestFormAnalysisPromptRequiresLowConfidenceOverGuessing(t *testing.T) {
	t.Parallel()

	body, err := Raw(FormAnalysis)
	if err != nil {
		t.Fatalf("read form analysis prompt: %v", err)
	}
	lower := strings.ToLower(body)

	// The single most important instruction in this prompt: an unclear video
	// must produce no findings rather than plausible invented ones.
	for _, phrase := range []string{`"low"`, "empty", "not seeing a problem is not the same"} {
		if !strings.Contains(lower, phrase) {
			t.Errorf("form analysis prompt is missing its no-guessing instruction (looked for %q)", phrase)
		}
	}
}

func TestRenderSubstitutesData(t *testing.T) {
	t.Parallel()

	out, err := Render(ConversationTitle, map[string]any{"Message": "my knee hurts when I run"})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(out, "my knee hurts when I run") {
		t.Fatalf("the message was not substituted into the prompt:\n%s", out)
	}
}

// missingkey=error is what stops "<no value>" being rendered into a coaching
// instruction, so a missing field must fail loudly.
func TestRenderFailsOnMissingData(t *testing.T) {
	t.Parallel()

	if _, err := Render(ConversationTitle, map[string]any{}); err == nil {
		t.Fatal("rendering with a missing key should fail rather than emit <no value>")
	}
}

func TestRenderUnknownPrompt(t *testing.T) {
	t.Parallel()

	if _, err := Render("does_not_exist.md", nil); err == nil {
		t.Fatal("expected an error for an unknown prompt")
	}
}
