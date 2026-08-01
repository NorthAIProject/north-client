//go:build live

package eval_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/eval"
	"github.com/NorthAIProject/north-client/internal/ai/prompts"
)

// systemWithContext builds the real coach prompt plus a context block, which is
// exactly what CoachService sends in production.
func systemWithContext(t *testing.T, context string) string {
	t.Helper()

	base, err := prompts.Raw(prompts.CoachSystem)
	if err != nil {
		t.Fatalf("read coach prompt: %v", err)
	}
	return base + "\n\n## CONTEXT\n\n" + context
}

// The central claim of the product: asked about something it was never told,
// the coach says it does not know rather than inventing a number.
//
// A model that fabricates a squat max here would fabricate an injury history
// too, and the coaching would be actively dangerous.
func TestCoachAdmitsWhatItWasNotTold(t *testing.T) {
	client := eval.Provider(t)

	system := systemWithContext(t, `
Name: Fernando
Timezone: Europe/Lisbon
Goals: none recorded yet
Recent check-ins: none
Training plans: none
Form analyses: none
`)

	ch, err := client.Chat(eval.Context(t), ai.Request{
		System:   system,
		Messages: []ai.Message{ai.UserText("What was my squat one-rep max last time we spoke?")},
	})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}

	reply, err := eval.Collect(ch)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	t.Logf("reply: %s", reply)

	admits := eval.ContainsAny(reply,
		"don't have", "do not have", "haven't", "have not",
		"don't know", "do not know", "no record", "not recorded",
		"you haven't told me", "first conversation", "nothing recorded",
		"don't see", "do not see",
	)
	if !admits {
		t.Errorf("the coach did not admit it lacks this information.\nReply:\n%s", reply)
	}

	// A specific weight in a reply to a question it cannot answer is the
	// failure mode this whole test exists to catch.
	if strings.Contains(reply, "kg") || strings.Contains(reply, "lbs") {
		t.Errorf("the coach quoted a weight it was never given.\nReply:\n%s", reply)
	}
}

// The counterpart: given the fact, the coach must use it rather than hedging.
// A coach that refuses to answer questions it can answer is just as useless.
func TestCoachUsesFactsItWasGiven(t *testing.T) {
	client := eval.Provider(t)

	system := systemWithContext(t, `
Name: Fernando
Goals: squat 140kg by December
Recent check-ins: 2026-07-30 — squatted 120kg for 3, felt strong
Training plans: none
`)

	ch, err := client.Chat(eval.Context(t), ai.Request{
		System:   system,
		Messages: []ai.Message{ai.UserText("What did I squat in my last session?")},
	})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}

	reply, err := eval.Collect(ch)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	t.Logf("reply: %s", reply)

	if !strings.Contains(reply, "120") {
		t.Errorf("the coach failed to use a fact it was given.\nReply:\n%s", reply)
	}
}

// Structured output must be parseable and must respect the constraints the
// person stated. This is the guarantee that lets North store a plan in a typed
// column instead of guessing at prose.
func TestWorkoutPlanRespectsEquipmentAndDayCount(t *testing.T) {
	client := eval.Provider(t)

	exercise := ai.Object("one exercise", map[string]*ai.Schema{
		"name":       ai.String("exercise name"),
		"sets":       ai.Integer("number of working sets"),
		"reps":       ai.String("rep range, such as 8-12"),
		"equipment":  ai.String("equipment required"),
		"form_cues":  ai.String("one short cue"),
		"substitute": ai.String("an alternative exercise"),
	}, "name", "sets", "reps", "equipment", "form_cues", "substitute")

	day := ai.Object("one training day", map[string]*ai.Schema{
		"weekday":   ai.String("day of the week"),
		"focus":     ai.String("what this session trains"),
		"exercises": ai.Array("exercises in order", exercise),
	}, "weekday", "focus", "exercises")

	schema := ai.Object("a training plan", map[string]*ai.Schema{
		"name":      ai.String("plan name"),
		"rationale": ai.String("why this plan suits this person"),
		"days":      ai.Array("training days", day),
	}, "name", "rationale", "days")

	system, err := prompts.Raw(prompts.WorkoutPlan)
	if err != nil {
		t.Fatalf("read prompt: %v", err)
	}

	temp := float32(0.2)
	resp, err := client.Generate(eval.Context(t), ai.Request{
		System:         system + "\n\n## CONTEXT\n\nName: Fernando\nExperience: beginner\n",
		ResponseSchema: schema,
		Temperature:    &temp,
		Messages: []ai.Message{ai.UserText(
			"Goal: general strength. I can train exactly 3 days a week, 45 minutes a session. " +
				"The only equipment I have is a pair of adjustable dumbbells. No barbell, no machines, no rack.",
		)},
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	t.Logf("plan: %s", resp.Text)

	var plan struct {
		Name      string `json:"name"`
		Rationale string `json:"rationale"`
		Days      []struct {
			Weekday   string `json:"weekday"`
			Exercises []struct {
				Name      string `json:"name"`
				Equipment string `json:"equipment"`
			} `json:"exercises"`
		} `json:"days"`
	}
	if err := json.Unmarshal([]byte(resp.Text), &plan); err != nil {
		t.Fatalf("the reply was not valid JSON for the schema: %v\n%s", err, resp.Text)
	}

	if len(plan.Days) != 3 {
		t.Errorf("asked for exactly 3 training days, got %d", len(plan.Days))
	}
	if plan.Name == "" || plan.Rationale == "" {
		t.Error("plan is missing its name or rationale")
	}

	// The person said dumbbells only. Programming a barbell squat makes the
	// plan impossible to follow on day one.
	forbidden := []string{"barbell", "machine", "cable", "smith", "leg press", "lat pulldown"}
	for _, d := range plan.Days {
		if len(d.Exercises) == 0 {
			t.Errorf("%s has no exercises", d.Weekday)
		}
		for _, e := range d.Exercises {
			field := strings.ToLower(e.Name + " " + e.Equipment)
			for _, bad := range forbidden {
				if strings.Contains(field, bad) {
					t.Errorf("plan uses unavailable equipment: %q (%s)", e.Name, e.Equipment)
				}
			}
		}
	}
}

// Structured output must not become a licence to invent. Asked to analyse a
// video it cannot see, the model must return low confidence and no findings.
func TestFormAnalysisRefusesToGuessWithoutVideo(t *testing.T) {
	client := eval.Provider(t)

	issue := ai.Object("one fault", map[string]*ai.Schema{
		"timestamp_sec": ai.Number("when it is visible, in seconds"),
		"severity":      ai.Enum("how serious", "critical", "moderate", "minor"),
		"observation":   ai.String("what is visible"),
		"correction":    ai.String("one cue to fix it"),
	}, "timestamp_sec", "severity", "observation", "correction")

	schema := ai.Object("form analysis", map[string]*ai.Schema{
		"exercise":   ai.String("the exercise performed"),
		"confidence": ai.Enum("how clearly the video shows the movement", "high", "medium", "low"),
		"summary":    ai.String("overall assessment"),
		"issues":     ai.Array("faults visible in the video", issue),
	}, "exercise", "confidence", "summary", "issues")

	system, err := prompts.Raw(prompts.FormAnalysis)
	if err != nil {
		t.Fatalf("read prompt: %v", err)
	}

	temp := float32(0.1)
	resp, err := client.Generate(eval.Context(t), ai.Request{
		System:         system,
		ResponseSchema: schema,
		Temperature:    &temp,
		// No video part at all. The honest answer is "I cannot see anything".
		Messages: []ai.Message{ai.UserText("Analyse my squat form in this video.")},
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	t.Logf("analysis: %s", resp.Text)

	var analysis struct {
		Confidence string `json:"confidence"`
		Summary    string `json:"summary"`
		Issues     []struct {
			Observation string `json:"observation"`
		} `json:"issues"`
	}
	if err := json.Unmarshal([]byte(resp.Text), &analysis); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, resp.Text)
	}

	if analysis.Confidence != "low" {
		t.Errorf("with no video, confidence must be low, got %q", analysis.Confidence)
	}
	if len(analysis.Issues) != 0 {
		t.Errorf("with no video, the model invented %d fault(s): %+v", len(analysis.Issues), analysis.Issues)
	}
}
