package coach

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/ai"
)

func call(name, args string) ai.ToolCall {
	return ai.ToolCall{Name: name, Arguments: json.RawMessage(args)}
}

// The bug this exists for: the approval card printed the tool name and its raw
// JSON, so a person deciding whether to let the coach replace their plan was
// reading `create_workout_plan {"goal": "Squat 120 kg", "days_per_week": 3}`.
func TestDescribeCallReadsAsSentencesNotJSON(t *testing.T) {
	t.Parallel()

	got := DescribeCall(call("create_workout_plan",
		`{"goal":"Squat 120 kg by spring","days_per_week":3,"weekdays":["Monday","Wednesday","Friday"],`+
			`"equipment":["barbell","machine"],"limitations":"left knee flares on deep lunges","session_minutes":60}`))

	want := "Create workout plan\n" +
		"Goal: Squat 120 kg by spring\n" +
		"Days per week: 3\n" +
		"Weekdays: Monday, Wednesday, Friday\n" +
		"Equipment: barbell, machine\n" +
		"Limitations: left knee flares on deep lunges\n" +
		"Session minutes: 60"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	if strings.ContainsAny(got, "{}\"") {
		t.Fatalf("raw JSON leaked into the summary: %q", got)
	}
}

func TestDescribeCallWithoutArguments(t *testing.T) {
	t.Parallel()

	for _, args := range []string{"", "{}", "  ", "null"} {
		if got := DescribeCall(call("delete_meal_plan", args)); got != "Delete meal plan" {
			t.Fatalf("args %q: got %q", args, got)
		}
	}
}

// Empty values say nothing a person needs to approve, so they are left out
// rather than shown as "Limitations: ".
func TestDescribeCallSkipsEmptyValues(t *testing.T) {
	t.Parallel()

	got := DescribeCall(call("create_goal", `{"title":"Run 5k","notes":"","tags":[],"due":null}`))
	if got != "Create goal\nTitle: Run 5k" {
		t.Fatalf("got %q", got)
	}
}

func TestDescribeCallScalarsAndNesting(t *testing.T) {
	t.Parallel()

	got := DescribeCall(call("log_water", `{"ml":250.5,"confirmed":true,"meta":{"source":"chat","count":2},`+
		`"items":[{"name":"Oats","grams":80},{"name":"Milk","grams":200}]}`))
	want := "Log water\n" +
		"Ml: 250.5\n" +
		"Confirmed: yes\n" +
		"Meta: source chat, count 2\n" +
		"Items: name Oats, grams 80; name Milk, grams 200"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

// A long value is shortened, never dropped: the person still sees it is there.
func TestDescribeCallShortensLongValues(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("a", 500)
	got := DescribeCall(call("create_check_in", `{"notes":"`+long+`"}`))
	line := strings.Split(got, "\n")[1]
	if len([]rune(line)) > 260 || !strings.HasSuffix(line, "…") {
		t.Fatalf("expected a shortened line ending in …, got %d runes: %q", len([]rune(line)), line)
	}
}

// Arguments that are not a JSON object still reach the person, as text.
func TestDescribeCallFallsBackOnUnparseableArguments(t *testing.T) {
	t.Parallel()

	got := DescribeCall(call("set_alert", `not json`))
	if got != "Set alert\nnot json" {
		t.Fatalf("got %q", got)
	}
}
