package coach

import (
	"context"
	"strings"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
)

// The chat header says what Khepri is doing while a reply is in flight — "is
// thinking", "is writing", "is checking your goals" (see
// _reviews/muse-chat-contract.md, "Avatar states + status copy"). The first two
// the browser can tell for itself from the stream. The third it cannot: tool
// calls never reach the page as content, so the server names them in a
// `status` frame.

// toolStatusKeys maps a capability onto the phrase the header shows while it
// runs. Keyed by what the person would recognise rather than one entry per
// tool: search_exercises and get_exercise are both "looking up exercises" to
// somebody watching. A name missing here falls back to chat.tool.default,
// so a new capability shows a vaguer line rather than none.
var toolStatusKeys = map[string]string{
	"search_exercises": "chat.tool.exercises",
	"get_exercise":     "chat.tool.exercises",

	"calculate_macros": "chat.tool.macros",

	"list_goals":      "chat.tool.goals",
	"search_goals":    "chat.tool.goals",
	"create_goal":     "chat.tool.goals.write",
	"add_goal_update": "chat.tool.goals.write",

	"create_check_in": "chat.tool.checkin",
	"list_check_ins":  "chat.tool.checkin.read",

	"search_documents": "chat.tool.documents",

	"get_workout_plan":        "chat.tool.workout",
	"swap_workout_exercise":   "chat.tool.workout.write",
	"add_workout_exercise":    "chat.tool.workout.write",
	"remove_workout_exercise": "chat.tool.workout.write",

	"search_ingredients": "chat.tool.nutrition",
	"todays_nutrition":   "chat.tool.nutrition",

	"list_alerts": "chat.tool.alerts",
	"set_alert":   "chat.tool.alerts.write",

	"log_water":      "chat.tool.log",
	"log_sleep":      "chat.tool.log",
	"complete_habit": "chat.tool.log",
	"log_activity":   "chat.tool.log",
	"record_weight":  "chat.tool.log",
	"log_food":       "chat.tool.log",
}

const defaultToolStatusKey = "chat.tool.default"

// ToolStatusKeys lists every label key a tool can resolve to, for the test
// that holds each translation under the contract's length limit.
func ToolStatusKeys() []string {
	seen := map[string]bool{defaultToolStatusKey: true}
	out := []string{defaultToolStatusKey}
	for _, key := range toolStatusKeys {
		if !seen[key] {
			seen[key] = true
			out = append(out, key)
		}
	}
	return out
}

// toolStatus is the full status line for a round of tool calls, in the
// request's language: "is checking your goals".
//
// A round can carry several calls; the first names it. The header has room
// for one phrase, and the model lists the call it cares most about first
// often enough that picking any other would be no better.
func toolStatus(ctx context.Context, calls []ai.ToolCall) string {
	key := defaultToolStatusKey
	if len(calls) > 0 {
		if k, ok := toolStatusKeys[calls[0].Name]; ok {
			key = k
		}
	}
	return i18n.Tf(ctx, "chat.status.tool", i18n.T(ctx, key))
}

// sseLine flattens a value onto one SSE data line. A newline inside data:
// ends the field early, and a status label has no business carrying one.
func sseLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
